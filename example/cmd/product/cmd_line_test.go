// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"

	"github.com/gotermcli/routercli/command"
)

// configConfigLineLevels - This function builds a minimal
// *command.TreeStructure with a "config" level and a "config-line" level
// whose Parent is "config", the same shape example/var/tree/tree_structure.yaml
// declares, mirroring cmd_interface_test.go's configConfigIfLevels for
// "line" mode instead.
func configConfigLineLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"config":      {Name: "config", Tree: map[string]*command.Command{}},
		"config-line": {Name: "config-line", Parent: "config", PromptSuffix: "(config-line)", Tree: map[string]*command.Command{}},
	}}
}

// ----------------------------------------------------------------------
//
// "line", the config-line push handler
//
// ----------------------------------------------------------------------

// TestLineHandlerPushesConfigLineFrame - This test verifies that "line"
// pushes a config-line frame, with no Context needed, unlike "interface",
// since there is only one, global set of line defaults, not a named
// collection.
func TestLineHandlerPushesConfigLineFrame(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = configConfigLineLevels()
	ctx.Position = command.NewCommandLevelStack("config", "(config)", map[string]*command.Command{})
	cmd := loadTestCommand(t, "line")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("line handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 2 {
		t.Fatalf("expected Position depth 2 after entering line config, got %d", ctx.Position.Depth())
	}
	if ctx.Position.Current().Name != "config-line" {
		t.Errorf("Current().Name = %q, want %q", ctx.Position.Current().Name, "config-line")
	}
}

// TestLineHandlerRefusesOutsideConfig - This test verifies that "line" is
// refused, and leaves Position untouched, when run from anywhere other
// than config, config-line's parent.
func TestLineHandlerRefusesOutsideConfig(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = configConfigLineLevels()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "line")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error entering line config from exec, got nil")
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("expected Position to stay at depth 1 after a refused entry, got %d", ctx.Position.Depth())
	}
}

// ----------------------------------------------------------------------
//
// "line length" and "line width"
//
// ----------------------------------------------------------------------

// TestLineLengthHandlerSetsStateAndDefaultPageLines - This test verifies
// that "line length 30" sets both state.Line.Length, so it renders in
// running-config and survives a restart, and ctx.DefaultPageLines
// directly, so the change takes effect immediately for this process too,
// not only after a future replay.
func TestLineLengthHandlerSetsStateAndDefaultPageLines(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "line.length")

	if err := cmd.RunFunc(ctx, []string{"30"}); err != nil {
		t.Fatalf("line.length handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Line.Length == nil || *state.Line.Length != 30 {
		t.Errorf("state.Line.Length = %v, want a pointer to 30", state.Line.Length)
	}
	if ctx.DefaultPageLines != 30 {
		t.Errorf("ctx.DefaultPageLines = %d, want 30", ctx.DefaultPageLines)
	}
}

// TestLineLengthHandlerRejectsOutOfRangeValues - This test verifies the
// same <0-512> boundary enforcement cmd/core's "terminal length" already
// applies, reused here through the shared "terminal.not_a_number" and
// "terminal.out_of_range" catalog keys.
func TestLineLengthHandlerRejectsOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "513", "not-a-number"} {
		ctx := newTestContext()
		cmd := loadTestCommand(t, "line.length")

		if err := cmd.RunFunc(ctx, []string{raw}); err == nil {
			t.Errorf("expected an error for invalid line length %q, got nil", raw)
		}
		state := ctx.State.(*State)
		if state.Line.Length != nil {
			t.Errorf("state.Line.Length = %v, want nil (unchanged) after a rejected value", state.Line.Length)
		}
	}
}

// TestLineWidthHandlerSetsStateAndDefaultTerminalWidth - This test
// verifies that "line width 100" sets both state.Line.Width and
// ctx.DefaultTerminalWidth, with no effect on state.Line.Length or
// ctx.DefaultPageLines, confirming the two commands are wired to two
// independent targets, the same distinction
// TestTerminalWidthHandlerSetsValidValue already locks down in cmd/core
// for the session scoped commands.
func TestLineWidthHandlerSetsStateAndDefaultTerminalWidth(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "line.width")

	if err := cmd.RunFunc(ctx, []string{"100"}); err != nil {
		t.Fatalf("line.width handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Line.Width == nil || *state.Line.Width != 100 {
		t.Errorf("state.Line.Width = %v, want a pointer to 100", state.Line.Width)
	}
	if ctx.DefaultTerminalWidth != 100 {
		t.Errorf("ctx.DefaultTerminalWidth = %d, want 100", ctx.DefaultTerminalWidth)
	}
	if state.Line.Length != nil {
		t.Errorf("state.Line.Length = %v, want nil (line width must not touch it)", state.Line.Length)
	}
	if ctx.DefaultPageLines != 0 {
		t.Errorf("ctx.DefaultPageLines = %d, want 0 (line width must not touch it)", ctx.DefaultPageLines)
	}
}

// TestLineWidthHandlerRejectsOutOfRangeValues - This test verifies the
// same <0-512> boundary enforcement as
// TestLineLengthHandlerRejectsOutOfRangeValues, for line width.
func TestLineWidthHandlerRejectsOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "5000"} {
		ctx := newTestContext()
		cmd := loadTestCommand(t, "line.width")

		if err := cmd.RunFunc(ctx, []string{raw}); err == nil {
			t.Errorf("expected an error for invalid line width %q, got nil", raw)
		}
	}
}

// ----------------------------------------------------------------------
//
// "line paging" / "no line paging"
//
// ----------------------------------------------------------------------

// TestLinePagingHandlerEnablesByDefault - This test verifies that "paging"
// inside line mode sets both state.Line.Paging to a pointer to true and
// ctx.PagingEnabled to true directly.
func TestLinePagingHandlerEnablesByDefault(t *testing.T) {
	ctx := newTestContext()
	ctx.PagingEnabled = false
	cmd := loadTestCommand(t, "line.paging")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("line.paging handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Line.Paging == nil || !*state.Line.Paging {
		t.Errorf("state.Line.Paging = %v, want a pointer to true", state.Line.Paging)
	}
	if !ctx.PagingEnabled {
		t.Error("ctx.PagingEnabled = false, want true")
	}
}

// TestLinePagingHandlerNegatedDisablesByDefault - This test verifies that
// "no paging" inside line mode sets both state.Line.Paging to a pointer to
// false and ctx.PagingEnabled to false directly, the negated counterpart
// to TestLinePagingHandlerEnablesByDefault.
func TestLinePagingHandlerNegatedDisablesByDefault(t *testing.T) {
	ctx := newTestContext()
	ctx.PagingEnabled = true
	ctx.Negated = true
	cmd := loadTestCommand(t, "line.paging")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("negated line.paging handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Line.Paging == nil || *state.Line.Paging {
		t.Errorf("state.Line.Paging = %v, want a pointer to false", state.Line.Paging)
	}
	if ctx.PagingEnabled {
		t.Error("ctx.PagingEnabled = true, want false")
	}
}
