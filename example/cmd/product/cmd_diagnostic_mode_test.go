// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// execDiagnosticLevels - This function builds a minimal
// *command.TreeStructure with an "exec" level and a "diagnostic" level
// whose Parent is "exec", the same shape example/var/tree/tree_structure.yaml
// declares, for tests that need ctx.Levels.ByName["diagnostic"] populated
// without loading real YAML files.
func execDiagnosticLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"exec":       {Name: "exec", Tree: map[string]*command.Command{}},
		"diagnostic": {Name: "diagnostic", Parent: "exec", PromptSuffix: "(diag)", Tree: map[string]*command.Command{}},
	}}
}

// TestDiagnosticModeHandlerEntersFromExec - This test verifies that the
// registered "diagnostic-mode" handler moves a session from exec into
// diagnostic, swapping the root frame and updating Session.CommandLevel.
func TestDiagnosticModeHandlerEntersFromExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execDiagnosticLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "diagnostic-mode")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("diagnostic-mode handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "diagnostic" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "diagnostic")
	}
	if ctx.Position.Current().PromptSuffix != "(diag)" {
		t.Errorf("Position.Current().PromptSuffix = %q, want %q", ctx.Position.Current().PromptSuffix, "(diag)")
	}
}

// TestDiagnosticModeHandlerRefusesFromBase - This test verifies that
// "diagnostic-mode" is refused when the session is not currently at
// diagnostic's parent, exec, the same RequireCurrentCommandLevel check
// every root swap Command Level enforces.
func TestDiagnosticModeHandlerRefusesFromBase(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execDiagnosticLevels()
	ctx.Session = &auth.Session{CommandLevel: ""}
	ctx.Position = command.NewCommandLevelStack("base", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "diagnostic-mode")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error entering diagnostic mode from base, got nil")
	}
	if ctx.Session.CommandLevel == "diagnostic" {
		t.Error("Session.CommandLevel should not have changed on a refused entry")
	}
}

// TestExitDiagnosticModeHandlerReturnsToExec - This test verifies that
// "exit-diagnostic-mode" moves a session back from diagnostic to exec.
func TestExitDiagnosticModeHandlerReturnsToExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execDiagnosticLevels()
	ctx.Session = &auth.Session{CommandLevel: "diagnostic"}
	ctx.Position = command.NewCommandLevelStack("diagnostic", "(diag)", map[string]*command.Command{})
	cmd := loadTestCommand(t, "exit-diagnostic-mode")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("exit-diagnostic-mode handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
}

// TestExitDiagnosticModeHandlerAtExecIsNoOp - This test verifies that
// running "exit-diagnostic-mode" while not inside diagnostic is a no-op
// returning no error.
func TestExitDiagnosticModeHandlerAtExecIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execDiagnosticLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "exit-diagnostic-mode")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("exit-diagnostic-mode handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q (unchanged)", ctx.Session.CommandLevel, "exec")
	}
}
