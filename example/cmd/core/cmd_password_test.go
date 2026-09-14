// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// newPasswordTestContext - This function builds a *command.AppContext
// suitable for exercising verifyReauth, finishPasswordChange, and the
// registered "password.change" handler, without needing a real terminal,
// the same shape newTOTPTestContext in cmd_totp_test.go builds for its own
// package's tests. ctx.Users holds one entry, keyed and named username,
// pointing at u, and ctx.UsersFile points at a throwaway path in
// t.TempDir() that a test can os.Stat to confirm finishPasswordChange
// never writes there, since a password change only ever updates ctx.Users
// in memory, see that function's doc comment. ctx.PasswordPolicy defaults
// to a permissive MinLength of 1 with no composition rules, so a test can
// focus on one rule at a time by overriding just the field it cares about.
func newPasswordTestContext(t *testing.T, username string, u *auth.User) *command.AppContext {
	t.Helper()
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: username, Authenticated: true}
	ctx.Users = auth.Users{username: u}
	ctx.UsersFile = filepath.Join(t.TempDir(), "users.yaml")
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 1}
	ctx.PasswordChangeMaxAttempts = 3
	ctx.AuthProvider = auth.NewLocalProvider(ctx.Users)
	// rewireDaemonClient shares this exact Users map with
	// ctx.DaemonClient's Store, so finishPasswordChange's
	// ctx.DaemonClient.MutateUsers call sees, and modifies, the same
	// ctx.Users a test asserts against afterward. See rewireDaemonClient's
	// doc comment in testhelpers_test.go.
	rewireDaemonClient(ctx)
	return ctx
}

// testReauthProvider - This helper builds the auth.Provider verifyReauth
// takes, backed by a Users map containing exactly user, for the
// tests below that call verifyReauth directly rather than through a
// *command.AppContext.
func testReauthProvider(user *auth.User) auth.Provider {
	return auth.NewLocalProvider(auth.Users{user.Username: user})
}

// ----------------------------------------------------------------------
//
// verifyReauth
//
// ----------------------------------------------------------------------

// TestVerifyReauthAcceptsCorrectPasswordWithNoSecondFactor - This test
// verifies that a correct password alone is enough for a user with no
// second factor configured, no code required at all.
func TestVerifyReauthAcceptsCorrectPasswordWithNoSecondFactor(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	user := &auth.User{Username: "alice", PasswordHash: hash}

	if !verifyReauth(testReauthProvider(user), user, "s3cret", "", time.Now()) {
		t.Error("expected verifyReauth to accept a correct password with no second factor configured")
	}
}

// TestVerifyReauthRejectsWrongPasswordWithNoSecondFactor - This test
// verifies that a wrong password is refused even when no second factor is
// configured at all.
func TestVerifyReauthRejectsWrongPasswordWithNoSecondFactor(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	user := &auth.User{Username: "alice", PasswordHash: hash}

	if verifyReauth(testReauthProvider(user), user, "wrong-password", "", time.Now()) {
		t.Error("expected verifyReauth to reject a wrong password")
	}
}

// TestVerifyReauthAcceptsCorrectPasswordAndCodeWithSecondFactor - This
// test verifies that a correct password together with a valid TOTP code
// succeeds for a user with a second factor configured.
func TestVerifyReauthAcceptsCorrectPasswordAndCodeWithSecondFactor(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	now := time.Now()
	code, _ := auth.GenerateTOTPCode(secret, now)

	if !verifyReauth(testReauthProvider(user), user, "s3cret", code, now) {
		t.Error("expected verifyReauth to accept a correct password and a valid TOTP code")
	}
}

// TestVerifyReauthRejectsCorrectPasswordWithWrongCode - This test verifies
// that a correct password with a wrong second factor code is refused, the
// same "right password, wrong code still fails" behavior auth.PromptLogin
// already enforces for login.
func TestVerifyReauthRejectsCorrectPasswordWithWrongCode(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	now := time.Now()
	wrongCode, _ := auth.GenerateTOTPCode(secret, now.Add(-10*time.Minute))

	if verifyReauth(testReauthProvider(user), user, "s3cret", wrongCode, now) {
		t.Error("expected verifyReauth to reject a correct password with a wrong TOTP code")
	}
}

// TestVerifyReauthRejectsCorrectPasswordWithEmptyCode - This test verifies
// that a correct password with no code at all is refused for a user whose
// second factor is required, the state runPasswordChange is never meant to
// reach since it always prompts for a code first, but verifyReauth itself
// does not assume that.
func TestVerifyReauthRejectsCorrectPasswordWithEmptyCode(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}

	if verifyReauth(testReauthProvider(user), user, "s3cret", "", time.Now()) {
		t.Error("expected verifyReauth to reject a correct password with an empty code when a second factor is configured")
	}
}

// TestVerifyReauthRejectsWrongPasswordEvenWithValidCode - This test
// verifies that a wrong password is refused even alongside an otherwise
// valid TOTP code, the same "cannot substitute a valid second factor for a
// wrong first factor" property VerifyLogin itself depends on.
func TestVerifyReauthRejectsWrongPasswordEvenWithValidCode(t *testing.T) {
	hash, _ := auth.HashPassword("s3cret")
	secret, _ := auth.GenerateTOTPSecret()
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	now := time.Now()
	code, _ := auth.GenerateTOTPCode(secret, now)

	if verifyReauth(testReauthProvider(user), user, "wrong-password", code, now) {
		t.Error("expected verifyReauth to reject a wrong password even with a valid TOTP code")
	}
}

// ----------------------------------------------------------------------
//
// finishPasswordChange
//
// ----------------------------------------------------------------------

// TestFinishPasswordChangeUpdatesInMemoryOnly - This test verifies that a
// new password matching its own confirmation and satisfying
// ctx.PasswordPolicy replaces user.PasswordHash, in memory, so the new
// password verifies and the old one no longer does, and reports true. It
// never touches ctx.UsersFile on disk at all, only "write memory" persists
// an account change like this one, so a reload or restart before that runs
// reverts right back to the old password.
func TestFinishPasswordChangeUpdatesInMemoryOnly(t *testing.T) {
	hash, _ := auth.HashPassword("old-password")
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	changed, err := finishPasswordChange(ctx, user, "new-password", "new-password")
	if err != nil {
		t.Fatalf("finishPasswordChange returned unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected finishPasswordChange to report true for a valid, matching new password")
	}
	if !auth.VerifyPassword(user.PasswordHash, "new-password") {
		t.Error("expected user.PasswordHash to verify against the new password")
	}
	if auth.VerifyPassword(user.PasswordHash, "old-password") {
		t.Error("expected user.PasswordHash to no longer verify against the old password")
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written until an explicit save runs")
	}
}

// TestFinishPasswordChangeRejectsMismatchAndDoesNotSave - This test
// verifies that a new password not matching its own confirmation leaves
// user.PasswordHash untouched, reports false rather than an error, and
// never writes a users file at all.
func TestFinishPasswordChangeRejectsMismatchAndDoesNotSave(t *testing.T) {
	hash, _ := auth.HashPassword("old-password")
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	changed, err := finishPasswordChange(ctx, user, "new-password", "typo-password")
	if err != nil {
		t.Fatalf("finishPasswordChange returned unexpected error for a mismatch: %v", err)
	}
	if changed {
		t.Error("expected finishPasswordChange to report false for a mismatched confirmation")
	}
	if !auth.VerifyPassword(user.PasswordHash, "old-password") {
		t.Error("expected PasswordHash to be left unchanged after a mismatch")
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written when the new password did not match its confirmation")
	}
}

// TestFinishPasswordChangeRejectsPolicyViolationAndDoesNotSave - This test
// verifies that a new password failing ctx.PasswordPolicy, here too short
// against a MinLength of 10, is refused, leaving user.PasswordHash
// untouched and writing no users file, even though the two typed copies
// matched each other.
func TestFinishPasswordChangeRejectsPolicyViolationAndDoesNotSave(t *testing.T) {
	hash, _ := auth.HashPassword("old-password")
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 10}

	changed, err := finishPasswordChange(ctx, user, "short", "short")
	if err != nil {
		t.Fatalf("finishPasswordChange returned unexpected error for a policy violation: %v", err)
	}
	if changed {
		t.Error("expected finishPasswordChange to report false for a password too short for the policy")
	}
	if !auth.VerifyPassword(user.PasswordHash, "old-password") {
		t.Error("expected PasswordHash to be left unchanged after a policy violation")
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written when the new password violated the policy")
	}
}

// TestFinishPasswordChangeRejectsSameAsCurrentAndDoesNotSave - This test
// verifies that a new password identical to the account's current password
// is refused, even though it matches its own confirmation and satisfies
// the policy, since a password that does not change provides no security
// benefit.
func TestFinishPasswordChangeRejectsSameAsCurrentAndDoesNotSave(t *testing.T) {
	hash, _ := auth.HashPassword("same-password")
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	changed, err := finishPasswordChange(ctx, user, "same-password", "same-password")
	if err != nil {
		t.Fatalf("finishPasswordChange returned unexpected error for a same-as-current password: %v", err)
	}
	if changed {
		t.Error("expected finishPasswordChange to report false when the new password matches the current one")
	}
	if _, err := os.Stat(ctx.UsersFile); err == nil {
		t.Error("expected no users file to be written when the new password matched the current one")
	}
}

// TestFinishPasswordChangeMismatchCheckedBeforePolicy - This test verifies
// that a mismatch is reported even when the candidate would also have
// failed the policy, confirming finishPasswordChange checks the two typed
// copies against each other before ever validating either one against the
// policy, so a session sees one problem at a time.
func TestFinishPasswordChangeMismatchCheckedBeforePolicy(t *testing.T) {
	hash, _ := auth.HashPassword("old-password")
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 10}

	// Both copies are short enough to also fail the policy, and they do
	// not match each other either.
	changed, err := finishPasswordChange(ctx, user, "short1", "short2")
	if err != nil {
		t.Fatalf("finishPasswordChange returned unexpected error: %v", err)
	}
	if changed {
		t.Error("expected finishPasswordChange to report false for a mismatched, policy-violating pair")
	}
}

// ----------------------------------------------------------------------
//
// passwordChangeMaxAttempts
//
// ----------------------------------------------------------------------

// TestPasswordChangeMaxAttemptsReturnsConfiguredValue - This test verifies
// that passwordChangeMaxAttempts returns ctx.PasswordChangeMaxAttempts
// unchanged once it is set to a positive number, the ordinary case for a
// ctx wired up through main.go from a validated config.SystemConfig.
func TestPasswordChangeMaxAttemptsReturnsConfiguredValue(t *testing.T) {
	ctx := newTestContext()
	ctx.PasswordChangeMaxAttempts = 5
	if got := passwordChangeMaxAttempts(ctx); got != 5 {
		t.Errorf("passwordChangeMaxAttempts() = %d, want 5", got)
	}
}

// TestPasswordChangeMaxAttemptsDefaultsToOneWhenUnset - This test verifies
// that passwordChangeMaxAttempts falls back to 1, rather than 0, for a
// hand built *command.AppContext that never set PasswordChangeMaxAttempts
// at all. A real ctx wired up through main.go never reaches this fallback,
// since config.SystemConfig.validate rejects a PasswordChangeMaxAttempts
// below 1 at startup.
func TestPasswordChangeMaxAttemptsDefaultsToOneWhenUnset(t *testing.T) {
	ctx := newTestContext()
	if got := passwordChangeMaxAttempts(ctx); got != 1 {
		t.Errorf("passwordChangeMaxAttempts() = %d, want 1 for an unset PasswordChangeMaxAttempts", got)
	}
}

// TestPasswordChangeMaxAttemptsDefaultsToOneWhenNegative - This test
// verifies the same fallback as
// TestPasswordChangeMaxAttemptsDefaultsToOneWhenUnset for a negative
// PasswordChangeMaxAttempts, a value config.SystemConfig.validate would
// also reject, so this only matters for a hand built ctx.
func TestPasswordChangeMaxAttemptsDefaultsToOneWhenNegative(t *testing.T) {
	ctx := newTestContext()
	ctx.PasswordChangeMaxAttempts = -1
	if got := passwordChangeMaxAttempts(ctx); got != 1 {
		t.Errorf("passwordChangeMaxAttempts() = %d, want 1 for a negative PasswordChangeMaxAttempts", got)
	}
}

// ----------------------------------------------------------------------
//
// The registered "password.change" handler
//
// ----------------------------------------------------------------------

// TestPasswordChangeHandlerErrorsWhenNotLoggedIn - This test verifies that
// the registered "password.change" handler refuses to run at all for a
// session with no Session set, before it would ever try to read a password
// from a real terminal.
func TestPasswordChangeHandlerErrorsWhenNotLoggedIn(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "password.change")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running password change without being logged in, got nil")
	}
}

// TestPasswordChangeHandlerErrorsWhenNoCurrentUser - This test verifies
// that the registered "password.change" handler refuses to run for an
// authenticated session whose own username has no matching entry in
// ctx.Users, before it would ever try to read a password from a real
// terminal.
func TestPasswordChangeHandlerErrorsWhenNoCurrentUser(t *testing.T) {
	ctx := newTestContext()
	ctx.Session = &auth.Session{Username: "ghost", Authenticated: true}
	ctx.Users = auth.Users{"alice": {Username: "alice", PasswordHash: "$0$x"}}
	cmd := loadTestCommand(t, "password.change")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error running password change for a session with no matching user record, got nil")
	}
}
