// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// baseExecLevels - This function builds a minimal *command.TreeStructure
// with a "base" level and an "exec" level whose Parent is "base", the same
// shape example/var/tree/tree_structure.yaml declares for enable and disable, for
// tests that need ctx.Levels.ByName["exec"] populated without loading real
// YAML files.
func baseExecLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"base": {Name: "base", Tree: map[string]*command.Command{}},
		"exec": {Name: "exec", Parent: "base", PromptSuffix: "#", Tree: map[string]*command.Command{}},
	}}
}

// TestEnableHandlerEntersExecFromBase - This test verifies that the
// registered "enable" handler moves a session from base into exec,
// swapping the root CommandLevelStack frame in place, updating
// Session.CommandLevel, and returning no error, for a level with no
// PasswordHash configured.
func TestEnableHandlerEntersExecFromBase(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = baseExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "base"}
	ctx.Position = command.NewCommandLevelStack("base", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "enable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("enable handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if ctx.Position.Current().Name != "exec" {
		t.Errorf("Position.Current().Name = %q, want %q", ctx.Position.Current().Name, "exec")
	}
}

// TestEnableHandlerAlreadyAtExecIsNoOp - This test verifies that running
// "enable" again while already at exec leaves the session exactly where it
// was and still returns no error, the "already here" outcome rather than a
// failure.
func TestEnableHandlerAlreadyAtExecIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = baseExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "enable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("enable handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q (unchanged)", ctx.Session.CommandLevel, "exec")
	}
}

// TestDisableHandlerReturnsToBaseFromExec - This test verifies that the
// registered "disable" handler moves a session back from exec to base, the
// mirror image of TestEnableHandlerEntersExecFromBase.
func TestDisableHandlerReturnsToBaseFromExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = baseExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "disable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("disable handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "base" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "base")
	}
	if ctx.Position.Current().Name != "base" {
		t.Errorf("Position.Current().Name = %q, want %q", ctx.Position.Current().Name, "base")
	}
}

// TestDisableHandlerAtBaseIsNoOp - This test verifies that running
// "disable" while already at base, not inside exec at all, is a no-op
// returning no error, rather than erroring or somehow moving the session
// further.
func TestDisableHandlerAtBaseIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = baseExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "base"}
	ctx.Position = command.NewCommandLevelStack("base", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "disable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("disable handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "base" {
		t.Errorf("Session.CommandLevel = %q, want %q (unchanged)", ctx.Session.CommandLevel, "base")
	}
}
