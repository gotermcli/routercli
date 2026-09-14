// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/i18n"
	"github.com/gotermcli/routercli/paging"
)

// ----------------------------------------------------------------------
// Fixtures
// ----------------------------------------------------------------------

// adminExecLevels - This function builds a minimal *command.TreeStructure
// with "base", "exec", and "admin" levels, admin parented from exec, the
// same shape example/var/tree/tree_structure.yaml declares, for tests exercising
// the registered "admin" and "return.admin" handlers without loading real
// YAML files.
func adminExecLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"base":  {Name: "base", Tree: map[string]*command.Command{}},
		"exec":  {Name: "exec", Parent: "base", PromptSuffix: "#", Tree: map[string]*command.Command{}},
		"admin": {Name: "admin", Parent: "exec", PromptSuffix: "(admin)", Tree: map[string]*command.Command{}},
	}}
}

// newAdminTestContext - This function builds a *command.AppContext
// suitable for exercising the account and erase/reload/restore handlers
// directly: a real, on-disk UsersFile seeded with u under username, a
// matching, freshly loaded ctx.Users, and every other field those handlers
// read.
func newAdminTestContext(t *testing.T, username string, u *auth.User) *command.AppContext {
	t.Helper()
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: username, Authenticated: true}
	ctx.Users = auth.Users{username: u}
	ctx.UsersFile = filepath.Join(t.TempDir(), "users.yaml")
	if err := auth.SaveUsers(ctx.UsersFile, ctx.Users); err != nil {
		t.Fatalf("failed to seed UsersFile: %v", err)
	}
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 1}
	ctx.RolesFile = filepath.Join(t.TempDir(), "roles.yaml")
	ctx.Roles = RolesFixture()
	ctx.DefaultsDir = t.TempDir()
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config-does-not-exist")
	// rewireDaemonClient shares this exact Users map, and Roles struct,
	// with ctx.DaemonClient's Store, so a handler migrated to MutateUsers,
	// "account create" and "account delete" among them, sees and modifies
	// the same ctx.Users a test asserts against afterward. See
	// rewireDaemonClient's doc comment in testhelpers_test.go.
	rewireDaemonClient(ctx)
	return ctx
}

// RolesFixture - This function returns a *command.RoleSet declaring two
// roles, "admin", marked bypass: true, and "operator", the same shape this
// project's shipped example/var/tree/roles.yaml declares.
func RolesFixture() *command.RoleSet {
	return &command.RoleSet{
		ByName: map[string]*command.Role{
			"admin":    {Name: "admin", Bypass: true},
			"operator": {Name: "operator"},
		},
		BypassRole: "admin",
	}
}

// ----------------------------------------------------------------------
//
// admin, return.admin
//
// ----------------------------------------------------------------------

// TestAdminHandlerEntersAdminFromExec - This test verifies that the
// registered "admin" handler moves a session from exec into admin,
// mirroring TestEnableHandlerEntersExecFromBase in cmd_enable_test.go.
func TestAdminHandlerEntersAdminFromExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "admin")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("admin handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "admin" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "admin")
	}
}

// TestAdminHandlerAlreadyHereIsNoOp - This test verifies that running
// "admin" again while already there is a no-op, no error.
func TestAdminHandlerAlreadyHereIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "admin"}
	ctx.Position = command.NewCommandLevelStack("admin", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "admin")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("admin handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "admin" {
		t.Errorf("Session.CommandLevel = %q, want %q (unchanged)", ctx.Session.CommandLevel, "admin")
	}
}

// TestReturnAdminHandlerReturnsToExec - This test verifies that the
// registered "return.admin" handler moves a session back from admin to
// exec.
func TestReturnAdminHandlerReturnsToExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "admin"}
	ctx.Position = command.NewCommandLevelStack("admin", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "return.admin")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("return.admin handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
}

// TestReturnAdminHandlerNotHereIsNoOp - This test verifies that running
// "return.admin" while not currently in admin is a no-op, no error.
func TestReturnAdminHandlerNotHereIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "return.admin")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("return.admin handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q (unchanged)", ctx.Session.CommandLevel, "exec")
	}
}

// ----------------------------------------------------------------------
//
// account create
//
// ----------------------------------------------------------------------

// TestRunAccountCreateWithIOInteractivePrompt - This test verifies the
// bare "account create <username>" shape: it prompts interactively,
// masked, twice, and the new account ends up with a working password hash
// and MustChangePassword set true.
func TestRunAccountCreateWithIOInteractivePrompt(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runAccountCreateWithIO(ctx, int(slave.Fd()), slave, []string{"bob"})
	})

	sendLine(t, master, "n3wpassword")
	sendLine(t, master, "n3wpassword")

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runAccountCreateWithIO returned unexpected error: %v", err)
	}
	bob, ok := ctx.Users["bob"]
	if !ok {
		t.Fatal("expected account \"bob\" to have been created")
	}
	if !auth.VerifyPassword(bob.PasswordHash, "n3wpassword") {
		t.Error("expected bob's password hash to verify against the typed password")
	}
	if !bob.MustChangePassword {
		t.Error("expected MustChangePassword to be true after an interactively prompted password")
	}
}

// TestRunAccountCreateWithIOMismatchedConfirmation - This test verifies
// that a typed password and confirmation that do not match leaves the
// account uncreated and returns an error.
func TestRunAccountCreateWithIOMismatchedConfirmation(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runAccountCreateWithIO(ctx, int(slave.Fd()), slave, []string{"bob"})
	})

	sendLine(t, master, "n3wpassword")
	sendLine(t, master, "different")

	if err := awaitHandler(t, resCh, 5*time.Second); err == nil {
		t.Fatal("expected an error for a mismatched password confirmation")
	}
	if _, ok := ctx.Users["bob"]; ok {
		t.Error("expected account \"bob\" not to have been created after a mismatched confirmation")
	}
}

// TestRunAccountCreateGeneratesPassword - This test verifies "account
// create <username> generate": the new account gets a working, randomly
// generated password meeting ctx.PasswordPolicy and MustChangePassword set
// true. The generated password itself is printed once through the package
// level fmt.Println, not through the injected stdout, the same convention
// runTOTPEnable's secret follows; reading it back out of captured stdout
// is left to the pty based interactive smoke test, the same as that
// convention already accepts for TOTP enrollment.
func TestRunAccountCreateGeneratesPassword(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 12, RequireUppercase: true, RequireNumbers: true, RequireSpecialChars: true}

	var stdout bytes.Buffer
	_, cerr := paging.CaptureOutput(func() {
		if err := runAccountCreateWithIO(ctx, int(os.Stdin.Fd()), &stdout, []string{"carol", "generate"}); err != nil {
			t.Errorf("runAccountCreateWithIO returned unexpected error: %v", err)
		}
	})
	if cerr != nil {
		t.Fatalf("paging.CaptureOutput returned error: %v", cerr)
	}

	carol, ok := ctx.Users["carol"]
	if !ok {
		t.Fatal("expected account \"carol\" to have been created")
	}
	if !carol.MustChangePassword {
		t.Error("expected MustChangePassword to be true for a generated password")
	}
	if carol.PasswordHash == "" || !auth.IsRecognizedHash(carol.PasswordHash) {
		t.Errorf("expected a real, recognized password hash, got %q", carol.PasswordHash)
	}
}

// TestRunAccountCreateImportsHash - This test verifies "account create
// <username> hash <hash>": the imported hash is stored verbatim and
// MustChangePassword is left false, since an imported hash is presumed to
// already be the real intended credential.
func TestRunAccountCreateImportsHash(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	hash, err := auth.HashPassword("preexisting")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}

	var stdout bytes.Buffer
	if err := runAccountCreateWithIO(ctx, int(os.Stdin.Fd()), &stdout, []string{"dave", "hash", hash}); err != nil {
		t.Fatalf("runAccountCreateWithIO returned unexpected error: %v", err)
	}

	dave, ok := ctx.Users["dave"]
	if !ok {
		t.Fatal("expected account \"dave\" to have been created")
	}
	if dave.PasswordHash != hash {
		t.Errorf("dave.PasswordHash = %q, want the imported hash %q verbatim", dave.PasswordHash, hash)
	}
	if dave.MustChangePassword {
		t.Error("expected MustChangePassword to be false for an imported hash")
	}
}

// TestRunAccountCreateRejectsUnrecognizedHash - This test verifies that
// "account create <username> hash <value>" refuses a value that is not a
// recognized "$id$encoded" hash.
func TestRunAccountCreateRejectsUnrecognizedHash(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	var stdout bytes.Buffer
	err := runAccountCreateWithIO(ctx, int(os.Stdin.Fd()), &stdout, []string{"dave", "hash", "not-a-real-hash"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized hash value")
	}
	if _, ok := ctx.Users["dave"]; ok {
		t.Error("expected account \"dave\" not to have been created")
	}
}

// TestRunAccountCreateRejectsExistingUsername - This test verifies that
// "account create" refuses a username that already exists.
func TestRunAccountCreateRejectsExistingUsername(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	var stdout bytes.Buffer
	err := runAccountCreateWithIO(ctx, int(os.Stdin.Fd()), &stdout, []string{"admin", "hash", "$0$anything"})
	if err == nil {
		t.Fatal("expected an error creating an account with a username that already exists")
	}
}

// ----------------------------------------------------------------------
//
// account delete
//
// ----------------------------------------------------------------------

// TestRunAccountDeleteRemovesAccount - This test verifies the ordinary
// success path: deleting an account that is not the last holder of the
// bypass role removes it from ctx.Users and persists that to disk.
func TestRunAccountDeleteRemovesAccount(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.Users["bob"] = &auth.User{Username: "bob", PasswordHash: "$0$x"}
	if err := auth.SaveUsers(ctx.UsersFile, ctx.Users); err != nil {
		t.Fatalf("failed to seed users file: %v", err)
	}

	if err := runAccountDelete(ctx, []string{"bob"}); err != nil {
		t.Fatalf("runAccountDelete returned unexpected error: %v", err)
	}
	if _, ok := ctx.Users["bob"]; ok {
		t.Error("expected account \"bob\" to have been deleted")
	}
}

// TestRunAccountDeleteNotFound - This test verifies that deleting a
// username with no matching account is refused with an error.
func TestRunAccountDeleteNotFound(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runAccountDelete(ctx, []string{"ghost"}); err == nil {
		t.Fatal("expected an error deleting a nonexistent account")
	}
}

// TestRunAccountDeleteRefusesLastBypassRoleHolder - This test verifies the
// safety rail: deleting the one remaining account that holds the
// deployment's bypass role is refused, closing off a deployment locking
// itself out of admin entirely.
func TestRunAccountDeleteRefusesLastBypassRoleHolder(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	err := runAccountDelete(ctx, []string{"admin"})
	if err == nil {
		t.Fatal("expected an error deleting the last account holding the bypass role")
	}
	if _, ok := ctx.Users["admin"]; !ok {
		t.Error("expected the last bypass role holder to remain after a refused delete")
	}
}

// TestRunAccountDeleteAllowsNonLastBypassRoleHolder - This test verifies
// that deleting an account holding the bypass role is allowed as long as
// it is not the only one left holding it.
func TestRunAccountDeleteAllowsNonLastBypassRoleHolder(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.Users["carol"] = &auth.User{Username: "carol", PasswordHash: "$0$x", Roles: []string{"admin"}}
	if err := auth.SaveUsers(ctx.UsersFile, ctx.Users); err != nil {
		t.Fatalf("failed to seed users file: %v", err)
	}

	if err := runAccountDelete(ctx, []string{"admin"}); err != nil {
		t.Fatalf("runAccountDelete returned unexpected error: %v", err)
	}
	if _, ok := ctx.Users["admin"]; ok {
		t.Error("expected account \"admin\" to have been deleted since carol also holds the bypass role")
	}
}

// ----------------------------------------------------------------------
//
// account roles add, account roles remove
//
// ----------------------------------------------------------------------

// TestRunAccountRolesAddAssignsRole - This test verifies the ordinary
// success path for "account roles add".
func TestRunAccountRolesAddAssignsRole(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.Users["bob"] = &auth.User{Username: "bob", PasswordHash: "$0$x"}
	if err := auth.SaveUsers(ctx.UsersFile, ctx.Users); err != nil {
		t.Fatalf("failed to seed users file: %v", err)
	}

	if err := runAccountRolesAdd(ctx, []string{"bob", "operator"}); err != nil {
		t.Fatalf("runAccountRolesAdd returned unexpected error: %v", err)
	}
	if !hasRole(ctx.Users["bob"], "operator") {
		t.Error("expected bob to hold the \"operator\" role")
	}
}

// TestRunAccountRolesAddRejectsUnknownRole - This test verifies that
// assigning a role name not declared in roles.yaml is refused outright,
// rather than silently accepted and left to never match any AllowedRoles
// check.
func TestRunAccountRolesAddRejectsUnknownRole(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.Users["bob"] = &auth.User{Username: "bob", PasswordHash: "$0$x"}

	if err := runAccountRolesAdd(ctx, []string{"bob", "ghost-role"}); err == nil {
		t.Fatal("expected an error assigning an undeclared role")
	}
}

// TestRunAccountRolesAddRejectsUnknownAccount - This test verifies that
// assigning a role to a username with no matching account is refused.
func TestRunAccountRolesAddRejectsUnknownAccount(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runAccountRolesAdd(ctx, []string{"ghost", "operator"}); err == nil {
		t.Fatal("expected an error assigning a role to a nonexistent account")
	}
}

// TestRunAccountRolesAddAlreadyHasIsNoOp - This test verifies that
// assigning a role the account already holds is a no-op, not an error and
// not a duplicate entry.
func TestRunAccountRolesAddAlreadyHasIsNoOp(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runAccountRolesAdd(ctx, []string{"admin", "admin"}); err != nil {
		t.Fatalf("runAccountRolesAdd returned unexpected error: %v", err)
	}
	if len(ctx.Users["admin"].Roles) != 1 {
		t.Errorf("expected no duplicate role entry, got %v", ctx.Users["admin"].Roles)
	}
}

// TestRunAccountRolesRemoveRemovesRole - This test verifies the ordinary
// success path for "account roles remove", removing a role that is not the
// account's only hold on the bypass role.
func TestRunAccountRolesRemoveRemovesRole(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin", "operator"}})

	if err := runAccountRolesRemove(ctx, []string{"admin", "operator"}); err != nil {
		t.Fatalf("runAccountRolesRemove returned unexpected error: %v", err)
	}
	if hasRole(ctx.Users["admin"], "operator") {
		t.Error("expected the \"operator\" role to have been removed")
	}
}

// TestRunAccountRolesRemoveDoesNotHaveIsNoOp - This test verifies that
// removing a role the account does not hold is a no-op, not an error.
func TestRunAccountRolesRemoveDoesNotHaveIsNoOp(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runAccountRolesRemove(ctx, []string{"admin", "operator"}); err != nil {
		t.Fatalf("runAccountRolesRemove returned unexpected error: %v", err)
	}
}

// TestRunAccountRolesRemoveRefusesLastBypassRoleHolder - This test
// verifies that removing the bypass role from its own last remaining
// holder is refused, exactly as unrecoverable as "account delete" would be
// in the same situation.
func TestRunAccountRolesRemoveRefusesLastBypassRoleHolder(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	err := runAccountRolesRemove(ctx, []string{"admin", "admin"})
	if err == nil {
		t.Fatal("expected an error removing the bypass role from its last remaining holder")
	}
	if !hasRole(ctx.Users["admin"], "admin") {
		t.Error("expected the bypass role to remain after a refused removal")
	}
}

// ----------------------------------------------------------------------
//
// erase users, restore-factory-defaults, reload
//
// ----------------------------------------------------------------------

// TestRunEraseUsersRestoresFromDefaults - This test verifies that "erase
// users" replaces the live UsersFile with DefaultsDir's skeleton copy and
// reloads ctx.Users from it.
func TestRunEraseUsersRestoresFromDefaults(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	defaultHash, err := auth.HashPassword("changeme")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	defaultsBody := "users:\n  admin:\n    password: \"" + defaultHash + "\"\n    roles: [admin]\n    must_change_password: true\n"
	defPath := filepath.Join(ctx.DefaultsDir, filepath.Base(ctx.UsersFile))
	if err := os.WriteFile(defPath, []byte(defaultsBody), 0640); err != nil {
		t.Fatalf("failed to seed default users.yaml: %v", err)
	}

	if err := runEraseUsers(ctx, nil); err != nil {
		t.Fatalf("runEraseUsers returned unexpected error: %v", err)
	}
	restored, ok := ctx.Users["admin"]
	if !ok {
		t.Fatal("expected the restored users.yaml to still have an \"admin\" account")
	}
	if !restored.MustChangePassword {
		t.Error("expected the restored account to carry MustChangePassword")
	}
}

// TestRunEraseUsersNoDefaultsIsNotAnError - This test verifies that "erase
// users" is a no-op, not an error, when DefaultsDir has no matching
// skeleton file at all, and that ctx.Users is left untouched.
func TestRunEraseUsersNoDefaultsIsNotAnError(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runEraseUsers(ctx, nil); err != nil {
		t.Fatalf("runEraseUsers returned unexpected error: %v", err)
	}
	if _, ok := ctx.Users["admin"]; !ok {
		t.Error("expected ctx.Users to be left untouched when no default file exists")
	}
}

// TestRunRebootEndsTheConnection - This test verifies that "reboot", and
// its "reload" alias, re-reads ctx.UsersFile and ctx.RolesFile from disk
// and then returns command.ErrQuit, the sentinel that ends the current
// connection.
func TestRunRebootEndsTheConnection(t *testing.T) {
	// auth.LoadUsers hard-rejects any account with an empty PasswordHash,
	// see auth.User's doc comment, so the fixture account needs a real
	// hash on disk for runReboot's auth.LoadUsers call to succeed, unlike
	// most other tests in this file that never reread UsersFile from disk
	// at all.
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", PasswordHash: hash, Roles: []string{"admin"}})

	err = runReboot(ctx, nil)
	if err != command.ErrQuit {
		t.Fatalf("runReboot returned %v, want command.ErrQuit", err)
	}
	if _, ok := ctx.Users["admin"]; !ok {
		t.Error("expected ctx.Users to still have \"admin\" after reload re-read it from disk")
	}
}

// TestRunRebootWithRealDaemonAsksTheDaemonInstead - This test
// verifies that "reboot", with no args and not negated, asks
// ctx.DaemonClient.Reboot rather than doing today's standalone reread
// itself, once a fakeDaemonClient stands in for a real daemon connection,
// and returns nil rather than command.ErrQuit, since this session's ending
// now arrives asynchronously through its own FarewellChannel instead.
// ctx.Users is left untouched, confirming this path skipped reloadFromDisk
// rather than merely happening to also succeed at it.
func TestRunRebootWithRealDaemonAsksTheDaemonInstead(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	fake := &fakeDaemonClient{}
	ctx.DaemonClient = fake
	before := ctx.Users["admin"]

	if err := runReboot(ctx, nil); err != nil {
		t.Fatalf("runReboot returned %v, want nil", err)
	}
	if fake.RebootCalls != 1 {
		t.Errorf("DaemonClient.Reboot was called %d times, want 1", fake.RebootCalls)
	}
	if ctx.Users["admin"] != before {
		t.Error("expected ctx.Users to be left untouched, reloadFromDisk should not have run")
	}
}

// TestRunRebootWithRealDaemonFailurePropagatesTheError - This test
// verifies that a genuine failure reported by ctx.DaemonClient.Reboot,
// anything other than command.ErrDaemonNotConfigured, is reported back to
// whoever typed "reload" or "reboot" rather than silently falling back to
// the standalone reread path, which would hide a real daemon side failure
// behind an apparently successful local reload.
func TestRunRebootWithRealDaemonFailurePropagatesTheError(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.DaemonClient = &fakeDaemonClient{RebootErr: errors.New("daemon: reread failed")}

	if err := runReboot(ctx, nil); err == nil {
		t.Fatal("expected runReboot to return an error when the daemon's Reboot call fails")
	}
}

// TestRunRebootWithDelaySchedulesRatherThanEndingTheConnection - This test
// verifies that "reload <seconds>" arms ctx.ReloadScheduler and returns
// nil, rather than reloading immediately, leaving the current connection
// running until either the delay elapses or "no reload" cancels it.
func TestRunRebootWithDelaySchedulesRatherThanEndingTheConnection(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.ReloadScheduler = command.NewPendingReload()
	defer ctx.ReloadScheduler.Cancel()

	if err := runReboot(ctx, []string{"5"}); err != nil {
		t.Fatalf("runReboot returned unexpected error: %v", err)
	}
	if !ctx.ReloadScheduler.Pending() {
		t.Error("expected ctx.ReloadScheduler.Pending() == true after \"reload 5\"")
	}
}

// TestRunRebootWithInvalidDelayReturnsError - This test verifies that
// "reload <seconds>" refuses a delay that is not a positive integer, zero,
// negative, and non-numeric alike, without touching ctx.ReloadScheduler at
// all.
func TestRunRebootWithInvalidDelayReturnsError(t *testing.T) {
	for _, delay := range []string{"0", "-5", "soon"} {
		ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
		ctx.ReloadScheduler = command.NewPendingReload()

		if err := runReboot(ctx, []string{delay}); err == nil {
			t.Errorf("runReboot(%q) returned nil error, want an error", delay)
		}
		if ctx.ReloadScheduler.Pending() {
			t.Errorf("runReboot(%q) left a reload pending, want nothing scheduled", delay)
		}
	}
}

// TestRunRebootWithDelayButNoSchedulerReturnsError - This test verifies
// that "reload <seconds>" fails cleanly, rather than panicking on a nil
// ctx.ReloadScheduler, for a caller that never wired one up, main.go's
// AppContext construction being the only real one that does.
func TestRunRebootWithDelayButNoSchedulerReturnsError(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := runReboot(ctx, []string{"5"}); err == nil {
		t.Error("expected an error when ctx.ReloadScheduler is nil")
	}
}

// TestRunRebootNegatedCancelsThePendingReboot - This test verifies that "no
// reload", ctx.Negated set true, cancels a reload "reload <seconds>" had
// scheduled, returning nil and leaving nothing pending.
func TestRunRebootNegatedCancelsThePendingReboot(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.ReloadScheduler = command.NewPendingReload()
	ctx.ReloadScheduler.Schedule(time.Hour)

	ctx.Negated = true
	if err := runReboot(ctx, nil); err != nil {
		t.Fatalf("negated runReboot returned unexpected error: %v", err)
	}
	if ctx.ReloadScheduler.Pending() {
		t.Error("expected nothing pending after \"no reload\" cancelled it")
	}
}

// TestRunRebootNegatedWithNothingPendingReturnsError - This test verifies
// that "no reload" reports an error, matching
// command.PendingReload.Cancel's documented behavior, rather than silently
// succeeding, when nothing was scheduled.
func TestRunRebootNegatedWithNothingPendingReturnsError(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.ReloadScheduler = command.NewPendingReload()

	ctx.Negated = true
	if err := runReboot(ctx, nil); err == nil {
		t.Error("expected an error from \"no reload\" when nothing was pending")
	}
}

// TestRunRestoreFactoryDefaultsRestoresAndReloads - This test verifies the
// full recovery path: startup-config is erased, both users.yaml and
// roles.yaml are restored from DefaultsDir, and the command ends by
// returning command.ErrQuit, the same as "reload".
func TestRunRestoreFactoryDefaultsRestoresAndReloads(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	if err := os.WriteFile(ctx.StartupConfigFile, []byte("hostname somehost\n"), 0640); err != nil {
		t.Fatalf("failed to seed startup-config: %v", err)
	}

	defaultHash, err := auth.HashPassword("changeme")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	usersBody := "users:\n  admin:\n    password: \"" + defaultHash + "\"\n    roles: [admin]\n    must_change_password: true\n"
	if err := os.WriteFile(filepath.Join(ctx.DefaultsDir, filepath.Base(ctx.UsersFile)), []byte(usersBody), 0640); err != nil {
		t.Fatalf("failed to seed default users.yaml: %v", err)
	}
	rolesBody := "roles:\n  admin:\n    desc: \"Full administrative access\"\n    bypass: true\n"
	if err := os.WriteFile(filepath.Join(ctx.DefaultsDir, filepath.Base(ctx.RolesFile)), []byte(rolesBody), 0640); err != nil {
		t.Fatalf("failed to seed default roles.yaml: %v", err)
	}

	err = runRestoreFactoryDefaults(ctx, nil)
	if err != command.ErrQuit {
		t.Fatalf("runRestoreFactoryDefaults returned %v, want command.ErrQuit", err)
	}
	if _, statErr := os.Stat(ctx.StartupConfigFile); !os.IsNotExist(statErr) {
		t.Error("expected startup-config to have been erased")
	}
	restoredUsers, err := auth.LoadUsers(ctx.UsersFile)
	if err != nil {
		t.Fatalf("auth.LoadUsers after restore: %v", err)
	}
	if _, ok := restoredUsers["admin"]; !ok {
		t.Error("expected the restored users.yaml to have an \"admin\" account")
	}
	restoredRoles, err := command.LoadRoles(ctx.RolesFile)
	if err != nil {
		t.Fatalf("command.LoadRoles after restore: %v", err)
	}
	if restoredRoles.BypassRole != "admin" {
		t.Errorf("restoredRoles.BypassRole = %q, want %q", restoredRoles.BypassRole, "admin")
	}
}

// TestRunRestoreFactoryDefaultsLeavesVendorConfigFileAlone - This test
// verifies that "restore-factory-defaults" never touches
// ctx.VendorConfigFile, the same guarantee
// TestEraseStartupConfigLeavesVendorConfigFileAlone in
// example/cmd/product/cmd_startup_config_test.go already locks down for "erase
// startup-config" alone. A VendorRestricted level's state must survive
// every path this project ships back to a genuine factory reset, not just
// the more direct one.
func TestRunRestoreFactoryDefaultsLeavesVendorConfigFileAlone(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	ctx.VendorConfigFile = filepath.Join(t.TempDir(), "vendor-config")
	if err := os.WriteFile(ctx.VendorConfigFile, []byte("! (vendor-config)\npassword manager hash $6$$vendorhash\n!\n"), 0640); err != nil {
		t.Fatalf("failed to seed vendor-config file: %v", err)
	}

	// reloadFromDisk, which runRestoreFactoryDefaults ends by calling,
	// needs a real UsersFile and RolesFile to read back, the same seeding
	// TestRunRestoreFactoryDefaultsRestoresAndReloads above already needs
	// for that same reason; this test is not about that part of the flow,
	// only about VendorConfigFile surviving it.
	defaultHash, err := auth.HashPassword("changeme")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	usersBody := "users:\n  admin:\n    password: \"" + defaultHash + "\"\n    roles: [admin]\n"
	if err := os.WriteFile(filepath.Join(ctx.DefaultsDir, filepath.Base(ctx.UsersFile)), []byte(usersBody), 0640); err != nil {
		t.Fatalf("failed to seed default users.yaml: %v", err)
	}
	rolesBody := "roles:\n  admin:\n    desc: \"Full administrative access\"\n    bypass: true\n"
	if err := os.WriteFile(filepath.Join(ctx.DefaultsDir, filepath.Base(ctx.RolesFile)), []byte(rolesBody), 0640); err != nil {
		t.Fatalf("failed to seed default roles.yaml: %v", err)
	}

	err = runRestoreFactoryDefaults(ctx, nil)
	if err != command.ErrQuit {
		t.Fatalf("runRestoreFactoryDefaults returned %v, want command.ErrQuit", err)
	}
	if _, statErr := os.Stat(ctx.VendorConfigFile); statErr != nil {
		t.Errorf("expected vendor-config to survive restore-factory-defaults untouched, stat returned: %v", statErr)
	}
}

// ----------------------------------------------------------------------
//
// generatePassword
//
// ----------------------------------------------------------------------

// TestGeneratePasswordSatisfiesPolicy - This test verifies that a
// generated password always satisfies the PasswordPolicy it was generated
// for, across every required character class at once, checked with
// auth.ValidatePassword itself rather than reimplementing the same rules
// here.
func TestGeneratePasswordSatisfiesPolicy(t *testing.T) {
	policy := auth.PasswordPolicy{MinLength: 20, RequireUppercase: true, RequireNumbers: true, RequireSpecialChars: true}
	for i := 0; i < 25; i++ {
		password, err := generatePassword(policy)
		if err != nil {
			t.Fatalf("generatePassword returned unexpected error: %v", err)
		}
		if violations := auth.ValidatePassword(password, policy); len(violations) != 0 {
			t.Fatalf("generated password %q violates its own policy: %v", password, violations)
		}
	}
}

// TestGeneratePasswordMeetsMinimumLengthFloor - This test verifies that a
// policy with a very low or zero MinLength still generates a password at
// least passwordGenMinLength characters long, longer than whatever bare
// minimum was configured.
func TestGeneratePasswordMeetsMinimumLengthFloor(t *testing.T) {
	password, err := generatePassword(auth.PasswordPolicy{MinLength: 0})
	if err != nil {
		t.Fatalf("generatePassword returned unexpected error: %v", err)
	}
	if len(password) < passwordGenMinLength {
		t.Errorf("generated password length %d, want at least %d", len(password), passwordGenMinLength)
	}
}

// TestGeneratePasswordNeverExceedsMaxPasswordLength - This test verifies
// that an extreme MinLength is capped at auth.MaxPasswordLength, so
// ValidatePassword's TooLong check can never reject a password this
// function just generated.
func TestGeneratePasswordNeverExceedsMaxPasswordLength(t *testing.T) {
	password, err := generatePassword(auth.PasswordPolicy{MinLength: 1000})
	if err != nil {
		t.Fatalf("generatePassword returned unexpected error: %v", err)
	}
	if len(password) > auth.MaxPasswordLength {
		t.Errorf("generated password length %d, want at most %d", len(password), auth.MaxPasswordLength)
	}
}

// ----------------------------------------------------------------------
// disconnect user
// ----------------------------------------------------------------------

// TestRunDisconnectUserNoDaemonReportsClearMessage - This test verifies
// that "disconnect user", run against the plain daemon.NewStandaloneClient
// newTestContext already provides, no real daemon configured, reports a
// clear, disconnect-specific message rather than the generic failure text
// every other daemon call gets. This exercises the defense-in-depth path
// only, since a real deployment with no daemon configured has this command
// pruned out of its own tree entirely, see main.go's featureFlags.
func TestRunDisconnectUserNoDaemonReportsClearMessage(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})

	err := runDisconnectUser(ctx, []string{"alice"})
	if err == nil {
		t.Fatal("expected runDisconnectUser to fail with no daemon configured")
	}
	if err.Error() != "[[disconnect.user.not_supported]]" {
		t.Errorf("error = %q, want the not_supported message", err.Error())
	}
}

// TestRunDisconnectUserSuccessPassesUsernameAndSessionID - This test
// verifies that "disconnect user <username> <session-id>" passes both
// arguments through to ctx.DaemonClient.DisconnectUser exactly, and that a
// bare "disconnect user <username>" passes an empty session ID through,
// "the one session belonging to username," see
// command.DaemonClient.DisconnectUser's doc comment.
func TestRunDisconnectUserSuccessPassesUsernameAndSessionID(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	fake := &fakeDaemonClient{}
	ctx.DaemonClient = fake

	if err := runDisconnectUser(ctx, []string{"alice", "s1"}); err != nil {
		t.Fatalf("runDisconnectUser returned unexpected error: %v", err)
	}
	if fake.DisconnectUserUsername != "alice" || fake.DisconnectUserSessionID != "s1" {
		t.Errorf("DisconnectUser called with (%q, %q), want (\"alice\", \"s1\")", fake.DisconnectUserUsername, fake.DisconnectUserSessionID)
	}

	if err := runDisconnectUser(ctx, []string{"bob"}); err != nil {
		t.Fatalf("runDisconnectUser returned unexpected error: %v", err)
	}
	if fake.DisconnectUserUsername != "bob" || fake.DisconnectUserSessionID != "" {
		t.Errorf("DisconnectUser called with (%q, %q), want (\"bob\", \"\")", fake.DisconnectUserUsername, fake.DisconnectUserSessionID)
	}
}

// TestRunDisconnectUserAmbiguousPropagatesTheDaemonsOwnErrorText - This
// test verifies that an ambiguous session error the daemon itself already
// formatted, candidate session IDs included, see
// daemon.Server.disconnectErrorText, reaches whoever typed the command
// unchanged rather than being replaced by a generic message.
func TestRunDisconnectUserAmbiguousPropagatesTheDaemonsOwnErrorText(t *testing.T) {
	ctx := newAdminTestContext(t, "admin", &auth.User{Username: "admin", Roles: []string{"admin"}})
	// T() on a nil Translator, newAdminTestContext's default, drops any
	// format args rather than applying them, see i18n.Translator.T's doc
	// comment, so a real Translator with a minimal catalog is needed here
	// to see the daemon's error text interpolated into the returned
	// message.
	ctx.Translator = i18n.New(map[string]i18n.Catalog{
		"en": {"disconnect.user.failed": "Failed to disconnect user: %s"},
	}, "en", "en")
	ctx.DaemonClient = &fakeDaemonClient{DisconnectUserErr: errors.New(`more than one session for "alice", specify a session ID: s1, s2`)}

	err := runDisconnectUser(ctx, []string{"alice"})
	if err == nil {
		t.Fatal("expected runDisconnectUser to fail on an ambiguous session")
	}
	if !strings.Contains(err.Error(), "s1, s2") {
		t.Errorf("error = %q, want it to name the candidate session IDs", err.Error())
	}
}
