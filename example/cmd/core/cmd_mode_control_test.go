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

// TestExitHandlerAtRootReturnsErrQuit - This test verifies that running
// "exit" at the root frame, AtRoot() true, returns command.ErrQuit rather
// than popping, the sentinel main.go's runLoop checks for to end the whole
// program.
func TestExitHandlerAtRootReturnsErrQuit(t *testing.T) {
	ctx := newTestContext()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "exit")

	if err := cmd.RunFunc(ctx, nil); err != command.ErrQuit {
		t.Errorf("exit handler at root returned %v, want command.ErrQuit", err)
	}
}

// TestExitHandlerNestedPopsOneLevel - This test verifies that running
// "exit" from a pushed, non-root frame pops exactly one level and returns
// no error, rather than quitting the program.
func TestExitHandlerNestedPopsOneLevel(t *testing.T) {
	ctx := newTestContext()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	ctx.Position.Push(command.CommandLevelFrame{Name: "config"})
	ctx.Position.Push(command.CommandLevelFrame{Name: "config-if"})
	cmd := loadTestCommand(t, "exit")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("exit handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 2 {
		t.Errorf("Position.Depth() = %d, want 2 after exiting one nested level", ctx.Position.Depth())
	}
	if ctx.Position.Current().Name != "config" {
		t.Errorf("Current().Name = %q, want %q", ctx.Position.Current().Name, "config")
	}
}

// TestEndHandlerJumpsStraightToRootFromAnyDepth - This test verifies that
// running "end" from several levels deep returns straight to the root
// frame in one call, rather than one level at a time like exit.
func TestEndHandlerJumpsStraightToRootFromAnyDepth(t *testing.T) {
	ctx := newTestContext()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	ctx.Position.Push(command.CommandLevelFrame{Name: "config"})
	ctx.Position.Push(command.CommandLevelFrame{Name: "config-if"})
	cmd := loadTestCommand(t, "end")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("end handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("Position.Depth() = %d, want 1 after \"end\"", ctx.Position.Depth())
	}
	if ctx.Position.Current().Name != "exec" {
		t.Errorf("Current().Name = %q, want %q", ctx.Position.Current().Name, "exec")
	}
}

// TestEndHandlerAtRootIsNoOp - This test verifies that "end" run already
// at the root frame stays there and returns no error, rather than quitting
// the way "exit" would.
func TestEndHandlerAtRootIsNoOp(t *testing.T) {
	ctx := newTestContext()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "end")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("end handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("Position.Depth() = %d, want 1", ctx.Position.Depth())
	}
}

// TestEndHandlerFromAdminCrossesBackToExec - This test verifies that "end"
// run from admin, a real Command Level reached through
// command.EnterCommandLevel rather than a pushed Position frame, lands the
// session back in exec, not just at admin's root frame. This is the exact
// case the user reported: typing "end" at router(admin)# visibly did
// nothing, since PopToRoot alone never crosses a Command Level boundary.
func TestEndHandlerFromAdminCrossesBackToExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "admin"}
	ctx.Position = command.NewCommandLevelStack("admin", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "end")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("end handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("Position.Depth() = %d, want 1", ctx.Position.Depth())
	}
}

// TestEndHandlerFromNestedPositionWithinAdminCrossesBackToExec - This test
// verifies that "end" first collapses any pushed frames within admin's
// Position stack, then still crosses the Command Level boundary back to
// exec, exercising both steps "end" now performs in a single call.
func TestEndHandlerFromNestedPositionWithinAdminCrossesBackToExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = adminExecLevels()
	ctx.Session = &auth.Session{CommandLevel: "admin"}
	ctx.Position = command.NewCommandLevelStack("admin", "", map[string]*command.Command{})
	ctx.Position.Push(command.CommandLevelFrame{Name: "admin-nested"})
	cmd := loadTestCommand(t, "end")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("end handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("Position.Depth() = %d, want 1", ctx.Position.Depth())
	}
}

// TestEndHandlerFromUserFallsBackToBaseWhenExecIsNeverReached - This test
// verifies that "end" run from user, whose Parent is base directly rather
// than exec, per example/var/tree/tree_structure.yaml, walks the parent chain
// until it runs out rather than looping or panicking when exec is never
// reached at all.
func TestEndHandlerFromUserFallsBackToBaseWhenExecIsNeverReached(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"base": {Name: "base", Tree: map[string]*command.Command{}},
		"user": {Name: "user", Parent: "base", PromptSuffix: "(user)", Tree: map[string]*command.Command{}},
	}}
	ctx.Session = &auth.Session{CommandLevel: "user"}
	ctx.Position = command.NewCommandLevelStack("user", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "end")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("end handler returned unexpected error: %v", err)
	}
	if ctx.Session.CommandLevel != "base" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "base")
	}
}
