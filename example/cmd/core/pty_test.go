// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/internal/clitest"
)

// ----------------------------------------------------------------------
//
// pty test helpers
//
// ----------------------------------------------------------------------

// newPTY - This function is a thin alias for clitest.NewPTY, kept so this
// package's tests read the same way they always have. The real
// implementation is shared with every other package that needs a real
// terminal; see internal/clitest.
func newPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	return clitest.NewPTY(t)
}

// sendLine - This function writes s, followed by a newline, to w, the
// master side of a pty from newPTY, the same as a person typing s and
// pressing Enter.
func sendLine(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := fmt.Fprintln(w, s); err != nil {
		t.Fatalf("failed to write %q to pty master: %v", s, err)
	}
}

// runHandler - This function calls fn, one of runPasswordChangeWithIO,
// runTOTPEnable, or runTOTPDisable, on its own goroutine and returns a
// channel carrying its error result. Every one of them blocks reading from
// a pty's slave side until a full line arrives, so the function under test
// must run on its own goroutine while the test itself writes to the master
// side; calling fn directly on the test's goroutine before writing
// anything would deadlock, each side waiting on the other.
func runHandler(fn func() error) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- fn() }()
	return ch
}

// awaitHandler - This function waits for a result on ch, failing the test
// rather than hanging forever if fn never returns within timeout, the sign
// of a real deadlock somewhere above rather than a slow but working test.
func awaitHandler(t *testing.T, ch <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		t.Fatal("handler did not return in time, likely deadlocked waiting on pty input")
		return nil
	}
}

// wrongTOTPCode - This function returns a code that does not verify
// against secret, generated well outside VerifyTOTPCode's clock-skew
// tolerance, the same technique auth/mfa_test.go's
// TestVerifySecondFactorRejectsWrongCodeThroughReaderFallback already
// uses, rather than a hardcoded literal that could coincidentally be
// correct.
func wrongTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := auth.GenerateTOTPCode(secret, time.Now().Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("GenerateTOTPCode returned error: %v", err)
	}
	return code
}

// ----------------------------------------------------------------------
//
// runPasswordChangeWithIO
//
// ----------------------------------------------------------------------

// TestRunPasswordChangeWithIOSucceedsAfterWrongCurrentPasswordRetry - This
// test verifies the re-authentication phase's retry, driven through a real
// pty rather than called directly: a wrong current password on the first
// attempt is rejected and retried, a correct one on the second attempt
// succeeds through to the new password prompts within that same attempt,
// and the account's stored hash verifies against the new password
// afterward.
func TestRunPasswordChangeWithIOSucceedsAfterWrongCurrentPasswordRetry(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runPasswordChangeWithIO(ctx, int(slave.Fd()), slave)
	})

	sendLine(t, master, "wrong-password")
	sendLine(t, master, "s3cret")
	sendLine(t, master, "n3wpassword")
	sendLine(t, master, "n3wpassword")

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runPasswordChangeWithIO returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(user.PasswordHash, "n3wpassword") {
		t.Error("expected the account's password hash to verify against the new password")
	}
	if auth.VerifyPassword(user.PasswordHash, "s3cret") {
		t.Error("expected the account's old password to no longer verify")
	}
}

// TestRunPasswordChangeWithIORetriesOnMismatchedConfirmationWithoutReauthenticating
// - This test verifies the documented behavior that once re-authentication
// has already succeeded, a later mismatch between the new password and its
// confirmation only re-prompts for the new password and its confirmation,
// never the current password again.
func TestRunPasswordChangeWithIORetriesOnMismatchedConfirmationWithoutReauthenticating(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runPasswordChangeWithIO(ctx, int(slave.Fd()), slave)
	})

	sendLine(t, master, "s3cret")
	sendLine(t, master, "n3wpassword")
	sendLine(t, master, "mismatched")
	sendLine(t, master, "n3wpassword")
	sendLine(t, master, "n3wpassword")

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runPasswordChangeWithIO returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(user.PasswordHash, "n3wpassword") {
		t.Error("expected the account's password hash to verify against the new password")
	}
}

// TestRunPasswordChangeWithIOExhaustsAttemptsOnRepeatedWrongPassword -This
// test verifies the whole retry loop's ceiling: a wrong current password
// on every attempt leaves the account's password unchanged and returns no
// error, the same "report and move on" outcome an exhausted login attempt
// budget produces elsewhere in this project.
func TestRunPasswordChangeWithIOExhaustsAttemptsOnRepeatedWrongPassword(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	user := &auth.User{Username: "alice", PasswordHash: hash}
	ctx := newPasswordTestContext(t, "alice", user)

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runPasswordChangeWithIO(ctx, int(slave.Fd()), slave)
	})

	for i := 0; i < ctx.PasswordChangeMaxAttempts; i++ {
		sendLine(t, master, "wrong-password")
	}

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runPasswordChangeWithIO returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(user.PasswordHash, "s3cret") {
		t.Error("expected the account's password to remain unchanged after exhausting every attempt")
	}
}

// ----------------------------------------------------------------------
//
// runTOTPDisable
//
// ----------------------------------------------------------------------

// TestRunTOTPDisableWithIOSucceedsAfterWrongAttemptRetry - This test
// verifies the retry loop re-reads a fresh password and code from the pty
// on its second attempt rather than reusing stale state: a correct
// password paired with a wrong code is rejected, a correct password and
// code on the following attempt clears TOTPSecret.
func TestRunTOTPDisableWithIOSucceedsAfterWrongAttemptRetry(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	ctx := newTOTPTestContext(t, "alice", user)
	ctx.TOTPMaxAttempts = 3

	validCode, err := auth.GenerateTOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode returned error: %v", err)
	}

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runTOTPDisable(ctx, int(slave.Fd()), slave)
	})

	sendLine(t, master, "s3cret")
	sendLine(t, master, wrongTOTPCode(t, secret))
	sendLine(t, master, "s3cret")
	sendLine(t, master, validCode)

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runTOTPDisable returned unexpected error: %v", err)
	}
	if user.TOTPSecret != "" {
		t.Error("expected TOTPSecret to be cleared after a successful disable")
	}
}

// TestRunTOTPDisableWithIOExhaustsAttemptsOnRepeatedWrongCode - This test
// verifies that a wrong code on every attempt leaves TOTPSecret set,
// rather than disabling the second factor on anything less than a fully
// verified attempt.
func TestRunTOTPDisableWithIOExhaustsAttemptsOnRepeatedWrongCode(t *testing.T) {
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	user := &auth.User{Username: "alice", PasswordHash: hash, TOTPSecret: secret}
	ctx := newTOTPTestContext(t, "alice", user)
	ctx.TOTPMaxAttempts = 2

	master, slave := newPTY(t)
	resCh := runHandler(func() error {
		return runTOTPDisable(ctx, int(slave.Fd()), slave)
	})

	for i := 0; i < ctx.TOTPMaxAttempts; i++ {
		sendLine(t, master, "s3cret")
		sendLine(t, master, wrongTOTPCode(t, secret))
	}

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runTOTPDisable returned unexpected error: %v", err)
	}
	if user.TOTPSecret == "" {
		t.Error("expected TOTPSecret to remain set after exhausting every attempt")
	}
}

// ----------------------------------------------------------------------
//
// runTOTPEnable
//
// ----------------------------------------------------------------------

// TestRunTOTPEnableWithIOExhaustsAttemptsOnRepeatedWrongCode - This test
// verifies runTOTPEnable's retry loop through a real pty: a wrong
// confirmation code on every attempt leaves TOTPSecret unset and clears
// the screen, through the injected stdout, once attempts are used up.
// runTOTPEnable generates its own secret internally and only ever prints
// it through the package level fmt.Println, not through the injected
// stdout, so a successful enrollment cannot be driven from this test
// without also capturing the real process's stdout to read that secret
// back, the same "read the secret back out of the captured output"
// technique the pty based interactive smoke test already covers by hand.
// finishTOTPEnable's success path, given a known secret and a matching
// code, already has thorough direct coverage elsewhere, so this test's
// scope stays the exhaustion path, which needs no secret to be legible
// from outside at all.
func TestRunTOTPEnableWithIOExhaustsAttemptsOnRepeatedWrongCode(t *testing.T) {
	user := &auth.User{Username: "alice"}
	ctx := newTOTPTestContext(t, "alice", user)
	ctx.TOTPMaxAttempts = 2

	master, slave := newPTY(t)
	var stdout bytes.Buffer
	resCh := runHandler(func() error {
		return runTOTPEnable(ctx, false, int(slave.Fd()), &stdout)
	})

	for i := 0; i < ctx.TOTPMaxAttempts; i++ {
		sendLine(t, master, "000000")
	}

	if err := awaitHandler(t, resCh, 5*time.Second); err != nil {
		t.Fatalf("runTOTPEnable returned unexpected error: %v", err)
	}
	if user.TOTPSecret != "" {
		t.Error("expected TOTPSecret to remain unset after exhausting every attempt")
	}
	if !strings.Contains(stdout.String(), ansiClearScreen) {
		t.Errorf("expected the screen to be cleared once every attempt was used up, got %q", stdout.String())
	}
}
