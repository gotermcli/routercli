// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"

	"github.com/gotermcli/routercli/command"
)

// newConfigIfTestContext - This function builds a *command.AppContext with
// a config-if frame already pushed, Context set to ifaceName, the state
// cmd_description_if.go and cmd_shutdown.go both expect: a session already
// inside "interface <name>", the same frame shape cmd_interface.go's
// handler pushes.
func newConfigIfTestContext(ifaceName string) *command.AppContext {
	ctx := newTestContext()
	ctx.Position = command.NewCommandLevelStack("config", "(config)", map[string]*command.Command{})
	ctx.Position.Push(command.CommandLevelFrame{Name: "config-if", PromptSuffix: "(config-if)", Context: ifaceName})
	return ctx
}

// TestDescriptionInterfaceHandlerSetsDescription - This test verifies that
// running "description.interface" with one argument sets the current
// interface's Description to that argument, the ordinary, non-negated
// path.
func TestDescriptionInterfaceHandlerSetsDescription(t *testing.T) {
	ctx := newConfigIfTestContext("eth0")
	cmd := loadTestCommand(t, "description.interface")

	if err := cmd.RunFunc(ctx, []string{"uplink to core"}); err != nil {
		t.Fatalf("description.interface handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	iface := state.Interfaces["eth0"]
	if iface == nil || iface.Description != "uplink to core" {
		t.Errorf("Interfaces[\"eth0\"].Description = %+v, want %q", iface, "uplink to core")
	}
}

// TestDescriptionInterfaceHandlerNegatedClearsDescription - This test
// verifies that running "description.interface" with ctx.Negated set, the
// "no description" path, clears the current interface's Description back
// to empty, ignoring any leftover args rather than erroring on them.
func TestDescriptionInterfaceHandlerNegatedClearsDescription(t *testing.T) {
	ctx := newConfigIfTestContext("eth0")
	ctx.State.(*State).Interface("eth0").Description = "uplink to core"
	ctx.Negated = true
	cmd := loadTestCommand(t, "description.interface")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("description.interface handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Interfaces["eth0"].Description != "" {
		t.Errorf("Description = %q after \"no description\", want empty", state.Interfaces["eth0"].Description)
	}
}

// TestDescriptionInterfaceHandlerScopedToItsOwnInterface - This test
// verifies that setting the description for one interface does not touch
// another interface's entry, confirming State.Interface's per-name lazy
// creation keys correctly rather than sharing a single implicit slot.
func TestDescriptionInterfaceHandlerScopedToItsOwnInterface(t *testing.T) {
	ctx := newConfigIfTestContext("eth0")
	ctx.State.(*State).Interface("eth1").Description = "unrelated"
	cmd := loadTestCommand(t, "description.interface")

	if err := cmd.RunFunc(ctx, []string{"eth0 only"}); err != nil {
		t.Fatalf("description.interface handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Interfaces["eth1"].Description != "unrelated" {
		t.Errorf("eth1's Description changed unexpectedly to %q", state.Interfaces["eth1"].Description)
	}
	if state.Interfaces["eth0"].Description != "eth0 only" {
		t.Errorf("eth0's Description = %q, want %q", state.Interfaces["eth0"].Description, "eth0 only")
	}
}
