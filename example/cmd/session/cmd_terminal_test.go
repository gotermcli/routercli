// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"testing"

	"github.com/gotermcli/routercli/paging"
)

// TestTerminalLengthHandlerSetsValidValue - This test verifies that
// "terminal length 40", a value inside the configured <0-512> range, sets
// AppContext.PageLines to a pointer holding the parsed number, its real,
// functional counterpart, not merely a cosmetic value stored elsewhere.
func TestTerminalLengthHandlerSetsValidValue(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.length")

	if err := cmd.RunFunc(ctx, []string{"40"}); err != nil {
		t.Fatalf("terminal.length handler returned unexpected error: %v", err)
	}
	if ctx.PageLines == nil {
		t.Fatal("PageLines = nil, want a pointer to 40")
	}
	if *ctx.PageLines != 40 {
		t.Errorf("*PageLines = %d, want 40", *ctx.PageLines)
	}
}

// TestTerminalLengthHandlerAcceptsZeroToDisablePaging - This test verifies
// that "terminal length 0" is accepted, not rejected as below the range's
// minimum, since zero carries the real, well known Cisco meaning "never
// pause", not merely the numeric floor of the <0-512> range.
func TestTerminalLengthHandlerAcceptsZeroToDisablePaging(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.length")

	if err := cmd.RunFunc(ctx, []string{"0"}); err != nil {
		t.Fatalf("terminal.length handler returned unexpected error for 0: %v", err)
	}
	if ctx.PageLines == nil || *ctx.PageLines != 0 {
		t.Errorf("PageLines = %v, want a pointer to 0", ctx.PageLines)
	}
}

// TestTerminalLengthHandlerRejectsNonNumber - This test verifies that a
// non-numeric argument is rejected with an error, rather than panicking in
// strconv.Atoi or silently leaving PageLines set.
func TestTerminalLengthHandlerRejectsNonNumber(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.length")

	if err := cmd.RunFunc(ctx, []string{"not-a-number"}); err == nil {
		t.Fatal("expected an error for a non-numeric terminal length, got nil")
	}
	if ctx.PageLines != nil {
		t.Errorf("PageLines = %v, want nil (unchanged) after a rejected value", ctx.PageLines)
	}
}

// TestTerminalLengthHandlerRejectsOutOfRangeValues - This test verifies
// that both ends of the <0-512> range are enforced: a value below the
// minimum and a value above the maximum are both rejected.
func TestTerminalLengthHandlerRejectsOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "513"} {
		ctx := newTestContext()
		cmd := loadTestCommand(t, "terminal.length")

		if err := cmd.RunFunc(ctx, []string{raw}); err == nil {
			t.Errorf("expected an error for out-of-range terminal length %q, got nil", raw)
		}
	}
}

// TestTerminalWidthHandlerSetsValidValue - This test verifies that
// "terminal width" sets its own field, AppContext.TerminalWidth, with no
// effect at all on AppContext.PageLines, confirming the two commands are
// wired to two independent targets.
func TestTerminalWidthHandlerSetsValidValue(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.width")

	if err := cmd.RunFunc(ctx, []string{"120"}); err != nil {
		t.Fatalf("terminal.width handler returned unexpected error: %v", err)
	}
	if ctx.TerminalWidth == nil {
		t.Fatal("TerminalWidth = nil, want a pointer to 120")
	}
	if *ctx.TerminalWidth != 120 {
		t.Errorf("*TerminalWidth = %d, want 120", *ctx.TerminalWidth)
	}
	if ctx.PageLines != nil {
		t.Errorf("PageLines = %v, want nil (terminal width must not touch it)", ctx.PageLines)
	}
}

// TestTerminalWidthHandlerRejectsOutOfRangeValues - This test verifies the
// same <0-512> boundary enforcement for terminal width as
// TestTerminalLengthHandlerRejectsOutOfRangeValues covers for terminal
// length.
func TestTerminalWidthHandlerRejectsOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "5000"} {
		ctx := newTestContext()
		cmd := loadTestCommand(t, "terminal.width")

		if err := cmd.RunFunc(ctx, []string{raw}); err == nil {
			t.Errorf("expected an error for out-of-range terminal width %q, got nil", raw)
		}
	}
}

// TestTerminalFilterModeHandlerSetsSubstring - This test verifies that
// "terminal filter-mode substring" sets FilterMode to
// paging.FilterModeSubstring.
func TestTerminalFilterModeHandlerSetsSubstring(t *testing.T) {
	ctx := newTestContext()
	ctx.FilterMode = paging.FilterModeRegex
	cmd := loadTestCommand(t, "terminal.filter-mode")

	if err := cmd.RunFunc(ctx, []string{"substring"}); err != nil {
		t.Fatalf("terminal.filter-mode handler returned unexpected error: %v", err)
	}
	if ctx.FilterMode != paging.FilterModeSubstring {
		t.Errorf("FilterMode = %v, want FilterModeSubstring", ctx.FilterMode)
	}
}

// TestTerminalFilterModeHandlerSetsRegex - This test verifies that
// "terminal filter-mode regex" sets FilterMode to paging.FilterModeRegex,
// the boundary's other side from
// TestTerminalFilterModeHandlerSetsSubstring.
func TestTerminalFilterModeHandlerSetsRegex(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.filter-mode")

	if err := cmd.RunFunc(ctx, []string{"regex"}); err != nil {
		t.Fatalf("terminal.filter-mode handler returned unexpected error: %v", err)
	}
	if ctx.FilterMode != paging.FilterModeRegex {
		t.Errorf("FilterMode = %v, want FilterModeRegex", ctx.FilterMode)
	}
}

// TestTerminalFilterModeHandlerRejectsUnknownWord - This test verifies
// that a word other than "substring" or "regex" is rejected with an error,
// rather than silently leaving FilterMode unchanged with no indication
// anything was wrong.
func TestTerminalFilterModeHandlerRejectsUnknownWord(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.filter-mode")

	if err := cmd.RunFunc(ctx, []string{"fuzzy"}); err == nil {
		t.Fatal("expected an error for an unrecognized filter mode, got nil")
	}
}
