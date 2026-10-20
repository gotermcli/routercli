// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"testing"

	"github.com/gotermcli/routercli/command"
)

// execConfigLevels - This function builds a minimal *command.TreeStructure
// with an "exec" level and a "config" level whose Parent is "exec", the
// same shape example/var/tree/tree_structure.yaml declares, for tests that need
// ctx.Levels.ByName["config"] populated without loading real YAML files.
func execConfigLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"exec":   {Name: "exec", Tree: map[string]*command.Command{}},
		"config": {Name: "config", Parent: "exec", PromptSuffix: "(config)", Tree: map[string]*command.Command{}},
	}}
}

// TestConfigureTerminalPushesConfigFrameFromExec - This test verifies that
// "configure terminal" pushes a new config-named CommandLevelFrame
// carrying the manifest's PromptSuffix and Tree, once the parent check
// against exec passes.
func TestConfigureTerminalPushesConfigFrameFromExec(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execConfigLevels()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "configure.terminal")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("configure.terminal handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 2 {
		t.Fatalf("expected Position depth 2 after entering config mode, got %d", ctx.Position.Depth())
	}
	if ctx.Position.Current().Name != "config" {
		t.Errorf("Current().Name = %q, want %q", ctx.Position.Current().Name, "config")
	}
	if ctx.Position.Current().PromptSuffix != "(config)" {
		t.Errorf("Current().PromptSuffix = %q, want %q", ctx.Position.Current().PromptSuffix, "(config)")
	}
}

// TestConfigureTerminalRefusesFromBase - This test verifies that
// "configure terminal" is refused, and leaves Position untouched, when run
// from anywhere other than exec, config's parent.
func TestConfigureTerminalRefusesFromBase(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = execConfigLevels()
	ctx.Position = command.NewCommandLevelStack("base", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "configure.terminal")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error entering config mode from base, got nil")
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("expected Position to stay at depth 1 after a refused entry, got %d", ctx.Position.Depth())
	}
}
