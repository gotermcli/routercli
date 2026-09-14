// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// Role - This type is one entry in a deployment's example/var/tree/roles.yaml,
// naming one role a user may hold and be granted through a Command or
// CommandLevel's AllowedRoles list. Roles in this project are flat and
// unordered, see RoleSet's doc comment, not a numbered hierarchy the way
// Cisco's privilege levels are.
type Role struct {
	// Name identifies this role. It is what a Command or CommandLevel's
	// AllowedRoles list references, and what auth.User.Roles stores. This
	// is not decoded from YAML; LoadRoles sets it from this role's key
	// under the manifest's "roles:" map, the same way CommandLevel.Name is
	// set from tree_structure.yaml's map key.
	Name string `yaml:"-"`

	// Desc is a short, human readable description of what this role is
	// for, purely documentation, read by nothing in package command itself
	// today.
	Desc string `yaml:"desc"`

	// Bypass, false by default, marks this role as the deployment's one
	// reserved escape hatch. A user holding it automatically passes every
	// AllowedRoles check, on any Command or CommandLevel, regardless of
	// what that list contains, see Authorized below. At most one role
	// across the whole manifest may set this; LoadRoles rejects a manifest
	// that sets it on more than one. This is what lets a deployment's very
	// first administrator account log in and start assigning ordinary
	// roles to everyone else, see example/var/tree/README.md's roles section for
	// the full bootstrap reasoning.
	Bypass bool `yaml:"bypass"`
}

// RoleSet - This type is the whole loaded, validated example/var/tree/roles.yaml
// manifest, every role a deployment has declared, indexed by name for fast
// lookup by Authorized and by whichever admin command validates a role
// name a session typed, see example/cmd/core/cmd_admin.go's "account roles add"
// and "account roles remove".
//
// Roles are flat and unordered a user may hold more than one, and a
// Command or CommandLevel's AllowedRoles check is satisfied by any overlap
// at all between the two, never by rank or hierarchy. See Authorized's doc
// comment for exactly how a check is decided.
type RoleSet struct {
	ByName map[string]*Role
	Order  []*Role // alphabetical by name, for anything that wants a stable listing

	// BypassRole is the Name of whichever role, if any, set bypass: true
	// in the manifest. Empty when no role in this deployment is marked
	// bypass. See Role.Bypass's doc comment.
	BypassRole string
}

// rolesFile - This type is the top-level shape of the roles manifest.
// Everything lives under a single "roles:" key, the same top-level key
// convention every other YAML file in this project already follows. Role
// itself decodes directly through its own yaml tags rather than through a
// separate mirror type.
type rolesFile struct {
	Roles map[string]Role `yaml:"roles"`
}

// ----------------------------------------------------------------------
// Public Functions - Roles
// ----------------------------------------------------------------------

// LoadRoles - This function reads a deployment's role declaration file.
//
// A missing file is not an error. It returns an empty RoleSet with no
// bypass role, which is correct for a deployment that never uses
// AllowedRoles anywhere.
//
// A file that exists but fails to parse, declares more than one bypass
// role, holds more than one YAML document, or sets an unknown key is a
// hard error.
func LoadRoles(path string) (*RoleSet, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &RoleSet{ByName: map[string]*Role{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading roles manifest %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var parsed rolesFile
	if err := dec.Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing roles manifest %s: %w", path, err)
	}

	var extra rolesFile
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("roles manifest %s contains multiple YAML documents", path)
		}
		return nil, fmt.Errorf("parsing roles manifest %s: %w", path, err)
	}

	byName := make(map[string]*Role, len(parsed.Roles))
	bypassRole := ""
	for name, r := range parsed.Roles {
		role := r
		role.Name = name
		if role.Bypass {
			if bypassRole != "" {
				return nil, fmt.Errorf("roles manifest %s marks more than one role bypass: true (%q and %q) - at most one role across the whole manifest may be the bypass role", path, bypassRole, name)
			}
			bypassRole = name
		}
		byName[name] = &role
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	order := make([]*Role, 0, len(names))
	for _, name := range names {
		order = append(order, byName[name])
	}

	return &RoleSet{ByName: byName, Order: order, BypassRole: bypassRole}, nil
}

// CurrentUserRoles - This function returns the roles the currently logged
// in session's account holds, from ctx.Users, or nil when there is no
// logged in session, no user database at all, meaning AuthRequired is off,
// or no matching entry for the session's username. A nil or empty result
// is not itself an error; whether that leaves a role gated command or
// level reachable at all is Authorized's decision, see its doc comment,
// not this function's.
func CurrentUserRoles(ctx *AppContext) []string {
	if ctx.Session == nil || !ctx.Session.Authenticated {
		return nil
	}
	if ctx.Users == nil {
		return nil
	}
	u := ctx.Users[ctx.Session.Username]
	if u == nil {
		return nil
	}
	return u.Roles
}

// Authorized - A role gate has to be checked in one place, or the answer
// could differ between running a command and entering a Command Level.
//
// This is that place. The read loop calls it for a Command, and
// EnterCommandLevel for a CommandLevel, both where the password is already
// checked.
//
// An empty allowedRoles means no gate, so every tree file that never sets
// it keeps working.
//
// Nothing is enforced while AuthRequired is false. With no login, no
// session has an identity, so there is no wrong role to refuse. RouterCLI
// is a library first: picked up with nothing configured, it should give a
// working command line, not lock a builder out of a level they just wrote.
//
// Otherwise a session passes when the user holds the bypass role, if one
// is declared, or any role in allowedRoles. Anything else is refused, the
// same deny by default that PasswordHash already follows.
func Authorized(ctx *AppContext, allowedRoles []string) bool {
	if len(allowedRoles) == 0 {
		return true
	}
	if !ctx.AuthRequired {
		return true
	}
	bypass := ""
	if ctx.Roles != nil {
		bypass = ctx.Roles.BypassRole
	}
	for _, r := range CurrentUserRoles(ctx) {
		if bypass != "" && r == bypass {
			return true
		}
		for _, a := range allowedRoles {
			if r == a {
				return true
			}
		}
	}
	return false
}
