// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import "testing"

// TestRegisterPanicsOnDuplicateName - This test verifies that registering
// the same handler name twice panics immediately, rather than silently
// letting the second registration win, since that would hide a real
// programming error, most likely two command files both claiming the same
// name by mistake, until whichever handler lost the race happened to be
// needed at runtime.
func TestRegisterPanicsOnDuplicateName(t *testing.T) {
	const name = "test-registry-duplicate-name"
	Register(name, func(*AppContext, []string) error { return nil })

	defer func() {
		if recover() == nil {
			t.Error("expected Register to panic on a duplicate name, but it did not")
		}
	}()
	Register(name, func(*AppContext, []string) error { return nil })
}
