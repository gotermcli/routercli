// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/config"
)

// State - This type is the daemon's canonical shared state, the concrete
// type a deployment's Store holds. Every field here is genuinely shared.
//
// Nothing per session belongs here. A Session, a Position, a Negated flag
// all stay on each CLI session's AppContext, outside this package.
//
// State has no locking of its own. Every field is only safe to read or
// write from inside a function passed to Store.Do. Nothing enforces that
// from the type system, the same way sync.Mutex does not stop a caller
// touching what it protects without holding it. It is a convention this
// package's tests hold to, not a compiler guarantee.
//
// A zero State is not useful; construct one with NewState.
type State struct {
	// ProductState holds whatever a deployment's product package considers
	// its running configuration, hostname, interfaces, description,
	// banners, line defaults, and so on for this project's cmd/product,
	// kept fully opaque here, an any, exactly matching
	// command.AppContext.State's existing genericity. This package never
	// reads or writes through it itself; wiring a concrete product state
	// type up to real mutations is not this package's concern.
	ProductState any

	// Levels holds every Command Level's definition, including each
	// level's runtime defined Aliases map and its own
	// PasswordHash/VendorDefinedPasswordHash, moved here from
	// command.AppContext.Levels. A nil Levels is not valid state for a
	// real deployment, the same way a nil *command.TreeStructure is never
	// valid on AppContext today, but NewState does not itself refuse a nil
	// value, leaving that validation to whatever loads a real deployment's
	// tree structure before constructing a Store around this State.
	Levels *command.TreeStructure

	// Users holds every account this deployment knows about, moved here
	// from what each CLI process today loads independently from UsersFile
	// at its own startup.
	Users auth.Users

	// Roles holds this deployment's role declarations, moved here from
	// what each CLI process today loads independently from RolesFile at
	// its own startup.
	Roles *command.RoleSet

	// Config holds the deployment wide settings this daemon loaded once at
	// startup, AuthRequired among them, handed to every attaching session
	// from here rather than each CLI process reading the file itself.
	//
	// A deployment treats this as read only after startup, replacing it
	// wholesale on reboot rather than mutating fields in place.
	Config *config.SystemConfig
}

// NewState - This function returns a State built from the pieces a daemon
// startup, or a test, assembles separately: product state, a loaded tree
// structure, a loaded user set, a loaded role set, and the system
// configuration.
//
// It does no loading and no validation beyond what its callers already
// did. It exists so building a State reads as one step rather than five
// field assignments repeated at every call site.
func NewState(productState any, levels *command.TreeStructure, users auth.Users, roles *command.RoleSet, cfg *config.SystemConfig) State {
	return State{
		ProductState: productState,
		Levels:       levels,
		Users:        users,
		Roles:        roles,
		Config:       cfg,
	}
}
