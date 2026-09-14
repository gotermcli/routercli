// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"path/filepath"
	"testing"

	"github.com/gotermcli/routercli/auth"
)

// ----------------------------------------------------------------------
// LoadRoles
// ----------------------------------------------------------------------

// TestLoadRolesMissingFileReturnsEmptyRoleSet - This test verifies that a
// RolesFile that does not exist on disk at all is not an error, the
// correct state for a deployment that never uses AllowedRoles anywhere in
// its tree, see LoadRoles' own doc comment.
func TestLoadRolesMissingFileReturnsEmptyRoleSet(t *testing.T) {
	roles, err := LoadRoles(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles returned unexpected error for a missing file: %v", err)
	}
	if roles == nil {
		t.Fatal("expected a non-nil, empty RoleSet for a missing file")
	}
	if len(roles.ByName) != 0 {
		t.Errorf("expected an empty ByName map, got %v", roles.ByName)
	}
	if roles.BypassRole != "" {
		t.Errorf("expected an empty BypassRole, got %q", roles.BypassRole)
	}
}

// TestLoadRolesValid - This test verifies that a well-formed roles
// manifest loads every role, sets Name from the map key rather than from
// the YAML body, and identifies the one role marked bypass: true.
func TestLoadRolesValid(t *testing.T) {
	yamlBody := `
roles:
  admin:
    desc: "Full administrative access"
    bypass: true
  operator:
    desc: "Read only access to most commands"
`
	path := writeTempFile(t, "roles-*.yaml", yamlBody)
	roles, err := LoadRoles(path)
	if err != nil {
		t.Fatalf("LoadRoles returned unexpected error: %v", err)
	}
	if len(roles.ByName) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles.ByName))
	}
	admin, ok := roles.ByName["admin"]
	if !ok {
		t.Fatal("expected an \"admin\" role")
	}
	if admin.Name != "admin" {
		t.Errorf("admin.Name = %q, want %q", admin.Name, "admin")
	}
	if admin.Desc != "Full administrative access" {
		t.Errorf("admin.Desc = %q, want %q", admin.Desc, "Full administrative access")
	}
	if !admin.Bypass {
		t.Error("expected admin.Bypass to be true")
	}
	operator, ok := roles.ByName["operator"]
	if !ok {
		t.Fatal("expected an \"operator\" role")
	}
	if operator.Bypass {
		t.Error("expected operator.Bypass to be false")
	}
	if roles.BypassRole != "admin" {
		t.Errorf("roles.BypassRole = %q, want %q", roles.BypassRole, "admin")
	}
	if len(roles.Order) != 2 || roles.Order[0].Name != "admin" || roles.Order[1].Name != "operator" {
		t.Errorf("expected Order to be alphabetical [admin operator], got %v", roles.Order)
	}
}

// TestLoadRolesMoreThanOneBypassIsError - This test verifies that a
// manifest marking more than one role bypass: true is a hard startup
// error, since at most one role across the whole manifest may be the
// reserved bypass role.
func TestLoadRolesMoreThanOneBypassIsError(t *testing.T) {
	yamlBody := `
roles:
  admin:
    desc: "Full administrative access"
    bypass: true
  superuser:
    desc: "Also everything"
    bypass: true
`
	path := writeTempFile(t, "roles-*.yaml", yamlBody)
	_, err := LoadRoles(path)
	if err == nil {
		t.Fatal("expected an error for a manifest marking more than one role bypass: true, got nil")
	}
}

// TestLoadRolesUnknownFieldIsError - This test verifies that an
// unrecognized key in a role entry is a hard startup error, the same
// KnownFields(true) convention every other YAML file in this project
// already follows.
func TestLoadRolesUnknownFieldIsError(t *testing.T) {
	yamlBody := `
roles:
  admin:
    desc: "Full administrative access"
    bipass: true
`
	path := writeTempFile(t, "roles-*.yaml", yamlBody)
	_, err := LoadRoles(path)
	if err == nil {
		t.Fatal("expected an error for a misspelled field, got nil")
	}
}

// TestLoadRolesMultipleDocumentsIsError - This test verifies that a roles
// manifest containing more than one YAML document is rejected, matching
// config.LoadSystemConfig and auth.LoadUsers' own convention.
func TestLoadRolesMultipleDocumentsIsError(t *testing.T) {
	yamlBody := "roles:\n  admin:\n    desc: \"a\"\n---\nroles:\n  operator:\n    desc: \"b\"\n"
	path := writeTempFile(t, "roles-*.yaml", yamlBody)
	_, err := LoadRoles(path)
	if err == nil {
		t.Fatal("expected an error for a manifest with more than one YAML document, got nil")
	}
}

// TestLoadRolesEmptyFileReturnsEmptyRoleSet - This test verifies that a
// roles file containing nothing, or only comments, is valid, not an error,
// matching LoadTree's treatment of an empty tree file.
func TestLoadRolesEmptyFileReturnsEmptyRoleSet(t *testing.T) {
	path := writeTempFile(t, "roles-*.yaml", "# nothing but a comment\n")
	roles, err := LoadRoles(path)
	if err != nil {
		t.Fatalf("LoadRoles returned unexpected error for an empty file: %v", err)
	}
	if len(roles.ByName) != 0 {
		t.Errorf("expected an empty ByName map, got %v", roles.ByName)
	}
}

// ----------------------------------------------------------------------
// CurrentUserRoles
// ----------------------------------------------------------------------

// TestCurrentUserRolesReturnsSessionUsersRoles - This test verifies that
// CurrentUserRoles returns the logged in user's Roles slice.
func TestCurrentUserRolesReturnsSessionUsersRoles(t *testing.T) {
	ctx := &AppContext{
		Session: &auth.Session{Username: "alice", Authenticated: true},
		Users: auth.Users{
			"alice": {Username: "alice", Roles: []string{"operator", "auditor"}},
		},
	}
	roles := CurrentUserRoles(ctx)
	if len(roles) != 2 || roles[0] != "operator" || roles[1] != "auditor" {
		t.Errorf("CurrentUserRoles = %v, want [operator auditor]", roles)
	}
}

// TestCurrentUserRolesNilWhenNotAuthenticated - This test verifies that a
// session that has never logged in, Authenticated false, never yields any
// roles, matching how a deployment with AuthRequired off behaves. That
// means such a deployment can never satisfy an AllowedRoles gate at all.
func TestCurrentUserRolesNilWhenNotAuthenticated(t *testing.T) {
	ctx := &AppContext{
		Session: &auth.Session{Username: "alice", Authenticated: false},
		Users: auth.Users{
			"alice": {Username: "alice", Roles: []string{"operator"}},
		},
	}
	if roles := CurrentUserRoles(ctx); roles != nil {
		t.Errorf("expected nil roles for an unauthenticated session, got %v", roles)
	}
}

// TestCurrentUserRolesNilWhenNoUserDatabase - This test verifies that a
// nil ctx.Users, meaning no UsersFile was ever loaded, never panics and
// yields no roles.
func TestCurrentUserRolesNilWhenNoUserDatabase(t *testing.T) {
	ctx := &AppContext{
		Session: &auth.Session{Username: "alice", Authenticated: true},
	}
	if roles := CurrentUserRoles(ctx); roles != nil {
		t.Errorf("expected nil roles when ctx.Users is nil, got %v", roles)
	}
}

// TestCurrentUserRolesNilWhenNoMatchingAccount - This test verifies that a
// session whose own username has no matching entry in ctx.Users, an edge
// case rather than something that should happen in practice, still returns
// nil rather than panicking.
func TestCurrentUserRolesNilWhenNoMatchingAccount(t *testing.T) {
	ctx := &AppContext{
		Session: &auth.Session{Username: "ghost", Authenticated: true},
		Users:   auth.Users{"alice": {Username: "alice", Roles: []string{"operator"}}},
	}
	if roles := CurrentUserRoles(ctx); roles != nil {
		t.Errorf("expected nil roles for a username with no matching account, got %v", roles)
	}
}

// ----------------------------------------------------------------------
// Authorized
// ----------------------------------------------------------------------

// TestAuthorizedEmptyAllowedRolesAlwaysTrue - This test verifies that a
// Command or CommandLevel that never sets AllowedRoles at all is never
// gated by this mechanism, keeping every existing tree file, none of which
// sets AllowedRoles, working completely unchanged.
func TestAuthorizedEmptyAllowedRolesAlwaysTrue(t *testing.T) {
	ctx := &AppContext{}
	if !Authorized(ctx, nil) {
		t.Error("expected Authorized to return true for a nil AllowedRoles list")
	}
	if !Authorized(ctx, []string{}) {
		t.Error("expected Authorized to return true for an empty AllowedRoles list")
	}
}

// TestAuthorizedGrantsOnOverlap - This test verifies that a session
// holding any one role in common with AllowedRoles is authorized, the "any
// overlap" rule, not requiring every role to match.
func TestAuthorizedGrantsOnOverlap(t *testing.T) {
	ctx := &AppContext{
		AuthRequired: true,
		Session:      &auth.Session{Username: "alice", Authenticated: true},
		Users: auth.Users{
			"alice": {Username: "alice", Roles: []string{"operator", "auditor"}},
		},
	}
	if !Authorized(ctx, []string{"admin", "auditor"}) {
		t.Error("expected Authorized to return true for a session holding one of several allowed roles")
	}
}

// TestAuthorizedDeniesNoOverlap - This test verifies deny by default: a
// session holding roles, just none that overlap with AllowedRoles, is
// refused, the same fail-closed convention PasswordHash already follows
// for a wrong or missing credential, once this deployment has AuthRequired
// turned on. See TestAuthorizedGrantsWhenAuthRequiredOff for the
// different, wide open outcome while it is off.
func TestAuthorizedDeniesNoOverlap(t *testing.T) {
	ctx := &AppContext{
		AuthRequired: true,
		Session:      &auth.Session{Username: "alice", Authenticated: true},
		Users: auth.Users{
			"alice": {Username: "alice", Roles: []string{"operator"}},
		},
	}
	if Authorized(ctx, []string{"admin"}) {
		t.Error("expected Authorized to return false for a session with no overlapping role")
	}
}

// TestAuthorizedDeniesNoRoles - This test verifies that a session holding
// no roles at all is refused by a role gated command or level, once this
// deployment has AuthRequired turned on.
func TestAuthorizedDeniesNoRoles(t *testing.T) {
	ctx := &AppContext{
		AuthRequired: true,
		Session:      &auth.Session{Username: "alice", Authenticated: true},
		Users:        auth.Users{"alice": {Username: "alice"}},
	}
	if Authorized(ctx, []string{"admin"}) {
		t.Error("expected Authorized to return false for a session holding no roles")
	}
}

// TestAuthorizedBypassRoleAlwaysGranted - This test verifies the bootstrap
// mechanism: a session holding the deployment's BypassRole passes every
// AllowedRoles check regardless of what that list contains.
func TestAuthorizedBypassRoleAlwaysGranted(t *testing.T) {
	ctx := &AppContext{
		AuthRequired: true,
		Session:      &auth.Session{Username: "alice", Authenticated: true},
		Users: auth.Users{
			"alice": {Username: "alice", Roles: []string{"admin"}},
		},
		Roles: &RoleSet{BypassRole: "admin"},
	}
	if !Authorized(ctx, []string{"some-completely-unrelated-role"}) {
		t.Error("expected a session holding the bypass role to be authorized regardless of AllowedRoles")
	}
}

// TestAuthorizedGrantsWhenAuthRequiredOff - This test verifies the out of
// the box, zero setup behavior this project's design favors: a deployment
// that never turned AuthRequired on has no real session identity anywhere,
// see CurrentUserRoles, and Authorized treats that as nothing to refuse,
// not a wrong role, so every AllowedRoles gate stays wide open until
// AuthRequired is turned on.
func TestAuthorizedGrantsWhenAuthRequiredOff(t *testing.T) {
	ctx := &AppContext{}
	if !Authorized(ctx, []string{"admin"}) {
		t.Error("expected Authorized to return true with AuthRequired off, regardless of AllowedRoles")
	}
}
