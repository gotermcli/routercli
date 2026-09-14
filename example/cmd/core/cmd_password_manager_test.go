// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// TestPasswordManagerNegatedClearsPasswordHash - This test verifies that
// running "password manager" with ctx.Negated set, the "no password
// manager" path, clears the current Command Level's PasswordHash back to
// empty, restoring the default of entering that level without a prompt.
// The positive path, which prompts for a new secret through
// auth.PromptSecret, needs a real terminal file descriptor and is covered
// only by the pty based interactive smoke test, the same accepted
// limitation every other secret prompt in this project has.
func TestPasswordManagerNegatedClearsPasswordHash(t *testing.T) {
	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec", PasswordHash: "$0$already-set"}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Negated = true
	// rewireDaemonClient shares this exact TreeStructure with
	// ctx.DaemonClient's Store, so cmd_password_manager.go's
	// ctx.DaemonClient.MutateLevels call sees, and modifies, the same
	// level this test asserts against below. See rewireDaemonClient's doc
	// comment in testhelpers_test.go.
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("password-manager handler returned unexpected error: %v", err)
	}
	if level.PasswordHash != "" {
		t.Errorf("PasswordHash = %q after \"no password manager\", want empty", level.PasswordHash)
	}
}

// TestPasswordManagerErrorsWhenCurrentLevelNotFound - This test verifies
// that "password manager" refuses, rather than panicking on a nil
// CommandLevel, when ctx.Session.CommandLevel names a level not present in
// ctx.Levels.ByName at all, a state the handler's doc comment says should
// not happen in practice but is checked for anyway.
func TestPasswordManagerErrorsWhenCurrentLevelNotFound(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{}}
	ctx.Session = &auth.Session{CommandLevel: "nonexistent"}
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error when ctx.Session.CommandLevel names an unknown level, got nil")
	}
}

// TestPasswordManagerRefusesToSetWhenNotUserSettable - This test verifies
// that "password manager" refuses outright, before ever reaching
// auth.PromptSecret, on a level carrying its own
// VendorDefinedPasswordHash. Without this check, an end user could type
// "password manager", believe they just changed this level's access, and
// never learn that the vendor defined secret is still the one gating
// entry, see command.CommandLevel.EffectivePasswordHash.
func TestPasswordManagerRefusesToSetWhenNotUserSettable(t *testing.T) {
	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec", VendorDefinedPasswordHash: "$6$$vendorhash", Hidden: true}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error when the current level is not user settable, got nil")
	}
	if level.PasswordHash != "" {
		t.Errorf("PasswordHash = %q after a refused \"password manager\", want it left untouched (empty)", level.PasswordHash)
	}
}

// TestPasswordManagerRefusesToClearWhenNotUserSettable - This test is
// TestPasswordManagerRefusesToSetWhenNotUserSettable's negated
// counterpart: "no password manager" must be refused the same way, rather
// than silently clearing an ordinary PasswordHash that was never the
// effective gate in the first place.
func TestPasswordManagerRefusesToClearWhenNotUserSettable(t *testing.T) {
	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec", VendorDefinedPasswordHash: "$6$$vendorhash", Hidden: true, PasswordHash: "$0$leftover"}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	ctx.Negated = true
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected an error when the current level is not user settable, got nil")
	}
	if level.PasswordHash != "$0$leftover" {
		t.Errorf("PasswordHash = %q after a refused \"no password manager\", want it left untouched", level.PasswordHash)
	}
}

// ----------------------------------------------------------------------
//
// "password manager hash"
//
// ----------------------------------------------------------------------

// TestPasswordManagerHashSetsPasswordHashDirectly - This test verifies the
// whole point of "password manager hash": a value already shaped like a
// real hash is stored on the current level's PasswordHash exactly as
// given, with no interactive prompt and no re-hashing.
func TestPasswordManagerHashSetsPasswordHashDirectly(t *testing.T) {
	hash, err := auth.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("auth.HashPassword returned error: %v", err)
	}

	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec"}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager.hash")

	if err := cmd.RunFunc(ctx, []string{hash}); err != nil {
		t.Fatalf("password-manager.hash handler returned unexpected error: %v", err)
	}
	if level.PasswordHash != hash {
		t.Errorf("PasswordHash = %q, want the exact hash given, %q", level.PasswordHash, hash)
	}
}

// TestPasswordManagerHashRefusesAnUnrecognizedValue - This test verifies
// that a value not shaped like any recognized hash, a plaintext password
// typed into the wrong field for instance, is refused rather than stored
// as is, see auth.IsRecognizedHash.
func TestPasswordManagerHashRefusesAnUnrecognizedValue(t *testing.T) {
	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec"}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager.hash")

	if err := cmd.RunFunc(ctx, []string{"hunter2"}); err == nil {
		t.Fatal("expected an error when the given value is not a recognized hash, got nil")
	}
	if level.PasswordHash != "" {
		t.Errorf("PasswordHash = %q after a refused \"password manager hash\", want it left untouched (empty)", level.PasswordHash)
	}
}

// TestPasswordManagerHashRefusesWhenNotUserSettable - This test verifies
// that "password manager hash" is refused the same way "password manager"
// itself is on a level carrying its own VendorDefinedPasswordHash, see
// TestPasswordManagerRefusesToSetWhenNotUserSettable above.
func TestPasswordManagerHashRefusesWhenNotUserSettable(t *testing.T) {
	hash, err := auth.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("auth.HashPassword returned error: %v", err)
	}

	ctx := newTestContext()
	level := &command.CommandLevel{Name: "exec", VendorDefinedPasswordHash: "$6$$vendorhash", Hidden: true}
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{"exec": level}}
	ctx.Session = &auth.Session{CommandLevel: "exec"}
	rewireDaemonClient(ctx)
	cmd := loadTestCommand(t, "password-manager.hash")

	if err := cmd.RunFunc(ctx, []string{hash}); err == nil {
		t.Fatal("expected an error when the current level is not user settable, got nil")
	}
	if level.PasswordHash != "" {
		t.Errorf("PasswordHash = %q after a refused \"password manager hash\", want it left untouched (empty)", level.PasswordHash)
	}
}
