// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"
)

// TestHostnameHandlerSetsHostname - This test verifies that running the
// registered "hostname" handler with one argument sets State.Hostname to
// that argument, the ordinary, non-negated path a real "hostname myrouter"
// command line takes.
func TestHostnameHandlerSetsHostname(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "hostname")

	if err := cmd.RunFunc(ctx, []string{"myrouter"}); err != nil {
		t.Fatalf("hostname handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Hostname != "myrouter" {
		t.Errorf("Hostname = %q, want %q", state.Hostname, "myrouter")
	}
}

// TestHostnameHandlerNegatedResetsToEmpty - This test verifies that
// running the "hostname" handler with ctx.Negated set, the "no hostname"
// path, clears Hostname back to empty rather than to defaultHostname
// itself. defaultHostname is only what the prompt falls back to displaying
// when Hostname is empty, see main.go's buildPrompt, not a value this
// handler stores back onto State.
func TestHostnameHandlerNegatedResetsToEmpty(t *testing.T) {
	ctx := newTestContext()
	ctx.State.(*State).Hostname = "myrouter"
	ctx.Negated = true
	cmd := loadTestCommand(t, "hostname")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("hostname handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.Hostname != "" {
		t.Errorf("Hostname = %q after \"no hostname\", want empty", state.Hostname)
	}
}
