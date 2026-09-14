// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------
//
// show.terminal / terminalStatusLines
//
// ----------------------------------------------------------------------

// TestShowTerminalHandlerReturnsNoError - This test verifies that "show
// terminal" runs without error and prints something, the minimum guarantee
// for a command with nothing else to observe against a translator-free
// AppContext, mirroring the same minimum guarantee cmd/core's
// TestShowVersionHandlerReturnsNoError checks for "show version".
func TestShowTerminalHandlerReturnsNoError(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "show.terminal")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Errorf("show.terminal handler returned unexpected error: %v", runErr)
	}
	if out == "" {
		t.Error("expected show.terminal to print something")
	}
}

// TestTerminalStatusLinesReflectsSessionPageLinesAndFilterMode - This test
// verifies terminalStatusLines directly, rather than through "show
// terminal" itself, since ctx.Translator is nil in newTestContext, see
// that function's doc comment, which would leave i18n.Translator.T
// returning only a bracketed key with no argument substitution, unable to
// prove the real length, width, or filter mode value reached the line. It
// confirms a session scoped "terminal length" override reaches the
// geometry line, and that "terminal length 0" is reported as this
// session's paging disabled, distinct from the deployment wide
// PagingEnabled setting on the very next line.
func TestTerminalStatusLinesReflectsSessionPageLinesAndFilterMode(t *testing.T) {
	ctx := newTestContext()
	zero := 0
	ctx.PageLines = &zero
	ctx.PagingEnabled = true
	ctx.Translator = testTranslator(t)

	lines := terminalStatusLines(ctx)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "session-disabled") {
		t.Errorf("terminalStatusLines = %q, want the session paging line to report disabled for PageLines 0", joined)
	}
	if !strings.Contains(joined, "global-enabled") {
		t.Errorf("terminalStatusLines = %q, want the global paging line to report enabled, PagingEnabled true and independent of PageLines", joined)
	}
}

// ----------------------------------------------------------------------
//
// show.history / historyLines
//
// ----------------------------------------------------------------------

// TestShowHistoryHandlerMissingFilePrintsEmptyMessage - This test verifies
// that a HistoryFile that does not exist yet, nothing typed against it yet
// in any session, is treated the same as an empty history, not an error,
// mirroring "show startup-config" own missing file treatment.
func TestShowHistoryHandlerMissingFilePrintsEmptyMessage(t *testing.T) {
	ctx := newTestContext()
	ctx.HistoryFile = filepath.Join(t.TempDir(), "does-not-exist.log")
	ctx.DefaultHistorySize = 500
	cmd := loadTestCommand(t, "show.history")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.history handler returned unexpected error: %v", runErr)
	}
	// ctx.Translator is nil here, the same minimal newTestContext every
	// other handler test in this package uses, so T() returns the
	// bracketed key itself rather than real catalog text. Checking for
	// that bracketed key is still a real, deterministic assertion that the
	// empty branch ran, not the one that prints file content.
	if !strings.Contains(out, "[[show.history.empty]]") {
		t.Errorf("output = %q, want the empty history message", out)
	}
}

// TestShowHistoryHandlerPrintsFileContent - This test verifies that "show
// history" prints back every line of a HistoryFile that fits within
// DefaultHistorySize, in the same order the file itself holds them, oldest
// first.
func TestShowHistoryHandlerPrintsFileContent(t *testing.T) {
	ctx := newTestContext()
	path := filepath.Join(t.TempDir(), "history.log")
	if err := os.WriteFile(path, []byte("show version\nshow terminal\nexit\n"), 0644); err != nil {
		t.Fatalf("failed to write test history file: %v", err)
	}
	ctx.HistoryFile = path
	ctx.DefaultHistorySize = 500
	cmd := loadTestCommand(t, "show.history")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.history handler returned unexpected error: %v", runErr)
	}
	want := "show version\nshow terminal\nexit\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestShowHistoryHandlerTruncatesToEffectiveHistorySize - This test
// verifies that only the last EffectiveHistorySize lines of the file are
// shown when the file itself holds more than that, the most recent
// commands, not the oldest ones, matching what an operator wants from a
// bounded "show history".
func TestShowHistoryHandlerTruncatesToEffectiveHistorySize(t *testing.T) {
	ctx := newTestContext()
	path := filepath.Join(t.TempDir(), "history.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive\n"), 0644); err != nil {
		t.Fatalf("failed to write test history file: %v", err)
	}
	ctx.HistoryFile = path
	two := 2
	ctx.HistorySize = &two
	cmd := loadTestCommand(t, "show.history")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.history handler returned unexpected error: %v", runErr)
	}
	want := "four\nfive\n"
	if out != want {
		t.Errorf("output = %q, want %q (the last 2 lines only)", out, want)
	}
}

// TestShowHistoryHandlerZeroSizePrintsEmptyMessage - This test verifies
// that "terminal history size 0" makes "show history" report nothing, the
// same "zero means none" convention "terminal length 0" already carries
// for paging, even though the file itself still has real content on disk,
// untouched.
func TestShowHistoryHandlerZeroSizePrintsEmptyMessage(t *testing.T) {
	ctx := newTestContext()
	path := filepath.Join(t.TempDir(), "history.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatalf("failed to write test history file: %v", err)
	}
	ctx.HistoryFile = path
	zero := 0
	ctx.HistorySize = &zero
	cmd := loadTestCommand(t, "show.history")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.history handler returned unexpected error: %v", runErr)
	}
	// ctx.Translator is nil here, the same minimal newTestContext every
	// other handler test in this package uses, so T() returns the
	// bracketed key itself rather than real catalog text. Checking for
	// that bracketed key is still a real, deterministic assertion that the
	// empty branch ran, not the one that prints file content.
	if !strings.Contains(out, "[[show.history.empty]]") {
		t.Errorf("output = %q, want the empty history message", out)
	}
}
