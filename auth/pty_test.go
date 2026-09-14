// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

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

// promptResult - This type bundles a masked prompt function's two return
// values so a test can send them over a channel from the goroutine that
// calls it, see runPrompt below.
type promptResult struct {
	value string
	err   error
}

// runPrompt - This function calls fn, one of PromptSecret,
// PromptNewPassword, PromptPasswordConfirmation, or PromptTOTPCode, on its
// own goroutine and returns a channel carrying its result. A pty backed
// masked read must run this way, not inline, since fn blocks reading from
// the pty's slave side until a full line arrives, and the test itself is
// what writes that line to the master side right after starting this
// goroutine. Calling fn directly on the test's goroutine before writing
// anything to master would deadlock, each side waiting on the other.
func runPrompt(fn func() (string, error)) <-chan promptResult {
	ch := make(chan promptResult, 1)
	go func() {
		value, err := fn()
		ch <- promptResult{value, err}
	}()
	return ch
}

// awaitPrompt - This function waits for res, failing the test rather than
// hanging forever if fn never returns within timeout, the sign of a real
// deadlock somewhere above rather than a slow but working test.
func awaitPrompt(t *testing.T, res <-chan promptResult, timeout time.Duration) promptResult {
	t.Helper()
	select {
	case r := <-res:
		return r
	case <-time.After(timeout):
		t.Fatal("prompt function did not return in time, likely deadlocked waiting on pty input")
		return promptResult{}
	}
}

// ----------------------------------------------------------------------
//
// PromptSecret
//
// ----------------------------------------------------------------------

// TestPromptSecretReadsMaskedPassword - This test verifies the genuine
// success path through a real pty: a password typed at the master side is
// read back through PromptSecret with no error, and the prompt text it
// writes reaches w, the same slave file, since real callers such as
// main.go's password manager command pass os.Stdout and int(os.Stdin.Fd())
// as the same underlying terminal.
func TestPromptSecretReadsMaskedPassword(t *testing.T) {
	master, slave := newPTY(t)

	resCh := runPrompt(func() (string, error) {
		return PromptSecret(slave, int(slave.Fd()), nil)
	})
	sendLine(t, master, "s3cret")
	res := awaitPrompt(t, resCh, 5*time.Second)

	if res.err != nil {
		t.Fatalf("PromptSecret returned unexpected error: %v", res.err)
	}
	if res.value != "s3cret" {
		t.Errorf("PromptSecret returned %q, want %q", res.value, "s3cret")
	}
}

// TestPromptSecretReturnsErrorWhenReadFails - This test verifies the plain
// error path: an invalid file descriptor makes term.ReadPassword fail
// immediately, and PromptSecret must surface that as an error rather than
// blocking or panicking. No pty is needed for this path.
func TestPromptSecretReturnsErrorWhenReadFails(t *testing.T) {
	var out bytes.Buffer
	_, err := PromptSecret(&out, -1, nil)
	if err == nil {
		t.Fatal("expected an error when the password read fails")
	}
}

// ----------------------------------------------------------------------
//
// PromptNewPassword
//
// ----------------------------------------------------------------------

// TestPromptNewPasswordReadsMaskedPassword - This test verifies the
// genuine success path through a real pty, the same technique
// TestPromptSecretReadsMaskedPassword uses, confirming PromptNewPassword
// reads a freshly typed candidate password back correctly.
func TestPromptNewPasswordReadsMaskedPassword(t *testing.T) {
	master, slave := newPTY(t)

	resCh := runPrompt(func() (string, error) {
		return PromptNewPassword(slave, int(slave.Fd()), nil)
	})
	sendLine(t, master, "n3wpassword")
	res := awaitPrompt(t, resCh, 5*time.Second)

	if res.err != nil {
		t.Fatalf("PromptNewPassword returned unexpected error: %v", res.err)
	}
	if res.value != "n3wpassword" {
		t.Errorf("PromptNewPassword returned %q, want %q", res.value, "n3wpassword")
	}
}

// TestPromptNewPasswordReturnsErrorWhenReadFails - This test verifies the
// same plain error path TestPromptSecretReturnsErrorWhenReadFails verifies
// for PromptSecret, this time for PromptNewPassword.
func TestPromptNewPasswordReturnsErrorWhenReadFails(t *testing.T) {
	var out bytes.Buffer
	_, err := PromptNewPassword(&out, -1, nil)
	if err == nil {
		t.Fatal("expected an error when the password read fails")
	}
}

// ----------------------------------------------------------------------
//
// PromptPasswordConfirmation
//
// ----------------------------------------------------------------------

// TestPromptPasswordConfirmationReadsMaskedPassword - This test verifies
// the genuine success path through a real pty, confirming
// PromptPasswordConfirmation reads a freshly typed confirmation password
// back correctly.
func TestPromptPasswordConfirmationReadsMaskedPassword(t *testing.T) {
	master, slave := newPTY(t)

	resCh := runPrompt(func() (string, error) {
		return PromptPasswordConfirmation(slave, int(slave.Fd()), nil)
	})
	sendLine(t, master, "n3wpassword")
	res := awaitPrompt(t, resCh, 5*time.Second)

	if res.err != nil {
		t.Fatalf("PromptPasswordConfirmation returned unexpected error: %v", res.err)
	}
	if res.value != "n3wpassword" {
		t.Errorf("PromptPasswordConfirmation returned %q, want %q", res.value, "n3wpassword")
	}
}

// TestPromptPasswordConfirmationReturnsErrorWhenReadFails - This test
// verifies the same plain error path the other two masked password prompts
// verify, this time for PromptPasswordConfirmation.
func TestPromptPasswordConfirmationReturnsErrorWhenReadFails(t *testing.T) {
	var out bytes.Buffer
	_, err := PromptPasswordConfirmation(&out, -1, nil)
	if err == nil {
		t.Fatal("expected an error when the password read fails")
	}
}

// ----------------------------------------------------------------------
//
// PromptTOTPCode
//
// ----------------------------------------------------------------------

// TestPromptTOTPCodeReadsMaskedCode - This test verifies the genuine
// success path through a real pty. Unlike promptAndVerifyTOTP,
// PromptTOTPCode has no bufio.Reader fallback for a non-terminal file
// descriptor, see its own doc comment, so this is the one path that
// exercises its masked read, a real pty is not optional here the way it is
// for promptAndVerifyTOTP's tests in mfa_test.go.
func TestPromptTOTPCodeReadsMaskedCode(t *testing.T) {
	master, slave := newPTY(t)

	resCh := runPrompt(func() (string, error) {
		return PromptTOTPCode(slave, int(slave.Fd()), nil)
	})
	sendLine(t, master, "123456")
	res := awaitPrompt(t, resCh, 5*time.Second)

	if res.err != nil {
		t.Fatalf("PromptTOTPCode returned unexpected error: %v", res.err)
	}
	if res.value != "123456" {
		t.Errorf("PromptTOTPCode returned %q, want %q", res.value, "123456")
	}
}

// TestPromptTOTPCodeReturnsErrorWhenReadFails - This test verifies the
// plain error path: an invalid file descriptor makes term.ReadPassword
// fail immediately, with no fallback to fall back to, so PromptTOTPCode
// must surface that as an error.
func TestPromptTOTPCodeReturnsErrorWhenReadFails(t *testing.T) {
	var out bytes.Buffer
	_, err := PromptTOTPCode(&out, -1, nil)
	if err == nil {
		t.Fatal("expected an error when the code read fails")
	}
}
