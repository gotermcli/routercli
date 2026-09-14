// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

// Package clitest holds small helpers shared by the test files of more
// than one command package. It lives under internal/ so nothing outside
// this module can import it, and it is imported only from _test.go files,
// so it is never linked into either shipped binary.
//
// This package holds the test helpers cmd/core, cmd/session, and
// cmd/product all need, so one copy serves all three rather than each
// carrying its own.
package clitest

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/creack/pty"
)

// CaptureStdout - This function runs fn with os.Stdout temporarily
// redirected to an in-memory pipe, and returns everything fn printed. A
// number of command handlers print straight to os.Stdout rather than
// through a writer carried on *command.AppContext, the same way a real
// Cisco or HP CLI's output is not something its command handlers thread a
// writer parameter through either. This is the smallest way to observe
// that output directly from a test, restoring the real os.Stdout afterward
// regardless of whether fn panics.
func CaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	real := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = real }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	return buf.String()
}

// NewPTY - This function opens a real pseudo terminal and returns both
// ends, registering a cleanup that closes them when the test finishes.
//
// A test needing a genuine terminal device, for a masked password read or
// a terminal size query, uses this rather than a pipe.
func NewPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, s, err := pty.Open()
	if err != nil {
		t.Fatalf("failed to open a pseudo terminal: %v", err)
	}
	t.Cleanup(func() {
		m.Close()
		s.Close()
	})
	return m, s
}
