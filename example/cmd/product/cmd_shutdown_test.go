// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import "testing"

// TestInterfaceShutdownHandlerSetsShutdownTrue - This test verifies that
// running "interface.shutdown" marks the current interface as
// administratively shut down, the ordinary, non-negated path.
func TestInterfaceShutdownHandlerSetsShutdownTrue(t *testing.T) {
	ctx := newConfigIfTestContext("eth0")
	cmd := loadTestCommand(t, "interface.shutdown")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("interface.shutdown handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if !state.Interfaces["eth0"].Shutdown {
		t.Error("expected Interfaces[\"eth0\"].Shutdown to be true")
	}
}

// TestInterfaceShutdownHandlerNegatedSetsShutdownFalse - This test
// verifies that running "interface.shutdown" with ctx.Negated set, the "no
// shutdown" path, administratively re-enables the interface, the same
// handler running with the opposite outcome rather than a separate
// registration, matching command.Resolve's "no" mechanism.
func TestInterfaceShutdownHandlerNegatedSetsShutdownFalse(t *testing.T) {
	ctx := newConfigIfTestContext("eth0")
	ctx.State.(*State).Interface("eth0").Shutdown = true
	ctx.Negated = true
	cmd := loadTestCommand(t, "interface.shutdown")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("interface.shutdown handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Interfaces["eth0"].Shutdown {
		t.Error("expected Interfaces[\"eth0\"].Shutdown to be false after \"no shutdown\"")
	}
}
