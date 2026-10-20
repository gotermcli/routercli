// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func testUsers(t *testing.T) Users {
	t.Helper()
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	return Users{
		"alice": {Username: "alice", PasswordHash: hash},
	}
}

// testProvider - This builds the Provider that VerifyLogin and PromptLogin
// take, backed by testUsers, so a call site can pass a real provider rather
// than a Users map.
func testProvider(t *testing.T) Provider {
	t.Helper()
	return NewLocalProvider(testUsers(t))
}

// TestVerifyLoginSuccess - This test verifies that a correct username and
// password produce an authenticated session with no CommandLevel set yet,
// since that is main.go's job right after construction.
func TestVerifyLoginSuccess(t *testing.T) {
	session, ok := VerifyLogin(testProvider(t), "alice", "s3cret")
	if !ok {
		t.Fatal("expected login to succeed with the correct password")
	}
	if session.Username != "alice" || !session.Authenticated {
		t.Errorf("unexpected session: %+v", session)
	}
	if session.CommandLevel != "" {
		t.Errorf("a fresh login session should not have a CommandLevel set yet, see NewSession's doc comment, got %q", session.CommandLevel)
	}
}

// TestVerifyLoginWrongPassword - This test verifies that a known username
// with the wrong password fails login.
func TestVerifyLoginWrongPassword(t *testing.T) {
	_, ok := VerifyLogin(testProvider(t), "alice", "wrong")
	if ok {
		t.Error("expected login to fail with an incorrect password")
	}
}

// TestVerifyLoginUnknownUser - This test verifies that a username with no
// matching User fails login.
func TestVerifyLoginUnknownUser(t *testing.T) {
	_, ok := VerifyLogin(testProvider(t), "nobody", "s3cret")
	if ok {
		t.Error("expected login to fail for a username that does not exist")
	}
}

// TestVerifyLoginUnknownUserTakesRealComparisonTime - This test verifies
// that VerifyLogin takes roughly bcrypt comparison time for an unknown
// username, rather than returning near instantly from a map miss.
//
// Response latency would otherwise reveal whether a username exists.
//
// The floor is a generous 10ms rather than an exact duration. A real bcrypt
// comparison at the default cost runs 60ms to 100ms on ordinary hardware,
// and a bare map lookup is sub-microsecond, so 10ms sits far from both and
// is not flaky on slower or faster machines.
func TestVerifyLoginUnknownUserTakesRealComparisonTime(t *testing.T) {
	start := time.Now()
	VerifyLogin(testProvider(t), "nobody-at-all", "whatever")
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("VerifyLogin for an unknown username returned in %s, want at least 10ms (a real bcrypt comparison); the dummy comparison may have been skipped, reintroducing the username enumeration timing side channel", elapsed)
	}
}

// TestPromptLoginRefusesWhenRateLimited - This test verifies that PromptLogin
// checks the rate limiter immediately after reading the username, before it
// tries to read a password.
//
// A locked out username must produce ErrLoginFailed and one auditFail call
// without term.ReadPassword being reached at all.
//
// That ordering is what makes this testable with no terminal. fd is invalid
// here, so a real read attempt would fail with its own error rather than
// returning ErrLoginFailed.
func TestPromptLoginRefusesWhenRateLimited(t *testing.T) {
	rl := NewKeyedRateLimiter(1, time.Minute, time.Minute)
	rl.RecordFailure("alice") // one failure trips the one attempt limit, locking "alice" out

	var audited []string
	auditFail := func(username string) { audited = append(audited, username) }

	var out bytes.Buffer
	_, err := PromptLogin(strings.NewReader("alice\n"), &out, -1, testProvider(t), testUsers(t), true, 3, rl, nil, auditFail)

	if err != ErrLoginFailed {
		t.Errorf("expected ErrLoginFailed, got %v", err)
	}
	if len(audited) != 1 || audited[0] != "alice" {
		t.Errorf("expected auditFail to be called once with %q, got %v", "alice", audited)
	}
}

// TestPromptLoginReturnsErrorWhenPasswordReadFails - This test verifies the
// plain error path just after a username is read, with no rate limiter
// configured: an invalid file descriptor makes term.ReadPassword fail, and
// PromptLogin must surface that as an error rather than looping or
// panicking.
//
// This is the only slice of the password read branch reachable without a
// real terminal. The success path, and VerifySecondFactor dispatching from
// inside it, need a genuine masked read and are covered by the pty based
// interactive test instead.
func TestPromptLoginReturnsErrorWhenPasswordReadFails(t *testing.T) {
	var out bytes.Buffer
	_, err := PromptLogin(strings.NewReader("alice\n"), &out, -1, testProvider(t), testUsers(t), true, 3, nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error when the password read fails, got nil")
	}
	if err == ErrLoginFailed {
		t.Error("expected the raw password-read error, not ErrLoginFailed, since the attempt loop never actually ran out")
	}
}
