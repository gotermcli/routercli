// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import "testing"

// TestSetDescriptionHandlerSetsDescription - This test verifies that
// running "set.description" with one argument sets State.Description to
// that argument, the session-wide counterpart to "description.interface"
// in cmd_description_if.go, scoped to different state and reachable from a
// different mode.
func TestSetDescriptionHandlerSetsDescription(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "set.description")

	if err := cmd.RunFunc(ctx, []string{"a lab router"}); err != nil {
		t.Fatalf("set.description handler returned unexpected error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Description != "a lab router" {
		t.Errorf("Description = %q, want %q", state.Description, "a lab router")
	}
}
