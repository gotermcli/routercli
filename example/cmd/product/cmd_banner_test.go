// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"
)

// TestBannerMOTDHandlerSetsBannerMOTD - This test verifies that running
// the registered "banner.motd" handler with one argument sets
// State.BannerMOTD to that argument, the ordinary, non-negated path a real
// "banner motd" command line takes.
func TestBannerMOTDHandlerSetsBannerMOTD(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "banner.motd")

	if err := cmd.RunFunc(ctx, []string{"Authorized users only"}); err != nil {
		t.Fatalf("banner.motd handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.BannerMOTD != "Authorized users only" {
		t.Errorf("BannerMOTD = %q, want %q", state.BannerMOTD, "Authorized users only")
	}
}

// TestBannerMOTDHandlerNegatedClearsIt - This test verifies that running
// the "banner.motd" handler with ctx.Negated set, the "no banner motd"
// path, clears BannerMOTD back to empty.
func TestBannerMOTDHandlerNegatedClearsIt(t *testing.T) {
	ctx := newTestContext()
	ctx.State.(*State).BannerMOTD = "Authorized users only"
	ctx.Negated = true
	cmd := loadTestCommand(t, "banner.motd")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("banner.motd handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.BannerMOTD != "" {
		t.Errorf("BannerMOTD = %q after \"no banner motd\", want empty", state.BannerMOTD)
	}
}

// TestBannerLoginHandlerSetsBannerLogin - This test verifies that running
// the registered "banner.login" handler with one argument sets
// State.BannerLogin to that argument, with no effect at all on BannerMOTD,
// confirming the two commands are wired to two independent fields.
func TestBannerLoginHandlerSetsBannerLogin(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "banner.login")

	if err := cmd.RunFunc(ctx, []string{"Please enter your credentials"}); err != nil {
		t.Fatalf("banner.login handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.BannerLogin != "Please enter your credentials" {
		t.Errorf("BannerLogin = %q, want %q", state.BannerLogin, "Please enter your credentials")
	}
	if state.BannerMOTD != "" {
		t.Errorf("BannerMOTD = %q, want empty (banner login must not touch it)", state.BannerMOTD)
	}
}

// TestBannerLoginHandlerNegatedClearsIt - This test verifies that running
// the "banner.login" handler with ctx.Negated set, the "no banner login"
// path, clears BannerLogin back to empty.
func TestBannerLoginHandlerNegatedClearsIt(t *testing.T) {
	ctx := newTestContext()
	ctx.State.(*State).BannerLogin = "Please enter your credentials"
	ctx.Negated = true
	cmd := loadTestCommand(t, "banner.login")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("banner.login handler returned error: %v", err)
	}

	state := ctx.State.(*State)
	if state.BannerLogin != "" {
		t.Errorf("BannerLogin = %q after \"no banner login\", want empty", state.BannerLogin)
	}
}
