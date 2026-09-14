// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"

	"github.com/gotermcli/routercli/command"
)

// configConfigIfLevels - This function builds a minimal
// *command.TreeStructure with a "config" level and a "config-if" level
// whose Parent is "config", the same shape example/var/tree/tree_structure.yaml
// declares, for tests that need ctx.Levels.ByName["config-if"] populated
// without loading real YAML files.
func configConfigIfLevels() *command.TreeStructure {
	return &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"config":    {Name: "config", Tree: map[string]*command.Command{}},
		"config-if": {Name: "config-if", Parent: "config", PromptSuffix: "(config-if)", Tree: map[string]*command.Command{}},
	}}
}

// TestInterfaceHandlerPushesConfigIfFrameWithNameAsContext - This test
// verifies that "interface eth0" pushes a config-if frame whose Context is
// the interface name argument, args[0], which cmd_description_if.go and
// cmd_shutdown.go later read back to know which interface they are
// editing.
func TestInterfaceHandlerPushesConfigIfFrameWithNameAsContext(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = configConfigIfLevels()
	ctx.Position = command.NewCommandLevelStack("config", "(config)", map[string]*command.Command{})
	cmd := loadTestCommand(t, "interface")

	if err := cmd.RunFunc(ctx, []string{"eth0"}); err != nil {
		t.Fatalf("interface handler returned unexpected error: %v", err)
	}
	if ctx.Position.Depth() != 2 {
		t.Fatalf("expected Position depth 2 after entering interface config, got %d", ctx.Position.Depth())
	}
	if ctx.Position.Current().Name != "config-if" {
		t.Errorf("Current().Name = %q, want %q", ctx.Position.Current().Name, "config-if")
	}
	if got, ok := ctx.Position.Current().Context.(string); !ok || got != "eth0" {
		t.Errorf("Current().Context = %v, want %q", ctx.Position.Current().Context, "eth0")
	}
}

// TestInterfaceHandlerRefusesOutsideConfig - This test verifies that
// "interface eth0" is refused, and leaves Position untouched, when run
// from anywhere other than config, config-if's parent.
func TestInterfaceHandlerRefusesOutsideConfig(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = configConfigIfLevels()
	ctx.Position = command.NewCommandLevelStack("exec", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "interface")

	if err := cmd.RunFunc(ctx, []string{"eth0"}); err == nil {
		t.Fatal("expected an error entering interface config from exec, got nil")
	}
	if ctx.Position.Depth() != 1 {
		t.Errorf("expected Position to stay at depth 1 after a refused entry, got %d", ctx.Position.Depth())
	}
}
