// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"strings"
	"testing"
)

// TestPruneDisabledCommandsRemovesDisabledCommand - This test verifies
// that a top-level command whose Requires flag is false in enabled is
// deleted from tree entirely.
func TestPruneDisabledCommandsRemovesDisabledCommand(t *testing.T) {
	tree := map[string]*Command{
		"totp": {Requires: "totp"},
	}
	enabled := map[string]bool{"totp": false}

	if err := PruneDisabledCommands(tree, enabled, ""); err != nil {
		t.Fatalf("PruneDisabledCommands returned unexpected error: %v", err)
	}
	if _, ok := tree["totp"]; ok {
		t.Error("expected \"totp\" to be removed once its requires flag is false")
	}
}

// TestPruneDisabledCommandsRemovesSubcommandsWithTheirParent - This test
// verifies that removing a disabled container command also removes
// everything beneath it, since the whole node is deleted from tree rather
// than only being marked somehow.
func TestPruneDisabledCommandsRemovesSubcommandsWithTheirParent(t *testing.T) {
	tree := map[string]*Command{
		"totp": {
			Requires: "totp",
			Subcommands: map[string]*Command{
				"enable":  {},
				"disable": {},
			},
		},
	}
	enabled := map[string]bool{"totp": false}

	if err := PruneDisabledCommands(tree, enabled, ""); err != nil {
		t.Fatalf("PruneDisabledCommands returned unexpected error: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("expected an empty tree once its only command was removed, got %v", tree)
	}
}

// TestPruneDisabledCommandsKeepsEnabledCommand - This test verifies that a
// command whose Requires flag is true in enabled stays in the tree.
func TestPruneDisabledCommandsKeepsEnabledCommand(t *testing.T) {
	tree := map[string]*Command{
		"totp": {Requires: "totp"},
	}
	enabled := map[string]bool{"totp": true}

	if err := PruneDisabledCommands(tree, enabled, ""); err != nil {
		t.Fatalf("PruneDisabledCommands returned unexpected error: %v", err)
	}
	if _, ok := tree["totp"]; !ok {
		t.Error("expected \"totp\" to stay in the tree once its requires flag is true")
	}
}

// TestPruneDisabledCommandsKeepsCommandWithNoRequires - This test verifies
// that a command with an empty Requires is always kept, regardless of what
// enabled contains, or whether enabled is empty altogether, since an empty
// Requires means this command was never gated on any flag in the first
// place.
func TestPruneDisabledCommandsKeepsCommandWithNoRequires(t *testing.T) {
	tree := map[string]*Command{
		"show": {},
	}

	if err := PruneDisabledCommands(tree, map[string]bool{}, ""); err != nil {
		t.Fatalf("PruneDisabledCommands returned unexpected error: %v", err)
	}
	if _, ok := tree["show"]; !ok {
		t.Error("expected \"show\" to stay in the tree, it has no Requires at all")
	}
}

// TestPruneDisabledCommandsRecursesIntoKeptSubcommands - This test
// verifies that a command whose own Requires is satisfied still has its
// Subcommands walked, so a nested command's, independent Requires is still
// honored.
func TestPruneDisabledCommandsRecursesIntoKeptSubcommands(t *testing.T) {
	tree := map[string]*Command{
		"totp": {
			Requires: "totp",
			Subcommands: map[string]*Command{
				"enable":  {},
				"upgrade": {Requires: "totp_v2"},
			},
		},
	}
	enabled := map[string]bool{"totp": true, "totp_v2": false}

	if err := PruneDisabledCommands(tree, enabled, ""); err != nil {
		t.Fatalf("PruneDisabledCommands returned unexpected error: %v", err)
	}
	sub := tree["totp"].Subcommands
	if _, ok := sub["enable"]; !ok {
		t.Error("expected \"totp enable\" to stay, it has no Requires of its own")
	}
	if _, ok := sub["upgrade"]; ok {
		t.Error("expected \"totp upgrade\" to be removed, its own requires flag is false")
	}
}

// TestPruneDisabledCommandsErrorsOnUnrecognizedFlagName - This test
// verifies that a Requires value with no matching key in enabled at all is
// a hard error, rather than silently being treated as disabled, or worse,
// as enabled. A typo'd flag name must fail loudly at startup, the same
// convention every other malformed piece of configuration in this project
// follows.
func TestPruneDisabledCommandsErrorsOnUnrecognizedFlagName(t *testing.T) {
	tree := map[string]*Command{
		"totp": {Requires: "totp_typo"},
	}
	enabled := map[string]bool{"totp": true}

	err := PruneDisabledCommands(tree, enabled, "")
	if err == nil {
		t.Fatal("expected an error for a Requires value with no matching flag, got nil")
	}
}

// TestPruneDisabledCommandsErrorIncludesCommandPath - This test verifies
// that the error for an unrecognized flag names the actual failing
// command, including its full path built from the path parameter, so a
// startup failure points directly at the offending tree file entry instead
// of just naming a bare flag string with no context.
func TestPruneDisabledCommandsErrorIncludesCommandPath(t *testing.T) {
	tree := map[string]*Command{
		"enable": {Requires: "totp_typo"},
	}

	err := PruneDisabledCommands(tree, map[string]bool{}, "totp")
	if err == nil {
		t.Fatal("expected an error for a Requires value with no matching flag, got nil")
	}
	const want = `"totp enable"`
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("expected the error to mention %s, got %q", want, got)
	}
}
