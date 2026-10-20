// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import "testing"

// TestTerminalHistorySizeHandlerSetsValidValue - This test verifies that
// "terminal history size 200", a value inside the configured <0-1000>
// range, sets AppContext.HistorySize to a pointer holding the parsed
// number, mirroring TestTerminalLengthHandlerSetsValidValue in
// cmd_terminal_test.go for the sibling "terminal length" command.
func TestTerminalHistorySizeHandlerSetsValidValue(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.history.size")

	if err := cmd.RunFunc(ctx, []string{"200"}); err != nil {
		t.Fatalf("terminal.history.size handler returned unexpected error: %v", err)
	}
	if ctx.HistorySize == nil {
		t.Fatal("HistorySize = nil, want a pointer to 200")
	}
	if *ctx.HistorySize != 200 {
		t.Errorf("*HistorySize = %d, want 200", *ctx.HistorySize)
	}
}

// TestTerminalHistorySizeHandlerAcceptsZero - This test verifies that
// "terminal history size 0" is accepted, the real, well known "never
// pause" style convention "terminal length 0" already carries, applied
// here to "remember nothing further" instead.
func TestTerminalHistorySizeHandlerAcceptsZero(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.history.size")

	if err := cmd.RunFunc(ctx, []string{"0"}); err != nil {
		t.Fatalf("terminal.history.size handler returned unexpected error for 0: %v", err)
	}
	if ctx.HistorySize == nil || *ctx.HistorySize != 0 {
		t.Errorf("HistorySize = %v, want a pointer to 0", ctx.HistorySize)
	}
}

// TestTerminalHistorySizeHandlerAcceptsUpperBound - This test verifies
// that 1000, historySizeMax, the top of RouterCLI's wider than Cisco's
// <0-256> range, is accepted, not rejected as out of range.
func TestTerminalHistorySizeHandlerAcceptsUpperBound(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.history.size")

	if err := cmd.RunFunc(ctx, []string{"1000"}); err != nil {
		t.Fatalf("terminal.history.size handler returned unexpected error for 1000: %v", err)
	}
	if ctx.HistorySize == nil || *ctx.HistorySize != 1000 {
		t.Errorf("HistorySize = %v, want a pointer to 1000", ctx.HistorySize)
	}
}

// TestTerminalHistorySizeHandlerRejectsNonNumber - This test verifies that
// a non-numeric argument is rejected with an error, rather than panicking
// in strconv.Atoi or silently leaving HistorySize set.
func TestTerminalHistorySizeHandlerRejectsNonNumber(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "terminal.history.size")

	if err := cmd.RunFunc(ctx, []string{"not-a-number"}); err == nil {
		t.Fatal("expected an error for a non-numeric terminal history size, got nil")
	}
	if ctx.HistorySize != nil {
		t.Errorf("HistorySize = %v, want nil (unchanged) after a rejected value", ctx.HistorySize)
	}
}

// TestTerminalHistorySizeHandlerRejectsOutOfRangeValues - This test
// verifies that both ends of the <0-1000> range are enforced: a value
// below the minimum and a value above the maximum are both rejected.
func TestTerminalHistorySizeHandlerRejectsOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "1001"} {
		ctx := newTestContext()
		cmd := loadTestCommand(t, "terminal.history.size")

		if err := cmd.RunFunc(ctx, []string{raw}); err == nil {
			t.Errorf("expected an error for out-of-range terminal history size %q, got nil", raw)
		}
	}
}
