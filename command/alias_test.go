// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"reflect"
	"testing"
)

// twoLevelStructureForAliasTests - This function builds a minimal
// TreeStructure with a base level, "operator", and one child, "exec",
// inheriting from it, the same fixture shape
// TestLoadTreeStructureBasicTwoLevelChain already uses, reused here since
// every ExpandAlias test needs at least two distinct levels to prove
// aliasing is scoped per level rather than global.
func twoLevelStructureForAliasTests(t *testing.T) *TreeStructure {
	t.Helper()
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    inherit_parent: true
    enter_command: test-alias-enter
    exit_command: test-alias-exit
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}
	return levels
}

// TestExpandAliasReturnsTokensUnchangedWhenNoAliasDefined - This test
// verifies that a level with no CommandLevel.Aliases ever set, the state
// every level starts in, leaves tokens exactly as given.
func TestExpandAliasReturnsTokensUnchangedWhenNoAliasDefined(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	ctx := testContext(t, levels, "exec")

	tokens := []string{"show"}
	got := ExpandAlias(ctx, tokens)
	if !reflect.DeepEqual(got, tokens) {
		t.Errorf("ExpandAlias = %v, want unchanged %v", got, tokens)
	}
}

// TestExpandAliasReturnsTokensUnchangedForEmptyInput - This test verifies
// that an empty tokens slice, nothing typed, is returned unchanged rather
// than panicking on tokens[0].
func TestExpandAliasReturnsTokensUnchangedForEmptyInput(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	ctx := testContext(t, levels, "exec")

	got := ExpandAlias(ctx, []string{})
	if len(got) != 0 {
		t.Errorf("ExpandAlias with empty input = %v, want empty", got)
	}
}

// TestExpandAliasSubstitutesFirstTokenAndKeepsTrailingArgs - This test
// verifies the core behavior: an alias name in tokens[0] is replaced by
// its own expansion, with the rest of tokens, whatever a session typed
// after the alias itself, left in place and appended after it, matching
// real Cisco's "alias exec" behavior where "sh ip route" with "sh" aliased
// to "show" runs "show ip route".
func TestExpandAliasSubstitutesFirstTokenAndKeepsTrailingArgs(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	ctx := testContext(t, levels, "exec")
	levels.ByName["exec"].Aliases = map[string][]string{
		"sh": {"show"},
	}

	got := ExpandAlias(ctx, []string{"sh", "running-config"})
	want := []string{"show", "running-config"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandAlias = %v, want %v", got, want)
	}
}

// TestExpandAliasIsScopedPerLevel - This test verifies that an alias
// defined against one level, "exec" here, has no effect while a session
// sits in a different level, "operator" here, even though both levels came
// from the same TreeStructure. This is the direct regression test for this
// project's confirmed design decision, aliases scoped per Command Level,
// not one shared global namespace.
func TestExpandAliasIsScopedPerLevel(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	levels.ByName["exec"].Aliases = map[string][]string{
		"sh": {"show"},
	}

	ctx := testContext(t, levels, "operator")
	tokens := []string{"sh"}
	got := ExpandAlias(ctx, tokens)
	if !reflect.DeepEqual(got, tokens) {
		t.Errorf("ExpandAlias in a level with no matching alias = %v, want unchanged %v", got, tokens)
	}
}

// TestExpandAliasIsASinglePassNotRecursive - This test verifies that an
// alias whose own expansion happens to start with another defined alias's
// name is resolved only once, never chased a second time, the same
// restraint real Cisco's "alias exec" applies so a session can never
// define a cycle that hangs command dispatch.
func TestExpandAliasIsASinglePassNotRecursive(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	ctx := testContext(t, levels, "exec")
	levels.ByName["exec"].Aliases = map[string][]string{
		"a": {"b"},
		"b": {"show"},
	}

	got := ExpandAlias(ctx, []string{"a"})
	want := []string{"b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandAlias = %v, want %v (one pass only, not chased through to \"show\")", got, want)
	}
}

// TestExpandAliasLeavesAnUnrelatedFirstTokenUnchanged - This test verifies
// that a first token naming a real command, not an alias, is left
// untouched, the overwhelming common case for every command a session ever
// types.
func TestExpandAliasLeavesAnUnrelatedFirstTokenUnchanged(t *testing.T) {
	levels := twoLevelStructureForAliasTests(t)
	ctx := testContext(t, levels, "exec")
	levels.ByName["exec"].Aliases = map[string][]string{
		"sh": {"show"},
	}

	tokens := []string{"configure", "terminal"}
	got := ExpandAlias(ctx, tokens)
	if !reflect.DeepEqual(got, tokens) {
		t.Errorf("ExpandAlias = %v, want unchanged %v", got, tokens)
	}
}
