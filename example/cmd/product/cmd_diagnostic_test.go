// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import "testing"

// TestDiagnosticSelfTestPrintsItsResult - This test verifies that
// "diagnostic.self-test" runs without error and prints its result line.
//
// The printed line is the only thing this command produces, so checking that
// it appears is the difference between proving the handler ran and proving it
// did its job.
func TestDiagnosticSelfTestPrintsItsResult(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "diagnostic.self-test")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("diagnostic.self-test returned unexpected error: %v", runErr)
	}
	if out == "" {
		t.Error("expected diagnostic.self-test to print its result line")
	}
}
