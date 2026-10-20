// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// newTOTPTestContext - This function builds a *command.AppContext suitable
// for exercising the totp enable and totp disable handlers, and the
// finishTOTPEnable and finishTOTPDisable functions they delegate to,
// without needing a real terminal. ctx.Users holds one entry, keyed and
// named username, pointing at u, and ctx.UsersFile points at a throwaway
// path in t.TempDir() that a test can os.Stat to confirm neither function
// ever writes there, since enabling or disabling TOTP only ever updates
// ctx.Users in memory.
func newTOTPTestContext(t *testing.T, username string, u *auth.User) *command.AppContext {
	t.Helper()
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: username, Authenticated: true}
	ctx.Users = auth.Users{username: u}
	ctx.UsersFile = filepath.Join(t.TempDir(), "users.yaml")
	// rewireDaemonClient shares this exact Users map with
	// ctx.DaemonClient's Store, so finishTOTPEnable's and
	// finishTOTPDisable's ctx.DaemonClient.MutateUsers calls see, and
	// modify, the same ctx.Users a test asserts against afterward. See
	// rewireDaemonClient's doc comment in testhelpers_test.go.
	rewireDaemonClient(ctx)
	return ctx
}

// TestCurrentUserFindsMatchingRecord - This test verifies that currentUser
// returns the *auth.User keyed under ctx.Session.Username in ctx.Users.
func TestCurrentUserFindsMatchingRecord(t *testing.T) {
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: "alice"}
	ctx.Users = auth.Users{"alice": {Username: "alice", PasswordHash: "$0$x"}}

	u, ok := currentUser(ctx)
	if !ok {
		t.Fatal("expected currentUser to find alice's record")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want %q", u.Username, "alice")
	}
}

// TestCurrentUserFalseWhenUsersNil - This test verifies that currentUser
// returns ok false, rather than panicking, when ctx.Users is nil, the
// state it is in for any session run with AuthRequired off.
func TestCurrentUserFalseWhenUsersNil(t *testing.T) {
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: "alice"}
	if _, ok := currentUser(ctx); ok {
		t.Error("expected currentUser to return false when ctx.Users is nil")
	}
}

// TestCurrentUserFalseWhenUsernameNotInUsers - This test verifies that
// currentUser returns ok false for a session whose own username has no
// matching entry in ctx.Users, an edge case that should not happen in
// practice for a session that already passed requireLoggedIn, but is
// checked rather than assumed.
func TestCurrentUserFalseWhenUsernameNotInUsers(t *testing.T) {
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: "ghost"}
	ctx.Users = auth.Users{"alice": {Username: "alice", PasswordHash: "$0$x"}}
	if _, ok := currentUser(ctx); ok {
		t.Error("expected currentUser to return false for a username with no matching entry")
	}
}

// ----------------------------------------------------------------------
//
// finishTOTPEnable
//
// ----------------------------------------------------------------------

// TestFinishTOTPEnableUpdatesInMemoryOnly - This test verifies that a
// valid confirmation code sets user.TOTPSecret, in memory, and reports
// true, telling the caller enrollment completed. It never touches
// ctx.UsersFile on disk at all, only "write memory" persists an account
// change like this one, so a reload or restart before that runs leaves
// TOTP disabled again.
func TestFinishTOTPEnableUpdatesInMemoryOnly(t *testing.T) {
	user := &auth.User{Username: "alice", PasswordHash: "$0$alicepass"}
	ctx := newTOTPTestContext(t, "alice", user)

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	now := time.Now()
	code, err := auth.GenerateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode returned error: %v", err)
	}

	enabled, err := finishTOTPEnable(ctx, user, secret, code, now)
	if err != nil {
		t.Fatalf("finishTOTPEnable returned unexpected error: %v", err)
	}
	if !enabled {
		t.Error("expected finishTOTPEnable to report true for a valid confirmation code")
	}
	if user.TOTPSecret != secret {
		t.Errorf("TOTPSecret = %q, want %q", user.TOTPSecret, secret)
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written until an explicit save runs")
	}
}

// TestFinishTOTPEnableRejectsInvalidCodeAndDoesNotSave - This test
// verifies that a wrong confirmation code leaves user.TOTPSecret empty,
// reports the failure without returning an error, the same way a wrong
// password at a login prompt is reported rather than failing the whole
// command, reports false rather than true, and never writes a users file
// at all.
func TestFinishTOTPEnableRejectsInvalidCodeAndDoesNotSave(t *testing.T) {
	user := &auth.User{Username: "alice", PasswordHash: "$0$alicepass"}
	ctx := newTOTPTestContext(t, "alice", user)

	secret, _ := auth.GenerateTOTPSecret()
	now := time.Now()
	// Ten minutes outside the +/-1 step skew window guarantees this does
	// not verify against "now", the same technique
	// TestVerifyTOTPCodeRejectsCodeOutsideSkewWindow in totp_test.go uses,
	// rather than a fixed wrong code that could, astronomically unlikely
	// but not impossible, be correct.
	wrongCode, _ := auth.GenerateTOTPCode(secret, now.Add(-10*time.Minute))

	enabled, err := finishTOTPEnable(ctx, user, secret, wrongCode, now)
	if err != nil {
		t.Fatalf("finishTOTPEnable returned unexpected error for a wrong code: %v", err)
	}
	if enabled {
		t.Error("expected finishTOTPEnable to report false for a wrong confirmation code")
	}
	if user.TOTPSecret != "" {
		t.Errorf("expected TOTPSecret to stay empty after a wrong code, got %q", user.TOTPSecret)
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written when the confirmation code was wrong")
	}
}

// ----------------------------------------------------------------------
//
// clearScreen
//
// ----------------------------------------------------------------------

// TestClearScreenWritesANSIResetSequence - This test verifies that
// clearScreen writes exactly the ANSI clear-and-home escape sequence to w,
// the same sequence runTOTPEnable relies on to wipe a freshly printed
// secret and its QR code, and the terminal's scrollback, once enrollment
// has either succeeded or run out of attempts.
func TestClearScreenWritesANSIResetSequence(t *testing.T) {
	var buf bytes.Buffer
	clearScreen(&buf)
	if buf.String() != ansiClearScreen {
		t.Errorf("clearScreen wrote %q, want %q", buf.String(), ansiClearScreen)
	}
}

// ----------------------------------------------------------------------
//
// finishTOTPDisable
//
// ----------------------------------------------------------------------

// TestFinishTOTPDisableClearsSecretInMemoryOnly - This test verifies that
// a correct account password together with a valid current TOTP code
// clears user.TOTPSecret, in memory, and reports true, telling the caller
// the account is now disabled. It never touches ctx.UsersFile on disk, the
// disable-side mirror of TestFinishTOTPEnableUpdatesInMemoryOnly.
func TestFinishTOTPDisableClearsSecretInMemoryOnly(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	ctx := newTOTPTestContext(t, "alice", user)

	now := time.Now()
	code, _ := auth.GenerateTOTPCode(secret, now)

	disabled, err := finishTOTPDisable(ctx, user, "s3cret", code, now)
	if err != nil {
		t.Fatalf("finishTOTPDisable returned unexpected error: %v", err)
	}
	if !disabled {
		t.Error("expected finishTOTPDisable to report true for a correct password and code")
	}
	if user.TOTPSecret != "" {
		t.Errorf("expected TOTPSecret to be cleared, got %q", user.TOTPSecret)
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written until an explicit save runs")
	}
}

// TestFinishTOTPDisableDeniedOnWrongPassword - This test verifies that a
// wrong account password refuses the disable, leaving user.TOTPSecret
// untouched and reporting false, even when the TOTP code supplied
// alongside it is correct.
func TestFinishTOTPDisableDeniedOnWrongPassword(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	ctx := newTOTPTestContext(t, "alice", user)

	now := time.Now()
	code, _ := auth.GenerateTOTPCode(secret, now)

	disabled, err := finishTOTPDisable(ctx, user, "wrong-password", code, now)
	if err != nil {
		t.Fatalf("finishTOTPDisable returned unexpected error for a wrong password: %v", err)
	}
	if disabled {
		t.Error("expected finishTOTPDisable to report false for a wrong password")
	}
	if user.TOTPSecret != secret {
		t.Errorf("expected TOTPSecret to remain unchanged after a wrong password, got %q", user.TOTPSecret)
	}
}

// TestFinishTOTPDisableDeniedOnWrongCode - This test verifies that a wrong
// TOTP code refuses the disable, leaving user.TOTPSecret untouched and
// reporting false, even when the account password supplied alongside it is
// correct.
func TestFinishTOTPDisableDeniedOnWrongCode(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	ctx := newTOTPTestContext(t, "alice", user)

	now := time.Now()
	wrongCode, _ := auth.GenerateTOTPCode(secret, now.Add(-10*time.Minute))

	disabled, err := finishTOTPDisable(ctx, user, "s3cret", wrongCode, now)
	if err != nil {
		t.Fatalf("finishTOTPDisable returned unexpected error for a wrong code: %v", err)
	}
	if disabled {
		t.Error("expected finishTOTPDisable to report false for a wrong code")
	}
	if user.TOTPSecret != secret {
		t.Errorf("expected TOTPSecret to remain unchanged after a wrong code, got %q", user.TOTPSecret)
	}
}

// ----------------------------------------------------------------------
//
// totpMaxAttempts
//
// ----------------------------------------------------------------------

// TestTOTPMaxAttemptsReturnsConfiguredValue - This test verifies that
// totpMaxAttempts returns ctx.TOTPMaxAttempts unchanged once it is set to
// a positive number, the ordinary case for a ctx wired up through main.go
// from a validated config.SystemConfig.
func TestTOTPMaxAttemptsReturnsConfiguredValue(t *testing.T) {
	ctx := newTestContext()
	ctx.TOTPMaxAttempts = 5
	if got := totpMaxAttempts(ctx); got != 5 {
		t.Errorf("totpMaxAttempts() = %d, want 5", got)
	}
}

// TestTOTPMaxAttemptsDefaultsToOneWhenUnset - This test verifies that
// totpMaxAttempts falls back to 1, rather than 0, for a hand built
// *command.AppContext that never set TOTPMaxAttempts at all, the state
// most throwaway test contexts in this package are in. A real ctx wired up
// through main.go never reaches this fallback, since
// config.SystemConfig.validate rejects a TOTPMaxAttempts below 1 at
// startup.
func TestTOTPMaxAttemptsDefaultsToOneWhenUnset(t *testing.T) {
	ctx := newTestContext()
	if got := totpMaxAttempts(ctx); got != 1 {
		t.Errorf("totpMaxAttempts() = %d, want 1 for an unset TOTPMaxAttempts", got)
	}
}

// TestTOTPMaxAttemptsDefaultsToOneWhenNegative - This test verifies the
// same fallback as TestTOTPMaxAttemptsDefaultsToOneWhenUnset for a
// negative TOTPMaxAttempts, a value config.SystemConfig.validate would
// also reject, so this only matters for a hand built ctx.
func TestTOTPMaxAttemptsDefaultsToOneWhenNegative(t *testing.T) {
	ctx := newTestContext()
	ctx.TOTPMaxAttempts = -1
	if got := totpMaxAttempts(ctx); got != 1 {
		t.Errorf("totpMaxAttempts() = %d, want 1 for a negative TOTPMaxAttempts", got)
	}
}

// ----------------------------------------------------------------------
//
// The registered "totp.enable", "totp.enable.qr", and "totp.disable"
// handlers
//
// ----------------------------------------------------------------------

// TestTOTPEnableHandlerErrorsWhenNotLoggedIn - This test verifies that the
// registered "totp.enable" handler refuses to run at all for a session
// with no Session set, before it would ever try to read a confirmation
// code from a real terminal.
func TestTOTPEnableHandlerErrorsWhenNotLoggedIn(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "totp.enable")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running totp enable without being logged in, got nil")
	}
}

// TestTOTPEnableHandlerErrorsWhenNoCurrentUser - This test verifies that
// the registered "totp.enable" handler refuses to run for an authenticated
// session whose own username has no matching entry in ctx.Users.
func TestTOTPEnableHandlerErrorsWhenNoCurrentUser(t *testing.T) {
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: "ghost", Authenticated: true}
	ctx.Users = auth.Users{"alice": {Username: "alice", PasswordHash: "$0$x"}}
	cmd := loadTestCommand(t, "totp.enable")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running totp enable for a session with no matching user record, got nil")
	}
}

// TestTOTPEnableHandlerReportsAlreadyEnabledWithoutPrompting - This test
// verifies that the registered "totp.enable" handler returns before ever
// trying to read a confirmation code from a real terminal when the current
// user already has a TOTPSecret set. If it did not return early here, this
// test would hang or fail trying to read from stdin, since nothing in this
// test provides one.
func TestTOTPEnableHandlerReportsAlreadyEnabledWithoutPrompting(t *testing.T) {
	user := &auth.User{Username: "alice", PasswordHash: "$0$x", TOTPSecret: "JBSWY3DPEHPK3PXP"}
	ctx := newTOTPTestContext(t, "alice", user)
	cmd := loadTestCommand(t, "totp.enable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("totp enable handler returned unexpected error: %v", err)
	}
	if user.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("expected the existing TOTPSecret to be left untouched, got %q", user.TOTPSecret)
	}
}

// TestTOTPEnableQRHandlerErrorsWhenNotLoggedIn - This test verifies that
// the registered "totp.enable.qr" handler, the "totp enable qr" form,
// refuses to run at all for a session with no Session set, the same early
// check "totp.enable" itself has, since both share runTOTPEnable.
func TestTOTPEnableQRHandlerErrorsWhenNotLoggedIn(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "totp.enable.qr")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running totp enable qr without being logged in, got nil")
	}
}

// TestTOTPEnableQRHandlerReportsAlreadyEnabledWithoutPrompting - This test
// verifies that "totp.enable.qr" also returns before ever trying to read a
// confirmation code from a real terminal when the current user already has
// a TOTPSecret set, the same early return
// TestTOTPEnableHandlerReportsAlreadyEnabledWithoutPrompting checks for
// the plain form.
func TestTOTPEnableQRHandlerReportsAlreadyEnabledWithoutPrompting(t *testing.T) {
	user := &auth.User{Username: "alice", PasswordHash: "$0$x", TOTPSecret: "JBSWY3DPEHPK3PXP"}
	ctx := newTOTPTestContext(t, "alice", user)
	cmd := loadTestCommand(t, "totp.enable.qr")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("totp enable qr handler returned unexpected error: %v", err)
	}
	if user.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("expected the existing TOTPSecret to be left untouched, got %q", user.TOTPSecret)
	}
}

// TestTOTPDisableHandlerErrorsWhenNotLoggedIn - This test verifies that
// the registered "totp.disable" handler refuses to run at all for a
// session with no Session set, before it would ever try to read a password
// or a code from a real terminal.
func TestTOTPDisableHandlerErrorsWhenNotLoggedIn(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "totp.disable")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running totp disable without being logged in, got nil")
	}
}

// TestTOTPDisableHandlerReportsNotEnabledWithoutPrompting - This test
// verifies that the registered "totp.disable" handler returns before ever
// trying to read a password or a code from a real terminal when the
// current user has no TOTPSecret set. If it did not return early here,
// this test would hang or fail trying to read from stdin.
func TestTOTPDisableHandlerReportsNotEnabledWithoutPrompting(t *testing.T) {
	user := &auth.User{Username: "alice", PasswordHash: "$0$x"}
	ctx := newTOTPTestContext(t, "alice", user)
	cmd := loadTestCommand(t, "totp.disable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("totp disable handler returned unexpected error: %v", err)
	}
}
