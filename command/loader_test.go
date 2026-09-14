// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// testHandlersOnce - This variable registers a couple of dummy handlers
// exactly once for the whole test binary. Register panics on a duplicate
// name, and multiple test functions in this file each need something in
// the registry to resolve against. This package intentionally has nothing
// registered by default, that only happens when cmd/core or cmd/product is
// imported, and these tests correctly do not depend on either import.
var testHandlersOnce sync.Once

func registerTestHandlers() {
	testHandlersOnce.Do(func() {
		Register("test.noop", func(ctx *AppContext, args []string) error { return nil })
		// test-switch-enter stands in for a real product's level switching
		// command, "admin" in this project's shipped tree for instance,
		// registered under a name that also shows up as some test
		// manifest's enter_command, see treestructure_test.go's
		// filterLevelSwitchCommands coverage.
		Register("test-switch-enter", func(ctx *AppContext, args []string) error { return nil })
	})
}

// TestLoadTreeValid - This test verifies that a simple, well-formed tree
// YAML file loads into a Command map with each command's RunFunc resolved
// to its registered handler.
func TestLoadTreeValid(t *testing.T) {
	registerTestHandlers()
	yaml := `
commands:
  help:
    desc: "Display available commands"
    run: test.noop
  exit:
    desc: "Exit"
    run: test.noop
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	tree, err := LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned unexpected error: %v", err)
	}
	if tree["help"] == nil || tree["help"].RunFunc == nil {
		t.Error("expected \"help\" to resolve to a handler")
	}
	if tree["exit"] == nil || tree["exit"].RunFunc == nil {
		t.Error("expected \"exit\" to resolve to a handler")
	}
}

// TestLoadTreeUnknownHandler - This test verifies that a command whose run
// value names a handler that was never registered is rejected at load
// time, rather than resolving to a nil RunFunc and failing later at first
// use.
func TestLoadTreeUnknownHandler(t *testing.T) {
	yaml := `
commands:
  bogus:
    desc: "A command whose handler was never registered"
    run: this.handler.does.not.exist
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for a tree referencing an unregistered handler, got nil")
	}
}

// TestLoadTreeEmptyFileReturnsNilTreeWithNoError - This test verifies that
// a tree file containing nothing, or only comments, is not an error. An
// empty command tree, zero commands, is unusual but valid, distinct from a
// file that fails to parse at all.
func TestLoadTreeEmptyFileReturnsNilTreeWithNoError(t *testing.T) {
	path := writeTempFile(t, "tree-*.yaml", "# nothing but a comment\n")
	tree, err := LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned unexpected error for an empty file: %v", err)
	}
	if tree != nil {
		t.Errorf("expected a nil tree for an empty file, got %v", tree)
	}
}

// TestLoadTreeUnknownHandlerNested - This test verifies that an
// unregistered handler name on a nested subcommand is rejected with an
// error naming the full "parent child" path, not just the leaf name in
// isolation, so the message points at where the typo lives in a larger
// tree file.
func TestLoadTreeUnknownHandlerNested(t *testing.T) {
	registerTestHandlers()
	yaml := `
commands:
  show:
    desc: "top"
    subcommands:
      version:
        desc: "A nested command whose handler was never registered"
        run: this.handler.does.not.exist
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for a nested tree entry referencing an unregistered handler, got nil")
	}
	if !strings.Contains(err.Error(), "show version") {
		t.Errorf("error = %q, expected it to name the full \"show version\" path", err.Error())
	}
}

// TestLoadTreeUnknownFieldIsError - This test verifies that a misspelled
// property name in a tree YAML file is rejected at load time, the same way
// config.LoadSystemConfig and auth.LoadUsers treat an unknown key in their
// own files, rather than being silently dropped, which would leave a
// command missing a directive its author thought they had set.
func TestLoadTreeUnknownFieldIsError(t *testing.T) {
	yaml := `
commands:
  bogus:
    desc: "A command with a typo'd property name"
    dsec: "this should have been desc"
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for an unknown field (misspelled dsec), got nil")
	}
}

// TestLoadTreeMultipleDocumentsIsError - This test verifies that a tree
// file containing more than one YAML document is rejected, the same way
// config.LoadSystemConfig and auth.LoadUsers treat their own files, since
// a command tree file is expected to be a single top-level mapping.
func TestLoadTreeMultipleDocumentsIsError(t *testing.T) {
	registerTestHandlers()
	yaml := "commands:\n  help:\n    run: test.noop\n---\ncommands:\n  exit:\n    run: test.noop\n"
	path := writeTempFile(t, "tree-*.yaml", yaml)
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for a tree file containing multiple YAML documents, got nil")
	}
}

// TestLoadTreeMalformedYAML - This test verifies that YAML which does not
// parse as the expected shape returns an error instead of a partial or
// zero value tree.
func TestLoadTreeMalformedYAML(t *testing.T) {
	path := writeTempFile(t, "tree-*.yaml", "commands: [this is not a map]")
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

// TestLoadTreeMissingFile - This test verifies that a path with no file on
// disk returns an error rather than an empty tree.
func TestLoadTreeMissingFile(t *testing.T) {
	_, err := LoadTree("/nonexistent/path/tree.yaml")
	if err == nil {
		t.Fatal("expected an error for a missing tree file, got nil")
	}
}

// TestLoadTreeNestedChildrenAndArgs - This test verifies that a nested
// subcommand loads correctly, and that its minargs, maxargs, and
// maxarglength settings all come through onto the resulting Command.
func TestLoadTreeNestedChildrenAndArgs(t *testing.T) {
	registerTestHandlers()
	yaml := `
commands:
  show:
    desc: "top"
    subcommands:
      version:
        desc: "leaf"
        run: test.noop
        minargs: 1
        maxargs: 2
        maxarglength: 10
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	tree, err := LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned unexpected error: %v", err)
	}
	leaf := tree["show"].Subcommands["version"]
	if leaf == nil {
		t.Fatal("expected show -> version to exist")
	}
	if leaf.MinArgs == nil || *leaf.MinArgs != 1 {
		t.Errorf("MinArgs = %v, want 1", leaf.MinArgs)
	}
	if leaf.MaxArgs == nil || *leaf.MaxArgs != 2 {
		t.Errorf("MaxArgs = %v, want 2", leaf.MaxArgs)
	}
	if leaf.MaxArgLength != 10 {
		t.Errorf("MaxArgLength = %v, want 10", leaf.MaxArgLength)
	}
}

// TestLoadTreeSetsDefIndexInFileOrder - This test verifies that
// commandMap.UnmarshalYAML stamps each command's DefIndex with its
// position among its own siblings in the source file, the one piece of
// ordering information a plain map[string]*Command decode would otherwise
// lose for good, since ListOptions.Alphabetical false relies on it.
// "zebra" is listed first in this file and "alpha" second, on purpose, so
// a test that accidentally checked alphabetical order instead of file
// order would fail.
func TestLoadTreeSetsDefIndexInFileOrder(t *testing.T) {
	registerTestHandlers()
	yaml := `
commands:
  zebra:
    run: test.noop
  alpha:
    run: test.noop
    subcommands:
      second:
        run: test.noop
      first:
        run: test.noop
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	tree, err := LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned unexpected error: %v", err)
	}

	if tree["zebra"].DefIndex != 0 {
		t.Errorf("zebra.DefIndex = %d, want 0 (listed first)", tree["zebra"].DefIndex)
	}
	if tree["alpha"].DefIndex != 1 {
		t.Errorf("alpha.DefIndex = %d, want 1 (listed second)", tree["alpha"].DefIndex)
	}

	sub := tree["alpha"].Subcommands
	if sub["second"].DefIndex != 0 {
		t.Errorf("alpha->second.DefIndex = %d, want 0 (listed first within alpha's subcommands)", sub["second"].DefIndex)
	}
	if sub["first"].DefIndex != 1 {
		t.Errorf("alpha->first.DefIndex = %d, want 1 (listed second within alpha's subcommands)", sub["first"].DefIndex)
	}
}

// TestLoadTreeUnknownFieldInsideSubcommandIsError - This test verifies
// that decodeCommandStrict's KnownFields enforcement, needed since a
// *yaml.Node's Decode has no KnownFields option of its own to carry over
// from LoadTree's top-level strict decoder, reaches a nested subcommand
// entry too, not just a top-level one, see TestLoadTreeUnknownFieldIsError
// above for the top-level case this mirrors.
func TestLoadTreeUnknownFieldInsideSubcommandIsError(t *testing.T) {
	yaml := `
commands:
  show:
    desc: "top"
    subcommands:
      version:
        dsec: "typo'd property name, nested this time"
`
	path := writeTempFile(t, "tree-*.yaml", yaml)
	_, err := LoadTree(path)
	if err == nil {
		t.Fatal("expected an error for an unknown field nested inside a subcommand, got nil")
	}
}

// writeTempFile - This function is a small shared helper for tests in this
// package that need a real file on disk.
func writeTempFile(t *testing.T, pattern, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return f.Name()
}
