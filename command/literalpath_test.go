// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import "testing"

// TestLiteralCommandPathFindsTopLevelCommand - This test verifies that a
// Run value belonging to a top level command in tree resolves to a single
// element path, its own key.
func TestLiteralCommandPathFindsTopLevelCommand(t *testing.T) {
	tree := map[string]*Command{
		"hostname": {Run: "hostname"},
	}
	got, ok := LiteralCommandPath(tree, "hostname")
	if !ok {
		t.Fatalf("expected LiteralCommandPath to find %q", "hostname")
	}
	if len(got) != 1 || got[0] != "hostname" {
		t.Errorf("got %v, want [\"hostname\"]", got)
	}
}

// TestLiteralCommandPathFindsNestedSubcommand - This test verifies that a
// Run value belonging to a nested subcommand resolves to the full path of
// literal keys leading to it, in order, the same shape "configure
// terminal" takes in example/var/tree/level_exec.yaml, "configure" as the parent
// with "terminal" as its own subcommand.
func TestLiteralCommandPathFindsNestedSubcommand(t *testing.T) {
	tree := map[string]*Command{
		"configure": {
			Subcommands: map[string]*Command{
				"terminal": {Run: "configure.terminal"},
			},
		},
	}
	got, ok := LiteralCommandPath(tree, "configure.terminal")
	if !ok {
		t.Fatalf("expected LiteralCommandPath to find %q", "configure.terminal")
	}
	want := []string{"configure", "terminal"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestLiteralCommandPathFindsDeeplyNestedSubcommand - This test verifies
// the same walk continues correctly three levels deep, not just two, the
// same shape "password manager" takes today, "password" as the parent with
// "manager" as its own subcommand, one level deeper than the "configure
// terminal" case above.
func TestLiteralCommandPathFindsDeeplyNestedSubcommand(t *testing.T) {
	tree := map[string]*Command{
		"a": {
			Subcommands: map[string]*Command{
				"b": {
					Subcommands: map[string]*Command{
						"c": {Run: "a.b.c"},
					},
				},
			},
		},
	}
	got, ok := LiteralCommandPath(tree, "a.b.c")
	if !ok {
		t.Fatalf("expected LiteralCommandPath to find %q", "a.b.c")
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

// TestLiteralCommandPathReturnsFalseWhenNotFound - This test verifies that
// a Run value not present anywhere in tree returns nil, false rather than
// a zero value path a caller could mistake for a real answer.
func TestLiteralCommandPathReturnsFalseWhenNotFound(t *testing.T) {
	tree := map[string]*Command{
		"hostname": {Run: "hostname"},
	}
	got, ok := LiteralCommandPath(tree, "no.such.command")
	if ok {
		t.Fatalf("expected LiteralCommandPath to report not found, got %v, true", got)
	}
	if got != nil {
		t.Errorf("expected a nil path when not found, got %v", got)
	}
}

// TestLiteralCommandPathReturnsFalseOnEmptyTree - This test verifies the
// degenerate case, an empty or nil tree, is handled the same way as not
// found rather than panicking.
func TestLiteralCommandPathReturnsFalseOnEmptyTree(t *testing.T) {
	if _, ok := LiteralCommandPath(nil, "hostname"); ok {
		t.Error("expected LiteralCommandPath on a nil tree to report not found")
	}
	if _, ok := LiteralCommandPath(map[string]*Command{}, "hostname"); ok {
		t.Error("expected LiteralCommandPath on an empty tree to report not found")
	}
}
