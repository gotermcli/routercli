// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// replayProbe - This type is what a handler registered by
// registerReplayTestHandlers below records itself into, reached through
// ctx.State, so a test can observe what an *AppContext field was at the
// moment a replayed line's RunFunc ran, rather than only before or after
// the whole ReplayLines call.
type replayProbe struct {
	sawReplayingStartupConfig bool
	sawNegated                bool
	calls                     int
}

var replayTestHandlersOnce sync.Once

// registerReplayTestHandlers - This function registers a small set of
// handlers this file's tests need, once for the whole test binary, the
// same sync.Once shaped guard registerTestHandlers in loader_test.go
// already uses, since Register panics on a duplicate name and more than
// one test function in this file needs these registered.
func registerReplayTestHandlers() {
	replayTestHandlersOnce.Do(func() {
		Register("test.replay-probe", func(ctx *AppContext, args []string) error {
			if probe, ok := ctx.State.(*replayProbe); ok {
				probe.sawReplayingStartupConfig = ctx.ReplayingStartupConfig
				probe.sawNegated = ctx.Negated
				probe.calls++
			}
			return nil
		})
		Register("test.replay-error", func(ctx *AppContext, args []string) error {
			return fmt.Errorf("deliberate test failure")
		})
	})
}

// TestReplayLinesSkipsBlankAndCommentLines - This test verifies that an
// empty line and a Cisco style "!" comment line, exactly what
// runningConfigLines' own header and trailing separator look like, are
// skipped rather than sent through resolution, while a real line among
// them still runs.
func TestReplayLinesSkipsBlankAndCommentLines(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	if err := ReplayLines(ctx, []string{"", "!  a comment line", "test"}, false); err != nil {
		t.Fatalf("ReplayLines returned unexpected error: %v", err)
	}
	if probe.calls != 1 {
		t.Errorf("probe.calls = %d, want 1 (only the real line should have run)", probe.calls)
	}
}

// TestReplayLinesStopsOnUnresolvableCommand - This test verifies that a
// line naming a command nobody registered stops replay immediately with an
// error naming the offending line, rather than skipping it and continuing
// on to whatever comes next.
func TestReplayLinesStopsOnUnresolvableCommand(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	err = ReplayLines(ctx, []string{"bogus-command-nobody-registered"}, false)
	if err == nil {
		t.Fatal("expected an error for an unresolvable command, got nil")
	}
	if !strings.Contains(err.Error(), "bogus-command-nobody-registered") {
		t.Errorf("expected the error to name the offending line, got: %v", err)
	}
	if probe.calls != 0 {
		t.Errorf("probe.calls = %d, want 0 (nothing valid should have run)", probe.calls)
	}
}

// TestReplayLinesStopsOnValidationError - This test verifies that a line
// failing ValidateArgs, too few arguments for a command requiring at least
// one here, stops replay with an error rather than running the handler
// anyway.
func TestReplayLinesStopsOnValidationError(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    minargs: 1\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	err = ReplayLines(ctx, []string{"test"}, false)
	if err == nil {
		t.Fatal("expected an argument validation error, got nil")
	}
	if probe.calls != 0 {
		t.Errorf("probe.calls = %d, want 0 (validation should have refused before RunFunc ever ran)", probe.calls)
	}
}

// TestReplayLinesStopsOnRunFuncError - This test verifies that a handler's
// error return stops replay immediately, wrapped with which line was being
// replayed, rather than being swallowed.
func TestReplayLinesStopsOnRunFuncError(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  fail:\n    run: test.replay-error\n  test:\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	err = ReplayLines(ctx, []string{"fail", "test"}, false)
	if err == nil {
		t.Fatal("expected the handler's error to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "deliberate test failure") {
		t.Errorf("expected the handler's error text to appear, got: %v", err)
	}
	if probe.calls != 0 {
		t.Errorf("probe.calls = %d, want 0 (replay should have stopped at the failing line, never reaching the one after it)", probe.calls)
	}
}

// TestReplayLinesHandlesNegation - This test verifies that a leading "no"
// is recognized the same way runLoop's dispatch recognizes it, setting
// ctx.Negated for the duration of the call and skipping ValidateArgs, the
// same "a negated line does not need its positive arguments" reasoning
// runLoop itself already follows.
func TestReplayLinesHandlesNegation(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    minargs: 1\n    negatable: true\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	if err := ReplayLines(ctx, []string{"no test"}, false); err != nil {
		t.Fatalf("ReplayLines returned unexpected error for a negated line missing its positive argument: %v", err)
	}
	if probe.calls != 1 {
		t.Fatalf("probe.calls = %d, want 1", probe.calls)
	}
	if !probe.sawNegated {
		t.Error("expected the handler to observe ctx.Negated true while running")
	}
	if ctx.Negated {
		t.Error("expected ctx.Negated to be cleared back to false once ReplayLines returns")
	}
}

// TestReplayLinesTrustedSetsReplayingStartupConfigDuringCall - This test
// verifies that trusted true sets ctx.ReplayingStartupConfig for the
// duration of each replayed line's RunFunc, observed here through
// replayProbe rather than through EnterCommandLevel, and that it is reset
// back to false once ReplayLines returns.
func TestReplayLinesTrustedSetsReplayingStartupConfigDuringCall(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	if err := ReplayLines(ctx, []string{"test"}, true); err != nil {
		t.Fatalf("ReplayLines returned unexpected error: %v", err)
	}
	if !probe.sawReplayingStartupConfig {
		t.Error("expected the handler to observe ctx.ReplayingStartupConfig true while running with trusted=true")
	}
	if ctx.ReplayingStartupConfig {
		t.Error("expected ctx.ReplayingStartupConfig to be reset to false once ReplayLines returns")
	}
}

// TestReplayLinesUntrustedNeverSetsReplayingStartupConfig - This test is
// the companion case: trusted false must never set
// ctx.ReplayingStartupConfig at all, not even briefly.
func TestReplayLinesUntrustedNeverSetsReplayingStartupConfig(t *testing.T) {
	registerReplayTestHandlers()
	opTree := writeTree(t, "  test:\n    run: test.replay-probe\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	probe := &replayProbe{}
	ctx.State = probe

	if err := ReplayLines(ctx, []string{"test"}, false); err != nil {
		t.Fatalf("ReplayLines returned unexpected error: %v", err)
	}
	if probe.sawReplayingStartupConfig {
		t.Error("expected the handler to observe ctx.ReplayingStartupConfig false while running with trusted=false")
	}
}

// TestReplayLinesTrustedBypassesPasswordCheck - This test verifies the
// actual mechanism trusted replay exists for: a line that enters a
// password protected Command Level, "enable" here, through the exact same
// command.EnterCommandLevel every real cmd_enable.go style handler already
// calls, succeeds with no password ever supplied, and no
// LastAuthenticatedAt recorded, when trusted is true. The password hash
// set on exec below is garbage; if anything here tried to check it, or
// worse, tried to prompt for one against this test's non-terminal stdin,
// this test would fail or hang rather than pass.
func TestReplayLinesTrustedBypassesPasswordCheck(t *testing.T) {
	registerReplayTestHandlers()
	registerTestHandlers()
	Register("test.replay-enter-exec", func(ctx *AppContext, args []string) error {
		level := ctx.Levels.ByName["exec"]
		parent := ctx.Levels.ByName["operator"]
		entered, err := EnterCommandLevel(ctx, level, parent)
		if err != nil {
			return err
		}
		if !entered {
			return fmt.Errorf("expected to enter exec, but it reported already there")
		}
		return nil
	})

	opTree := writeTree(t, "  enable:\n    run: test.replay-enter-exec\n")
	execTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	exec.PasswordHash = "$6$$irrelevant-should-never-actually-be-checked"

	ctx := testContext(t, levels, "operator")

	if err := ReplayLines(ctx, []string{"enable"}, true); err != nil {
		t.Fatalf("ReplayLines returned unexpected error entering a password protected level under trusted replay: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if !exec.LastAuthenticatedAt.IsZero() {
		t.Error("expected exec's LastAuthenticatedAt to stay zero: trusted replay is not a real, live credential check")
	}
	if ctx.ReplayingStartupConfig {
		t.Error("expected ctx.ReplayingStartupConfig to be reset to false once ReplayLines returns")
	}
}
