// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import "testing"

// ----------------------------------------------------------------------
//
// Command.EffectivePasswordHash / Command.UserSettablePassword
//
// ----------------------------------------------------------------------

// TestCommandEffectivePasswordHashPrefersVendorDefined - This test
// verifies that a command carrying both fields, itself a configuration
// VerifyVendorDefinedSecrets refuses to allow, still resolves
// deterministically toward the vendor defined value rather than either
// silently ignoring it or panicking, since a caller could still construct
// this combination directly in Go, bypassing YAML and the configuration
// checker entirely.
func TestCommandEffectivePasswordHashPrefersVendorDefined(t *testing.T) {
	c := &Command{PasswordHash: "ordinary", VendorDefinedPasswordHash: "vendor"}
	if got := c.EffectivePasswordHash(); got != "vendor" {
		t.Errorf("EffectivePasswordHash() = %q, want %q", got, "vendor")
	}
}

// TestCommandEffectivePasswordHashFallsBackToOrdinary - This test verifies
// the common case, no VendorDefinedPasswordHash at all, just falls through
// to the ordinary PasswordHash unchanged.
func TestCommandEffectivePasswordHashFallsBackToOrdinary(t *testing.T) {
	c := &Command{PasswordHash: "ordinary"}
	if got := c.EffectivePasswordHash(); got != "ordinary" {
		t.Errorf("EffectivePasswordHash() = %q, want %q", got, "ordinary")
	}
}

// TestCommandEffectivePasswordHashEmptyWhenNeitherSet - This test verifies
// that a command with no secret at all resolves to the empty string, which
// every caller, EnterCommandLevel's sibling check in main.go's runLoop and
// attachPasswordRateLimiters among them, treats as "not password gated".
func TestCommandEffectivePasswordHashEmptyWhenNeitherSet(t *testing.T) {
	c := &Command{}
	if got := c.EffectivePasswordHash(); got != "" {
		t.Errorf("EffectivePasswordHash() = %q, want empty", got)
	}
}

// TestCommandUserSettablePasswordDefaultsTrueWhenUnset - This test verifies
// that PasswordUserSettable left nil resolves to true. A tree file that
// never sets the field is the common case, and it MUST mean the password is
// settable rather than locked.
func TestCommandUserSettablePasswordDefaultsTrueWhenUnset(t *testing.T) {
	c := &Command{}
	if !c.UserSettablePassword() {
		t.Error("expected UserSettablePassword() to default true when PasswordUserSettable is nil")
	}
}

// TestCommandUserSettablePasswordHonorsExplicitFalse - This test verifies
// that an explicit false is respected even with no vendor defined secret
// at all, the standalone hardening case example/var/tree/README.md documents.
func TestCommandUserSettablePasswordHonorsExplicitFalse(t *testing.T) {
	no := false
	c := &Command{PasswordUserSettable: &no}
	if c.UserSettablePassword() {
		t.Error("expected UserSettablePassword() false when PasswordUserSettable is an explicit false")
	}
}

// TestCommandUserSettablePasswordAlwaysFalseWithVendorDefined - This test
// verifies the core rule: a vendor defined secret always wins, even
// against an explicit true, since a caller constructing this directly in
// Go bypasses the configuration checker that would otherwise catch that
// contradiction.
func TestCommandUserSettablePasswordAlwaysFalseWithVendorDefined(t *testing.T) {
	yes := true
	c := &Command{VendorDefinedPasswordHash: "vendor", PasswordUserSettable: &yes}
	if c.UserSettablePassword() {
		t.Error("expected UserSettablePassword() false whenever VendorDefinedPasswordHash is set, regardless of PasswordUserSettable")
	}
}

// ----------------------------------------------------------------------
//
// CommandLevel.EffectivePasswordHash / CommandLevel.UserSettablePassword
//
// ----------------------------------------------------------------------

// TestCommandLevelEffectivePasswordHashPrefersVendorDefined - This test is
// CommandLevel's version of
// TestCommandEffectivePasswordHashPrefersVendorDefined above.
func TestCommandLevelEffectivePasswordHashPrefersVendorDefined(t *testing.T) {
	cl := &CommandLevel{PasswordHash: "ordinary", VendorDefinedPasswordHash: "vendor"}
	if got := cl.EffectivePasswordHash(); got != "vendor" {
		t.Errorf("EffectivePasswordHash() = %q, want %q", got, "vendor")
	}
}

// TestCommandLevelEffectivePasswordHashFallsBackToOrdinary - This test is
// CommandLevel's version of
// TestCommandEffectivePasswordHashFallsBackToOrdinary above.
func TestCommandLevelEffectivePasswordHashFallsBackToOrdinary(t *testing.T) {
	cl := &CommandLevel{PasswordHash: "ordinary"}
	if got := cl.EffectivePasswordHash(); got != "ordinary" {
		t.Errorf("EffectivePasswordHash() = %q, want %q", got, "ordinary")
	}
}

// TestCommandLevelUserSettablePasswordDefaultsTrueWhenUnset - This test is
// CommandLevel's version of
// TestCommandUserSettablePasswordDefaultsTrueWhenUnset above, the exact
// behavior example/cmd/core/cmd_password_manager_test.go's existing tests already
// depend on continuing to work unchanged.
func TestCommandLevelUserSettablePasswordDefaultsTrueWhenUnset(t *testing.T) {
	cl := &CommandLevel{}
	if !cl.UserSettablePassword() {
		t.Error("expected UserSettablePassword() to default true when PasswordUserSettable is nil")
	}
}

// TestCommandLevelUserSettablePasswordAlwaysFalseWithVendorDefined - This
// test is CommandLevel's version of
// TestCommandUserSettablePasswordAlwaysFalseWithVendorDefined above.
func TestCommandLevelUserSettablePasswordAlwaysFalseWithVendorDefined(t *testing.T) {
	yes := true
	cl := &CommandLevel{VendorDefinedPasswordHash: "vendor", PasswordUserSettable: &yes}
	if cl.UserSettablePassword() {
		t.Error("expected UserSettablePassword() false whenever VendorDefinedPasswordHash is set, regardless of PasswordUserSettable")
	}
}

// ----------------------------------------------------------------------
//
// EffectiveHistorySize
//
// ----------------------------------------------------------------------

// TestEffectiveHistorySizeUsesDefaultWhenUnset - This test verifies that
// EffectiveHistorySize returns DefaultHistorySize when no session has ever
// typed "terminal history size", the same "override unset, use the
// fallback" shape paging.EffectivePageLines already gives PageLines and
// DefaultPageLines.
func TestEffectiveHistorySizeUsesDefaultWhenUnset(t *testing.T) {
	ctx := &AppContext{DefaultHistorySize: 500}
	if got := EffectiveHistorySize(ctx); got != 500 {
		t.Errorf("EffectiveHistorySize() = %d, want 500", got)
	}
}

// TestEffectiveHistorySizeUsesOverrideWhenSet - This test verifies that a
// non-nil HistorySize wins outright over DefaultHistorySize, exactly as
// given, once "terminal history size <n>" has been typed.
func TestEffectiveHistorySizeUsesOverrideWhenSet(t *testing.T) {
	n := 50
	ctx := &AppContext{DefaultHistorySize: 500, HistorySize: &n}
	if got := EffectiveHistorySize(ctx); got != 50 {
		t.Errorf("EffectiveHistorySize() = %d, want 50", got)
	}
}

// TestEffectiveHistorySizeHonorsExplicitZero - This test verifies that an
// override of exactly zero, "terminal history size 0", is honored as is,
// not treated as unset the way a bare nil check alone might invite
// confusing with. HistorySize is a *int specifically so zero and "never
// set" are distinguishable at all.
func TestEffectiveHistorySizeHonorsExplicitZero(t *testing.T) {
	zero := 0
	ctx := &AppContext{DefaultHistorySize: 500, HistorySize: &zero}
	if got := EffectiveHistorySize(ctx); got != 0 {
		t.Errorf("EffectiveHistorySize() = %d, want 0", got)
	}
}
