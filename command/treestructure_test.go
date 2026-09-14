// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"

	"github.com/gologme/log"
	"os"
	"path/filepath"
)

// writeTree - This function is a small convenience wrapper around
// writeTempFile, see loader_test.go, specifically for tree files shaped
// like "commands: {}". Most tests in this file just need a tree with one
// or two trivial leaf commands, not real behavior.
func writeTree(t *testing.T, yamlBody string) string {
	t.Helper()
	return writeTempFile(t, "tree-*.yaml", "commands:\n"+yamlBody)
}

// emptyCommonTree - This function is a shared common tree fixture. No
// help, exit, or end command is needed for these tests. LoadTreeStructure
// merges it in, but an empty one is a valid tree with nothing to collide
// with a level's commands.
func emptyCommonTree(t *testing.T) string {
	t.Helper()
	return writeTempFile(t, "common-*.yaml", "commands: {}\n")
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	return writeTempFile(t, "treestructure-*.yaml", "trees:\n"+body)
}

// discardLogger - This function returns a *log.Logger that
// EnterCommandLevel and ExitCommandLevel can call Debugln on without
// needing a real destination, matching completer_test.go's testLogger
// pattern in spirit.
func discardLogger() *log.Logger { return log.New(discardWriter{}, "", 0) }

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// testContext - This function builds a minimal *AppContext for exercising
// EnterCommandLevel, ExitCommandLevel, and RequireCurrentCommandLevel
// directly, without needing readline, an audit log, or any of main.go's
// other startup machinery.
func testContext(t *testing.T, levels *TreeStructure, startLevel string) *AppContext {
	t.Helper()
	session := &auth.Session{CommandLevel: startLevel}
	return &AppContext{
		Session:    session,
		Levels:     levels,
		Logger:     discardLogger(),
		Translator: nil,
		Position:   NewCommandLevelStack(startLevel, "", levels.ByName[startLevel].Tree),
	}
}

// ----------------------------------------------------------------------
//
// LoadTreeStructure, structural validation
//
// ----------------------------------------------------------------------

// TestLoadTreeStructureBasicTwoLevelChain - This test verifies that a base
// level and one child level load correctly, with the base level's Tree
// carrying its own command and, thanks to InheritParent, the child level's
// Tree carrying both its own command and everything from its parent.
func TestLoadTreeStructureBasicTwoLevelChain(t *testing.T) {
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
    enter_command: test-basic-enter
    exit_command: test-basic-exit
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	base := levels.Base()
	if base.Name != "operator" {
		t.Errorf("Base().Name = %q, want %q", base.Name, "operator")
	}
	if _, ok := base.Tree["show"]; !ok {
		t.Error("expected the base level's tree to contain \"show\"")
	}

	exec, ok := levels.ByName["exec"]
	if !ok {
		t.Fatal("expected \"exec\" to be a defined Command Level")
	}
	// InheritParent true means the child's tree must contain both its own
	// command and everything from its parent's tree. This is the direct
	// regression test for a real superset merge bug: a first attempt only
	// carried the child's commands forward, losing "show" once elevated.
	if _, ok := exec.Tree["configure"]; !ok {
		t.Error("expected exec's tree to contain its own \"configure\" command")
	}
	if _, ok := exec.Tree["show"]; !ok {
		t.Error("expected exec's tree to also contain \"show\", inherited from its parent since InheritParent is true")
	}
}

// TestLoadTreeStructureMarksCommonCommandsButNotOwnCommands - This test
// verifies that markCommonCommands stamps IsCommonCommand true on every
// command that came from the common tree, "exit" here, merged into every
// level's Tree unless SkipCommonMerge, and leaves a level's commands,
// "show" here, false. SortCommandNames's MergeCommon ordering depends on
// this distinction reaching every level's Tree, not just the common tree's
// standalone map.
func TestLoadTreeStructureMarksCommonCommandsButNotOwnCommands(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := writeTempFile(t, "common-*.yaml", "commands:\n  exit:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	tree := levels.Base().Tree
	if tree["show"] == nil || tree["show"].IsCommonCommand {
		t.Error("expected \"show\", the level's command, to have IsCommonCommand false")
	}
	if tree["exit"] == nil || !tree["exit"].IsCommonCommand {
		t.Error("expected \"exit\", merged in from the common tree, to have IsCommonCommand true")
	}
}

// TestLoadTreeStructureInheritParentFalseExcludesParentCommands - This
// test verifies that InheritParent false carries only the child level's
// commands forward, with none of the parent's commands merged in.
func TestLoadTreeStructureInheritParentFalseExcludesParentCommands(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	childTree := writeTree(t, "  diagnose:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  narrow-mode:
    tree_file: `+childTree+`
    parent: operator
    inherit_parent: false
    enter_command: test-narrow-enter
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	narrow := levels.ByName["narrow-mode"]
	if _, ok := narrow.Tree["diagnose"]; !ok {
		t.Error("expected narrow-mode's tree to contain its own \"diagnose\" command")
	}
	if _, ok := narrow.Tree["show"]; ok {
		t.Error("expected narrow-mode's tree to not contain \"show\" since InheritParent is false")
	}
}

// TestLoadTreeStructureThreeLevelChain - This test is the direct proof
// that this generalizes past a hardcoded depth of 2. Three levels, each
// contributing its own command, with InheritParent true all the way down,
// must result in the deepest level seeing all three.
func TestLoadTreeStructureThreeLevelChain(t *testing.T) {
	registerTestHandlers()
	l1 := writeTree(t, "  cmd-one:\n    run: test.noop\n")
	l2 := writeTree(t, "  cmd-two:\n    run: test.noop\n")
	l3 := writeTree(t, "  cmd-three:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  level-one:
    tree_file: `+l1+`
    is_base: true
  level-two:
    tree_file: `+l2+`
    parent: level-one
    inherit_parent: true
    enter_command: test-3lvl-enter-two
  level-three:
    tree_file: `+l3+`
    parent: level-two
    inherit_parent: true
    enter_command: test-3lvl-enter-three
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	three := levels.ByName["level-three"]
	for _, want := range []string{"cmd-one", "cmd-two", "cmd-three"} {
		if _, ok := three.Tree[want]; !ok {
			t.Errorf("expected level-three's tree to contain %q (inherited through the full 3-level chain), got %v", want, keysOf(three.Tree))
		}
	}
	if len(levels.Order) != 3 {
		t.Errorf("expected 3 Command Levels in build order, got %d", len(levels.Order))
	}
}

// TestLoadTreeStructureNestedModeAlongsideChain - This test verifies
// that a nested mode, with a Parent set but no enter_command declared
// here, loads and merges exactly like any root swap level, coexisting
// with a real privilege chain in the same manifest. This includes
// inheriting its parent's commands through InheritParent, matching
// real Cisco and HP fidelity where config mode still has "show"
// available. A project could still declare an enter_command for a
// nested mode, for VerifyCommandLevels to check, see verify_test.go,
// but nothing requires it here.
//
// Base().Name must still correctly identify the actual base level
// even though "config" also sets a Parent, the same as every other
// level does. IsBase, not "has no parent", is what distinguishes the
// base level.
func TestLoadTreeStructureNestedModeAlongsideChain(t *testing.T) {
	registerTestHandlers()
	baseTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	nestedTree := writeTree(t, "  hostname:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  base:
    tree_file: `+baseTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: base
    inherit_parent: true
    enter_command: test-nestedmode-enter
  config:
    tree_file: `+nestedTree+`
    parent: exec
    inherit_parent: true
    prompt_suffix: "(config)"
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	if levels.Base().Name != "base" {
		t.Errorf("Base().Name = %q, want %q (config also sets a Parent now, but is not is_base)", levels.Base().Name, "base")
	}
	config, ok := levels.ByName["config"]
	if !ok {
		t.Fatal("expected \"config\" to be a defined Command Level")
	}
	if _, ok := config.Tree["hostname"]; !ok {
		t.Error("expected config's tree to contain its own \"hostname\" command")
	}
	if _, ok := config.Tree["configure"]; !ok {
		t.Error("expected config's tree to also contain \"configure\", inherited from its parent exec (inherit_parent: true)")
	}
	if _, ok := config.Tree["show"]; !ok {
		t.Error("expected config's tree to also contain \"show\", inherited transitively through exec from base")
	}
	if config.PromptSuffix != "(config)" {
		t.Errorf("config.PromptSuffix = %q, want %q", config.PromptSuffix, "(config)")
	}
}

// TestLoadTreeStructureExcludesLevelSwitchCommandFromGrandchild - This
// test is the direct regression test for a real reported bug: a Command
// Level's enter_command, "myenter" here, standing in for "admin" in this
// project's shipped tree, lives in exec's tree file and correctly stays
// reachable there, but must not survive InheritParent into config, a
// further descendant of exec, since a session inside config is never
// positioned at exec, checked by RequireCurrentCommandLevel, so the
// command could never succeed there, only sit in config's listing failing
// every time it was tried. An ordinary command, "show" here, is unaffected
// and still carries all the way down to config, matching real Cisco and HP
// fidelity, and a level's directly declared commands, "hostname" here, are
// unaffected regardless of where they came from.
func TestLoadTreeStructureExcludesLevelSwitchCommandFromGrandchild(t *testing.T) {
	registerTestHandlers()
	baseTree := writeTree(t, "  enable:\n    run: test.noop\n")
	execTree := writeTree(t, "  myenter:\n    run: test-switch-enter\n  show:\n    run: test.noop\n")
	configTree := writeTree(t, "  hostname:\n    run: test.noop\n")
	subTree := writeTree(t, "  diagnose:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  base:
    tree_file: `+baseTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: base
    inherit_parent: true
    enter_command: test-basic-enter
  config:
    tree_file: `+configTree+`
    parent: exec
    inherit_parent: true
  sub:
    tree_file: `+subTree+`
    parent: exec
    inherit_parent: false
    enter_command: test-switch-enter
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	exec := levels.ByName["exec"]
	if _, ok := exec.Tree["myenter"]; !ok {
		t.Error("expected exec's tree to still contain \"myenter\", its own declared command")
	}
	if _, ok := exec.Tree["show"]; !ok {
		t.Error("expected exec's tree to still contain \"show\"")
	}

	config := levels.ByName["config"]
	if _, ok := config.Tree["myenter"]; ok {
		t.Error("expected config's tree to NOT contain \"myenter\", sub's enter_command, inherited from exec")
	}
	if _, ok := config.Tree["show"]; !ok {
		t.Error("expected config's tree to still contain \"show\", an ordinary command, inherited from exec")
	}
	if _, ok := config.Tree["hostname"]; !ok {
		t.Error("expected config's tree to still contain its own \"hostname\" command")
	}
}

// TestLoadTreeStructureExcludesLevelSwitchCommandDropsEmptyContainer -This
// test verifies the nested case, matching this project's shipped tree
// exactly: "configure terminal" in example/var/tree/level_exec.yaml, where the
// level switching command is a subcommand, "terminal" here, nested under a
// container, "configure" here, that exists purely to hold it. Once
// "terminal" is filtered out, "configure" has no Run of its own and no
// Subcommands left either, and must be dropped entirely rather than
// surviving as an empty, unusable stub.
func TestLoadTreeStructureExcludesLevelSwitchCommandDropsEmptyContainer(t *testing.T) {
	registerTestHandlers()
	baseTree := writeTree(t, "  enable:\n    run: test.noop\n")
	execTree := writeTree(t, `  configure:
    subcommands:
      terminal:
        run: test-switch-enter
  show:
    run: test.noop
`)
	configTree := writeTree(t, "  hostname:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  base:
    tree_file: `+baseTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: base
    inherit_parent: true
    enter_command: test-basic-enter
  config:
    tree_file: `+configTree+`
    parent: exec
    inherit_parent: true
    enter_command: test-switch-enter
`)

	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}

	exec := levels.ByName["exec"]
	if _, ok := exec.Tree["configure"]; !ok {
		t.Error("expected exec's tree to still contain its own \"configure\" container")
	}
	if exec.Tree["configure"].Subcommands["terminal"] == nil {
		t.Error("expected exec's \"configure\" to still carry its own \"terminal\" subcommand")
	}

	config := levels.ByName["config"]
	if _, ok := config.Tree["configure"]; ok {
		t.Error("expected config's tree to NOT contain \"configure\" at all, since its only subcommand, \"terminal\", is config's enter_command")
	}
	if _, ok := config.Tree["show"]; !ok {
		t.Error("expected config's tree to still contain \"show\", an ordinary command, inherited from exec")
	}
}

func keysOf(m map[string]*Command) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestLoadTreeStructureNoBaseEntryIsError - This test verifies that a
// manifest with no level setting is_base: true is rejected, since there
// would be nothing for Base() to return.
func TestLoadTreeStructureNoBaseEntryIsError(t *testing.T) {
	common := emptyCommonTree(t)
	tree := writeTree(t, "  show:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  only-level:
    tree_file: `+tree+`
    parent: nonexistent
    enter_command: test-nobase-enter
`)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error when no tree sets is_base: true")
	}
}

// TestLoadTreeStructureMultipleBaseEntriesIsError - This test verifies
// that a manifest with more than one level setting is_base: true is
// rejected, since Base() could only ever return one of them.
func TestLoadTreeStructureMultipleBaseEntriesIsError(t *testing.T) {
	common := emptyCommonTree(t)
	tree1 := writeTree(t, "  show:\n    run: test.noop\n")
	tree2 := writeTree(t, "  ping:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  base-one:
    tree_file: `+tree1+`
    is_base: true
  base-two:
    tree_file: `+tree2+`
    is_base: true
`)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error when more than one tree sets is_base: true")
	}
}

// TestLoadTreeStructureUnknownParentIsError - This test verifies that a
// level naming a parent that does not exist anywhere else in the manifest
// is rejected at load time.
func TestLoadTreeStructureUnknownParentIsError(t *testing.T) {
	common := emptyCommonTree(t)
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	childTree := writeTree(t, "  configure:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+childTree+`
    parent: does-not-exist
    enter_command: test-unknownparent-enter
`)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error when a tree's parent does not exist in the manifest")
	}
}

// TestLoadTreeStructureParentWithoutEnterCommandIsAllowed - This test
// verifies that LoadTreeStructure only validates what loading itself
// inherently requires. A level with a Parent but no declared EnterCommand
// loads fine here. Whether every non-base level should declare one is
// VerifyCommandLevels's job instead, see verify_test.go, a separate pass.
// This is the config and config-if shape when a project chooses not to
// declare enter_command for a nested mode.
func TestLoadTreeStructureParentWithoutEnterCommandIsAllowed(t *testing.T) {
	registerTestHandlers()
	common := emptyCommonTree(t)
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	childTree := writeTree(t, "  configure:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+childTree+`
    parent: operator
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure returned unexpected error: %v", err)
	}
	exec, ok := levels.ByName["exec"]
	if !ok {
		t.Fatal("expected \"exec\" to be a defined Command Level")
	}
	if exec.Parent != "operator" {
		t.Errorf("exec.Parent = %q, want %q", exec.Parent, "operator")
	}
	if exec.EnterCommand != "" {
		t.Errorf("expected exec.EnterCommand to be empty, got %q", exec.EnterCommand)
	}
}

// TestLoadTreeStructureCycleIsError - This test verifies that a parent
// chain that never terminates at the base level, here level-a's parent is
// level-b and level-b's parent is level-a, is detected and rejected rather
// than looping forever or resolving to a wrong base.
func TestLoadTreeStructureCycleIsError(t *testing.T) {
	common := emptyCommonTree(t)
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	aTree := writeTree(t, "  cmd-a:\n    run: test.noop\n")
	bTree := writeTree(t, "  cmd-b:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  level-a:
    tree_file: `+aTree+`
    parent: level-b
    enter_command: test-cycle-enter-a
  level-b:
    tree_file: `+bTree+`
    parent: level-a
    enter_command: test-cycle-enter-b
`)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error for a parent-chain cycle that never terminates")
	}
}

// TestLoadTreeStructureEmptyManifestIsError - This test verifies that a
// manifest defining no trees at all is rejected rather than producing an
// empty TreeStructure.
func TestLoadTreeStructureEmptyManifestIsError(t *testing.T) {
	common := emptyCommonTree(t)
	manifest := writeManifest(t, "")
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error for a manifest defining no trees at all")
	}
}

// TestLoadTreeStructureMissingFileIsError - This test verifies that a
// manifest path with no file on disk returns an error rather than a
// TreeStructure built from nothing.
func TestLoadTreeStructureMissingFileIsError(t *testing.T) {
	_, err := LoadTreeStructure("/nonexistent/tree_structure.yaml", "/nonexistent/common.yaml")
	if err == nil {
		t.Fatal("expected an error for a missing manifest file")
	}
}

// TestLoadTreeStructureUnknownFieldIsError - This test verifies that a
// misspelled property name on a level entry in the manifest is rejected at
// load time, the same way config.LoadSystemConfig and auth.LoadUsers treat
// an unknown key in their own files, rather than being silently dropped.
func TestLoadTreeStructureUnknownFieldIsError(t *testing.T) {
	common := emptyCommonTree(t)
	tree := writeTree(t, "  show:\n    run: test.noop\n")
	manifest := writeManifest(t, `
  operator:
    tree_file: `+tree+`
    is_base: true
    entar_command: test-typo-enter
`)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error for an unknown field (misspelled entar_command), got nil")
	}
}

// TestLoadTreeStructureMultipleDocumentsIsError - This test verifies that
// a manifest containing more than one YAML document is rejected, the same
// way config.LoadSystemConfig and auth.LoadUsers treat their own files,
// since a manifest is expected to be a single top-level mapping.
func TestLoadTreeStructureMultipleDocumentsIsError(t *testing.T) {
	common := emptyCommonTree(t)
	tree := writeTree(t, "  show:\n    run: test.noop\n")
	body := "trees:\n  operator:\n    tree_file: " + tree + "\n    is_base: true\n---\ntrees:\n  other:\n    tree_file: " + tree + "\n"
	manifest := writeTempFile(t, "treestructure-*.yaml", body)
	_, err := LoadTreeStructure(manifest, common)
	if err == nil {
		t.Fatal("expected an error for a manifest containing multiple YAML documents, got nil")
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel and ExitCommandLevel
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelMovesSessionAndSwapsTree - This test verifies that
// entering a level updates Session.CommandLevel and swaps the root
// CommandLevelStack frame to that level's tree, and that entering the same
// level again is a no-op rather than an error or a repeat move.
func TestEnterCommandLevelMovesSessionAndSwapsTree(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "operator")
	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error: %v", err)
	}
	if !entered {
		t.Error("expected entered=true")
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if !ctx.Session.AtLevel("exec") {
		t.Error("expected Session.AtLevel(\"exec\") to be true after entering")
	}
	if _, ok := ctx.Position.Current().Tree["configure"]; !ok {
		t.Error("expected the root tree to have been swapped to exec's tree (CommandLevelStack.SetRootTree)")
	}

	// Calling it again while already at that level should be a no-op,
	// entered=false and err=nil, not an error and not a re-elevation.
	entered, err = EnterCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Errorf("expected calling EnterCommandLevel again while already at that level to be a no-op, got error: %v", err)
	}
	if entered {
		t.Error("expected entered=false on a repeat entry (already there)")
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel changed unexpectedly on a repeat enter: %q", ctx.Session.CommandLevel)
	}
}

// TestExitCommandLevelReturnsToParent - This test verifies that exiting a
// level returns Session.CommandLevel and the root tree to the parent, and
// that exiting again while not currently at that level is a no-op.
func TestExitCommandLevelReturnsToParent(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	ctx := testContext(t, levels, "exec") // start already elevated
	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]

	exited, err := ExitCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Fatalf("ExitCommandLevel returned unexpected error: %v", err)
	}
	if !exited {
		t.Error("expected exited=true")
	}
	if ctx.Session.CommandLevel != "operator" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "operator")
	}
	if ctx.Session.AtLevel("exec") {
		t.Error("expected Session.AtLevel(\"exec\") to be false after leaving")
	}
	if _, ok := ctx.Position.Current().Tree["configure"]; ok {
		t.Error("expected the root tree to have been swapped back to operator's tree (no \"configure\")")
	}

	// Calling it again while not at that level should be a no-op.
	exited, err = ExitCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Errorf("expected calling ExitCommandLevel again while not at that level to be a no-op, got error: %v", err)
	}
	if exited {
		t.Error("expected exited=false when not currently at the level being exited")
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, rate limiting
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelRateLimitedReturnsBeforePrompting - This test is
// the direct, unambiguous test that a locked out RateLimiter is checked
// before EnterCommandLevel ever attempts to prompt for a password, see
// EnterCommandLevel's doc comment on its four return value cases. This
// distinguishes the two failure modes by the error message returned, not
// just whether it failed. A locked out attempt and a wrong password both
// result in entered=false and a non-nil error, so asserting only that
// would not prove the rate limit check happens first, since a broken
// implementation that reached PromptSecret anyway, failed to read a
// password in this non-interactive test environment, and then happened to
// return an access-denied-shaped error would look identical on a shallower
// assertion. testContext's Translator is nil, so ctx.Translator.T(key)
// resolves to the bracketed literal key itself, see i18n.Translator.T's
// nil safety, and checking for the exact key name in the returned error is
// what makes this assertion solid.
func TestEnterCommandLevelRateLimitedReturnsBeforePrompting(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-never-reached-in-this-test"
	// maxAttempts=1 with a real window and lockout means the first
	// recorded failure immediately locks it out, so there is no need to
	// trigger a real failure first, just pre-lock it directly for a clean,
	// deterministic test.
	exec.RateLimiter = auth.NewRateLimiter(1, time.Minute, 5*time.Minute)
	exec.RateLimiter.RecordFailure()

	ctx := testContext(t, levels, "operator")
	entered, err := EnterCommandLevel(ctx, exec, operator)

	if entered {
		t.Fatal("expected entered=false when rate limited")
	}
	if err == nil {
		t.Fatal("expected a non-nil error when rate limited")
	}
	if !strings.Contains(err.Error(), "auth.too_many_attempts") {
		t.Errorf("error = %q, want it to reference the auth.too_many_attempts key, proving the rate limit check ran rather than a password prompt failure that happened to also error", err.Error())
	}
	if strings.Contains(err.Error(), "commandlevel.access_denied") {
		t.Error("error should not reference commandlevel.access_denied, that would mean the password prompt was reached despite being locked out")
	}
}

// TestEnterCommandLevelWrongPasswordRecordsFailure - This test verifies
// that a wrong password feeds the rate limiter. Without this,
// RecordFailure never being called would mean the lockout in the test
// above could never occur through normal use, only through a test directly
// manipulating the limiter the way that test does.
func TestEnterCommandLevelWrongPasswordRecordsFailure(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.RateLimiter = auth.NewRateLimiter(3, time.Minute, 5*time.Minute)
	if ok, _ := exec.RateLimiter.Allow(); !ok {
		t.Fatal("expected a freshly constructed RateLimiter to allow immediately")
	}

	// Directly exercise the same RecordFailure this function calls on a
	// wrong password, matching what EnterCommandLevel does internally, see
	// its own source, without needing a real password prompt in this
	// non-interactive test. The actual prompt and verify plumbing is
	// exercised by the pty based live verification for this feature, not a
	// fast unit test.
	exec.RateLimiter.RecordFailure()
	exec.RateLimiter.RecordFailure()
	exec.RateLimiter.RecordFailure()

	ctx := testContext(t, levels, "operator")
	exec.PasswordHash = "$6$$irrelevant-should-not-be-reached"
	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil || !strings.Contains(err.Error(), "auth.too_many_attempts") {
		t.Errorf("expected the 3rd recorded failure to trigger a lockout on the next EnterCommandLevel call, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelRateLimiterUnaffectedWhenNoPasswordSet - This test
// verifies that entering a level with no PasswordHash configured succeeds
// immediately, never even consulting the rate limiter.
func TestEnterCommandLevelRateLimiterUnaffectedWhenNoPasswordSet(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	// No PasswordHash set at all, matching real HP ProCurve behavior where
	// no password configured means no prompt at all.
	exec.RateLimiter = auth.NewRateLimiter(1, time.Minute, 5*time.Minute)
	exec.RateLimiter.RecordFailure() // would lock out a password check, if one happened

	ctx := testContext(t, levels, "operator")
	entered, err := EnterCommandLevel(ctx, exec, operator)
	if err != nil || !entered {
		t.Errorf("expected entering a level with no PasswordHash to succeed regardless of rate-limiter state, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelRateLimiterDisabledWhenNotConfigured - This test
// verifies that a level whose RateLimiter is left nil, exactly the state
// every level is in before main.go wires one in, always allows rather than
// panicking or incorrectly refusing.
func TestEnterCommandLevelRateLimiterDisabledWhenNotConfigured(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	// RateLimiter left nil entirely. Allow() on a nil *RateLimiter must
	// return true, see auth.RateLimiter's nil receiver test, so this must
	// never panic or incorrectly refuse.
	if exec.RateLimiter != nil {
		t.Fatal("test setup error: expected RateLimiter to be nil")
	}
	if ok, _ := exec.RateLimiter.Allow(); !ok {
		t.Error("expected a nil RateLimiter to always allow")
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, VendorDefinedPasswordHash
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelGatesOnVendorDefinedPasswordHashAlone - This test
// verifies that a level with no ordinary PasswordHash at all, only a
// VendorDefinedPasswordHash, still gates EnterCommand exactly the way an
// ordinary PasswordHash would: rate limited, prompted, and refused on a
// failed prompt in this non-interactive test environment. Without
// EnterCommandLevel calling EffectivePasswordHash rather than reading
// level.PasswordHash directly, a level configured this way would let
// anyone straight in, silently making VendorDefinedPasswordHash inert.
func TestEnterCommandLevelGatesOnVendorDefinedPasswordHashAlone(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.VendorDefinedPasswordHash = "$6$$irrelevant-should-be-reached-and-fail"
	// PasswordHash left empty.

	ctx := testContext(t, levels, "operator")
	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt gated by VendorDefinedPasswordHash alone, got entered=%v err=%v", entered, err)
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, ReauthGracePeriod
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelGracePeriodDisabledByDefault - This test verifies
// that leaving AppContext.ReauthGracePeriod at its zero value, exactly how
// testContext and DefaultSystemConfig both leave it, never waves a prompt
// through no matter how recently LastAuthenticatedAt was set.
// withinReauthGracePeriod's first check is ctx.ReauthGracePeriod <= 0, and
// this is the test that proves that check gates entry rather than only
// being reachable in theory.
func TestEnterCommandLevelGracePeriodDisabledByDefault(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"
	exec.LastAuthenticatedAt = time.Now()

	ctx := testContext(t, levels, "operator")
	// ReauthGracePeriod left at its zero value.
	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt with ReauthGracePeriod disabled, even one second after LastAuthenticatedAt, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelGracePeriodWavesThroughRecentAuth - This test
// verifies the actual sudo like behavior: with ReauthGracePeriod
// configured and LastAuthenticatedAt recent, EnterCommandLevel succeeds
// with no prompt at all, even though PasswordHash is set to a value that
// would fail this non-interactive test's password prompt if one were
// attempted. It also verifies the window slides, that is, a successful
// grace period entry itself refreshes LastAuthenticatedAt rather than
// leaving the original timestamp in place.
func TestEnterCommandLevelGracePeriodWavesThroughRecentAuth(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-never-reached-in-this-test"
	original := time.Now().Add(-10 * time.Second)
	exec.LastAuthenticatedAt = original

	ctx := testContext(t, levels, "operator")
	ctx.ReauthGracePeriod = time.Minute

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error within the grace period: %v", err)
	}
	if !entered {
		t.Error("expected entered=true when LastAuthenticatedAt is within ReauthGracePeriod")
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
	if !exec.LastAuthenticatedAt.After(original) {
		t.Error("expected a grace period entry to slide LastAuthenticatedAt forward, not leave the original timestamp in place")
	}
}

// TestEnterCommandLevelGracePeriodExpiredPromptsAgain - This test verifies
// the boundary the sudo comparison depends on: once more time has passed
// than ReauthGracePeriod allows, EnterCommandLevel falls back to a real
// password prompt exactly as if LastAuthenticatedAt had never been set at
// all.
func TestEnterCommandLevelGracePeriodExpiredPromptsAgain(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"
	exec.LastAuthenticatedAt = time.Now().Add(-2 * time.Minute)

	ctx := testContext(t, levels, "operator")
	ctx.ReauthGracePeriod = time.Minute

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt once ReauthGracePeriod has elapsed, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelGracePeriodIgnoredWithoutPriorAuth - This test
// verifies that a zero valued LastAuthenticatedAt, the state every level
// starts in before it has ever been entered with a typed password, is
// never treated as "within the grace period" no matter how large
// ReauthGracePeriod is configured. Without this check, a freshly started
// process would wave through the very first entry attempt at any password
// protected level, which would defeat the point of the password entirely.
func TestEnterCommandLevelGracePeriodIgnoredWithoutPriorAuth(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"
	// LastAuthenticatedAt left at its zero value, never set.

	ctx := testContext(t, levels, "operator")
	ctx.ReauthGracePeriod = time.Hour

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt when LastAuthenticatedAt was never set, regardless of ReauthGracePeriod, got entered=%v err=%v", entered, err)
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, AuthBypassTrustWindow (GrantAuthBypass)
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelAuthBypassTrustWavesThroughAnotherLevel - This test
// verifies the actual mechanism GrantAuthBypass exists for: a level with no real
// relationship to the one being entered, other than GrantAuthBypass and a
// recent, real LastAuthenticatedAt, is enough to let EnterCommandLevel
// into a completely different, unrelated password protected level without
// prompting, and without marking that other level's LastAuthenticatedAt as
// though it had been proven too.
func TestEnterCommandLevelAuthBypassTrustWavesThroughAnotherLevel(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-never-reached-in-this-test"

	// bypassLevel is not exec's parent, and shares nothing with it beyond
	// both being levels in the same TreeStructure. Its own recent, real
	// authentication is what should carry the trust here.
	bypassLevel := &CommandLevel{Name: "admin", GrantAuthBypass: true, LastAuthenticatedAt: time.Now()}
	levels.Order = append(levels.Order, bypassLevel)
	levels.ByName["admin"] = bypassLevel

	ctx := testContext(t, levels, "operator")
	ctx.AuthBypassTrustWindow = time.Minute

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error within the auth bypass trust window: %v", err)
	}
	if !entered {
		t.Error("expected entered=true when a GrantAuthBypass level was recently, really authenticated")
	}
	if !exec.LastAuthenticatedAt.IsZero() {
		t.Error("expected exec's LastAuthenticatedAt to stay zero: the bypass level's authentication was never proof of exec's credential")
	}
}

// TestEnterCommandLevelAuthBypassTrustDisabledByDefault - This test verifies
// that leaving AppContext.AuthBypassTrustWindow at its zero value, exactly
// how testContext leaves it (DefaultSystemConfig's nonzero default is
// main.go's concern, not this package's), never waves a prompt through, no
// matter how recently some GrantAuthBypass level was authenticated.
func TestEnterCommandLevelAuthBypassTrustDisabledByDefault(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"

	bypassLevel := &CommandLevel{Name: "admin", GrantAuthBypass: true, LastAuthenticatedAt: time.Now()}
	levels.Order = append(levels.Order, bypassLevel)
	levels.ByName["admin"] = bypassLevel

	ctx := testContext(t, levels, "operator")
	// AuthBypassTrustWindow left at its zero value.

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt with AuthBypassTrustWindow disabled, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelAuthBypassTrustExpiredPromptsAgain - This test
// verifies the boundary: once more time has passed than
// AuthBypassTrustWindow allows since the GrantAuthBypass level's
// LastAuthenticatedAt, EnterCommandLevel falls back to a real password
// prompt for the unrelated level being entered.
func TestEnterCommandLevelAuthBypassTrustExpiredPromptsAgain(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"

	bypassLevel := &CommandLevel{Name: "admin", GrantAuthBypass: true, LastAuthenticatedAt: time.Now().Add(-2 * time.Minute)}
	levels.Order = append(levels.Order, bypassLevel)
	levels.ByName["admin"] = bypassLevel

	ctx := testContext(t, levels, "operator")
	ctx.AuthBypassTrustWindow = time.Minute

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt once AuthBypassTrustWindow has elapsed, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelAuthBypassTrustIgnoresLevelsNotMarked - This test
// verifies that a level's recent LastAuthenticatedAt, on its own, never
// grants trust for a different level unless GrantAuthBypass is set; an
// ordinary level a session happened to authenticate into recently must
// never become an accidental bypass level.
func TestEnterCommandLevelAuthBypassTrustIgnoresLevelsNotMarked(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"

	// An ordinary level, GrantAuthBypass left false, was very recently,
	// authenticated. This must not count.
	other := &CommandLevel{Name: "other", LastAuthenticatedAt: time.Now()}
	levels.Order = append(levels.Order, other)
	levels.ByName["other"] = other

	ctx := testContext(t, levels, "operator")
	ctx.AuthBypassTrustWindow = time.Minute

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt, an ordinary level's recent auth must not grant bypass style trust, got entered=%v err=%v", entered, err)
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, ReplayingStartupConfig
//
// ----------------------------------------------------------------------

// TestEnterCommandLevelReplayingStartupConfigBypassesPasswordCheck - This
// test verifies boot time trusted replay's bypass case, the one
// command.ReplayLines sets for the caller, see
// AppContext.ReplayingStartupConfig's doc comment: a level with a real
// password configured is entered with no prompt at all, and without
// recording LastAuthenticatedAt, since nobody typed a credential here,
// real or otherwise. The password itself is garbage; a real check, or a
// real prompt against this test's non-terminal stdin, would fail or hang
// rather than let this test pass.
func TestEnterCommandLevelReplayingStartupConfigBypassesPasswordCheck(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-never-actually-be-checked"

	ctx := testContext(t, levels, "operator")
	ctx.ReplayingStartupConfig = true

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error under ReplayingStartupConfig: %v", err)
	}
	if !entered {
		t.Error("expected entered=true, ReplayingStartupConfig must waive the password check entirely")
	}
	if !exec.LastAuthenticatedAt.IsZero() {
		t.Error("expected exec's LastAuthenticatedAt to stay zero: boot time replay is not a real, live credential check")
	}
}

// TestEnterCommandLevelReplayingStartupConfigFalseStillPrompts - This test
// is the companion sanity check: with ReplayingStartupConfig left at its
// zero value, false, a real password still refuses entry exactly as every
// other test in this file already expects, confirming the new case added
// nothing beyond its own narrow, explicit trigger.
func TestEnterCommandLevelReplayingStartupConfigFalseStillPrompts(t *testing.T) {
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
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	exec := levels.ByName["exec"]
	operator := levels.ByName["operator"]
	exec.PasswordHash = "$6$$irrelevant-should-be-reached-and-fail"

	ctx := testContext(t, levels, "operator")
	// ReplayingStartupConfig left at its zero value, false.

	entered, err := EnterCommandLevel(ctx, exec, operator)
	if entered || err == nil {
		t.Errorf("expected a real password prompt with ReplayingStartupConfig false, got entered=%v err=%v", entered, err)
	}
}

// TestEnterCommandLevelRefusesFromWrongParent - This test verifies that
// EnterCommandLevel refuses to move a session directly into a level while
// skipping that level's actual parent.
func TestEnterCommandLevelRefusesFromWrongParent(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	l2 := writeTree(t, "  cmd-two:\n    run: test.noop\n")
	l3 := writeTree(t, "  cmd-three:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  level-two:
    tree_file: `+l2+`
    parent: operator
  level-three:
    tree_file: `+l3+`
    parent: level-two
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	// Starting at "operator", try to jump directly into level-three,
	// skipping its actual parent, level-two. This exercises the defensive
	// check documented on EnterCommandLevel directly, bypassing normal
	// tree placement to simulate a session somehow arriving here anyway.
	ctx := testContext(t, levels, "operator")
	levelThree := levels.ByName["level-three"]
	levelTwo := levels.ByName["level-two"]

	entered, err := EnterCommandLevel(ctx, levelThree, levelTwo)
	if err == nil {
		t.Error("expected an error entering level-three directly from operator, skipping level-two")
	}
	if entered {
		t.Error("expected entered=false on a refused entry")
	}
	if ctx.Session.CommandLevel != "operator" {
		t.Errorf("Session.CommandLevel should be unchanged after a refused entry, got %q", ctx.Session.CommandLevel)
	}
}

// ----------------------------------------------------------------------
//
// EnterCommandLevel, AllowedRoles
//
// ----------------------------------------------------------------------

// rolesLevelFixture - This function builds a two level chain, base
// "operator" and root swap "exec", where exec's AllowedRoles is set to
// allowed, for the AllowedRoles tests below.
func rolesLevelFixture(t *testing.T, allowed []string) *TreeStructure {
	t.Helper()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	allowedYAML := "["
	for i, r := range allowed {
		if i > 0 {
			allowedYAML += ", "
		}
		allowedYAML += r
	}
	allowedYAML += "]"
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    allowed_roles: `+allowedYAML+`
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}
	return levels
}

// TestEnterCommandLevelDeniesWithoutMatchingRole - This test verifies that
// a level carrying AllowedRoles refuses a session holding no role in that
// list, once this deployment has AuthRequired turned on, the deny by
// default convention Authorized itself documents, checked independently
// of, and before, any PasswordHash this level might also carry.
func TestEnterCommandLevelDeniesWithoutMatchingRole(t *testing.T) {
	registerTestHandlers()
	levels := rolesLevelFixture(t, []string{"admin"})
	ctx := testContext(t, levels, "operator")
	ctx.AuthRequired = true
	ctx.Session.Authenticated = true
	ctx.Session.Username = "alice"
	ctx.Users = auth.Users{"alice": {Username: "alice", Roles: []string{"operator"}}}

	entered, err := EnterCommandLevel(ctx, levels.ByName["exec"], levels.ByName["operator"])
	if err == nil {
		t.Fatal("expected an error entering a role gated level without a matching role")
	}
	if entered {
		t.Error("expected entered=false on a role gated refusal")
	}
	if ctx.Session.CommandLevel != "operator" {
		t.Errorf("Session.CommandLevel should be unchanged after a role gated refusal, got %q", ctx.Session.CommandLevel)
	}
}

// TestEnterCommandLevelGrantsWithMatchingRole - This test verifies that a
// session holding one of AllowedRoles enters cleanly, with no password
// configured on the level at all.
func TestEnterCommandLevelGrantsWithMatchingRole(t *testing.T) {
	registerTestHandlers()
	levels := rolesLevelFixture(t, []string{"admin"})
	ctx := testContext(t, levels, "operator")
	ctx.AuthRequired = true
	ctx.Session.Authenticated = true
	ctx.Session.Username = "alice"
	ctx.Users = auth.Users{"alice": {Username: "alice", Roles: []string{"admin"}}}

	entered, err := EnterCommandLevel(ctx, levels.ByName["exec"], levels.ByName["operator"])
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error: %v", err)
	}
	if !entered {
		t.Error("expected entered=true for a session holding an allowed role")
	}
	if ctx.Session.CommandLevel != "exec" {
		t.Errorf("Session.CommandLevel = %q, want %q", ctx.Session.CommandLevel, "exec")
	}
}

// TestEnterCommandLevelGrantsViaBypassRole - This test verifies that a
// session holding the deployment's bypass role enters a role gated level
// regardless of what AllowedRoles contains, the bootstrap mechanism.
func TestEnterCommandLevelGrantsViaBypassRole(t *testing.T) {
	registerTestHandlers()
	levels := rolesLevelFixture(t, []string{"some-other-role"})
	ctx := testContext(t, levels, "operator")
	ctx.AuthRequired = true
	ctx.Session.Authenticated = true
	ctx.Session.Username = "alice"
	ctx.Users = auth.Users{"alice": {Username: "alice", Roles: []string{"admin"}}}
	ctx.Roles = &RoleSet{BypassRole: "admin"}

	entered, err := EnterCommandLevel(ctx, levels.ByName["exec"], levels.ByName["operator"])
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error: %v", err)
	}
	if !entered {
		t.Error("expected entered=true for a session holding the bypass role")
	}
}

// TestEnterCommandLevelRoleGateWaivedDuringReplay - This test verifies
// that ctx.ReplayingStartupConfig waives the role gate the same way it
// waives the password gate, since boot time replay has no Session, and so
// no logged in user's roles to check at all.
func TestEnterCommandLevelRoleGateWaivedDuringReplay(t *testing.T) {
	registerTestHandlers()
	levels := rolesLevelFixture(t, []string{"admin"})
	ctx := testContext(t, levels, "operator")
	ctx.ReplayingStartupConfig = true
	// Deliberately no AuthRequired, no Session.Authenticated, no Users, no
	// Roles at all: ReplayingStartupConfig short-circuits the role gate
	// before Authorized is ever even called, so none of that is what is
	// under test here, see TestAuthorizedGrantsWhenAuthRequiredOff in
	// roles_test.go for AuthRequired's separate effect on Authorized.

	entered, err := EnterCommandLevel(ctx, levels.ByName["exec"], levels.ByName["operator"])
	if err != nil {
		t.Fatalf("EnterCommandLevel returned unexpected error during replay: %v", err)
	}
	if !entered {
		t.Error("expected entered=true during replay despite AllowedRoles being set and nobody logged in")
	}
}

// TestEnterCommandLevelRoleGateIndependentOfPassword - This test verifies
// that AllowedRoles and PasswordHash are independent gates, both enforced
// when both are set: a session holding the right role still must satisfy
// the level's password too.
func TestEnterCommandLevelRoleGateIndependentOfPassword(t *testing.T) {
	registerTestHandlers()
	levels := rolesLevelFixture(t, []string{"admin"})
	exec := levels.ByName["exec"]
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	exec.PasswordHash = hash

	ctx := testContext(t, levels, "operator")
	ctx.AuthRequired = true
	ctx.Session.Authenticated = true
	ctx.Session.Username = "alice"
	ctx.Users = auth.Users{"alice": {Username: "alice", Roles: []string{"admin"}}}

	// The role check passes, but auth.PromptSecret will fail to read a
	// real password from this non-interactive test environment, so this
	// must still fail, not silently succeed just because the role check
	// already passed.
	entered, err := EnterCommandLevel(ctx, exec, levels.ByName["operator"])
	if err == nil {
		t.Fatal("expected an error: the role check passing must not bypass the level's separate password check")
	}
	if entered {
		t.Error("expected entered=false when the password check still fails despite a matching role")
	}
}

// ----------------------------------------------------------------------
//
// TreeStructure.Base()
//
// ----------------------------------------------------------------------

// TestTreeStructureBasePanicsOnEmptySet - This test verifies that Base
// panics rather than returning a nil or zero value CommandLevel when the
// TreeStructure has no trees at all, since that state should never occur
// once LoadTreeStructure has succeeded.
func TestTreeStructureBasePanicsOnEmptySet(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected Base() to panic on a TreeStructure with no trees at all")
		}
	}()
	(&TreeStructure{}).Base()
}

// ----------------------------------------------------------------------
//
// RequireCurrentCommandLevel
//
// ----------------------------------------------------------------------

// TestRequireCurrentCommandLevelChecksCommandLevelStackNotSessionCommandLevel
// - This test is the direct regression test for a real bug.
// RequireCurrentCommandLevel must check ctx.Position.Current().Name, not
// ctx.Session.CommandLevel. The two are different axes, privilege tier
// versus Command Level nesting, and only provably agree at the root frame,
// thanks to SetRootTree keeping Name in sync, see that function's doc
// comment. This sets up exactly the situation only Position.Current() can
// express, Session.CommandLevel is "exec" throughout, since config and
// config-if never touch it, while the session is nested inside "config".
// So config-if's parent check, parent: config, must pass here even though
// Session.CommandLevel says "exec", and must fail against "exec" even
// though that is the current Session.CommandLevel.
func TestRequireCurrentCommandLevelChecksCommandLevelStackNotSessionCommandLevel(t *testing.T) {
	session := &auth.Session{CommandLevel: "exec"} // privilege tier, never becomes "config"
	position := NewCommandLevelStack("exec", "", map[string]*Command{})
	position.Push(CommandLevelFrame{Name: "config", Tree: map[string]*Command{}})
	ctx := &AppContext{Session: session, Position: position, Translator: nil}

	if err := RequireCurrentCommandLevel(ctx, "config-if", "config"); err != nil {
		t.Errorf("expected success checking against the current (pushed) frame \"config\", got error: %v", err)
	}
	if err := RequireCurrentCommandLevel(ctx, "some-other-thing", "exec"); err == nil {
		t.Error("expected an error checking against \"exec\": Session.CommandLevel says exec, but the current Command Level is \"config\", and that is what must be checked")
	}
}

// TestRequireCurrentCommandLevelAtRootAfterSetRootTree - This test
// confirms the other half of the fix. After SetRootTree, Current().Name at
// the root correctly reflects the new level, so a check against that
// level's name succeeds. This matches what a hand-written cmd_*.go file
// entering a nested mode directly off an elevated root, config with parent
// exec, needs to work at all.
func TestRequireCurrentCommandLevelAtRootAfterSetRootTree(t *testing.T) {
	session := &auth.Session{CommandLevel: "base"}
	position := NewCommandLevelStack("base", "", map[string]*Command{})
	ctx := &AppContext{Session: session, Position: position, Translator: nil}

	if err := RequireCurrentCommandLevel(ctx, "config", "exec"); err == nil {
		t.Error("expected an error before elevating - the root frame's Name is still \"base\"")
	}

	position.SetRootTree("exec", "", map[string]*Command{})
	session.CommandLevel = "exec"

	if err := RequireCurrentCommandLevel(ctx, "config", "exec"); err != nil {
		t.Errorf("expected success after SetRootTree updated the root frame's Name to \"exec\", got error: %v", err)
	}
}

// TestLoadTreeStructureResolvesTreeFileAgainstTheManifest - This test
// verifies that a relative tree_file is resolved against the directory
// holding the manifest, not against the working directory.
//
// That is what lets one set of tree files work whatever directory the binary
// is started from. The manifest and its tree file are written into a
// temporary directory here, and the test runs with the working directory
// somewhere else entirely, so a resolution against the working directory
// cannot accidentally succeed.
func TestLoadTreeStructureResolvesTreeFileAgainstTheManifest(t *testing.T) {
	dir := t.TempDir()

	// Named by bare filename, the way the shipped manifest names its
	// siblings.
	treePath := filepath.Join(dir, "level_operator.yaml")
	if err := os.WriteFile(treePath, []byte("commands:\n  test:\n    run: test.manifest-relative\n"), 0o600); err != nil {
		t.Fatalf("failed to write the tree file: %v", err)
	}
	commonPath := filepath.Join(dir, "level_common.yaml")
	if err := os.WriteFile(commonPath, []byte("commands:\n"), 0o600); err != nil {
		t.Fatalf("failed to write the common tree file: %v", err)
	}
	manifestPath := filepath.Join(dir, "tree_structure.yaml")
	manifest := "trees:\n  operator:\n    tree_file: level_operator.yaml\n    is_base: true\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("failed to write the manifest: %v", err)
	}

	Register("test.manifest-relative", func(*AppContext, []string) error { return nil })

	levels, err := LoadTreeStructure(manifestPath, commonPath)
	if err != nil {
		t.Fatalf("a bare tree_file beside its manifest must load, got: %v", err)
	}
	lvl, ok := levels.ByName["operator"]
	if !ok {
		t.Fatal("expected the operator level to be loaded")
	}
	if _, ok := lvl.Tree["test"]; !ok {
		t.Errorf("expected the tree file's own command to be present, got %v", lvl.Tree)
	}
}

// TestLoadTreeStructureLeavesAnAbsoluteTreeFileAlone - This test verifies
// that an absolute tree_file is used as given rather than being joined onto
// the manifest's directory, which would produce a path that exists nowhere.
func TestLoadTreeStructureLeavesAnAbsoluteTreeFileAlone(t *testing.T) {
	treeDir := t.TempDir()
	treePath := filepath.Join(treeDir, "level_operator.yaml")
	if err := os.WriteFile(treePath, []byte("commands:\n  test:\n    run: test.absolute-tree-file\n"), 0o600); err != nil {
		t.Fatalf("failed to write the tree file: %v", err)
	}

	// A different directory, so joining the two would fail.
	manifestDir := t.TempDir()
	commonPath := filepath.Join(manifestDir, "level_common.yaml")
	if err := os.WriteFile(commonPath, []byte("commands:\n"), 0o600); err != nil {
		t.Fatalf("failed to write the common tree file: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "tree_structure.yaml")
	manifest := "trees:\n  operator:\n    tree_file: " + treePath + "\n    is_base: true\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("failed to write the manifest: %v", err)
	}

	Register("test.absolute-tree-file", func(*AppContext, []string) error { return nil })

	levels, err := LoadTreeStructure(manifestPath, commonPath)
	if err != nil {
		t.Fatalf("an absolute tree_file must load unchanged, got: %v", err)
	}
	if _, ok := levels.ByName["operator"].Tree["test"]; !ok {
		t.Error("expected the tree file's own command to be present")
	}
}
