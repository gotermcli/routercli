// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/i18n"
)

// newTestContext - This function builds a minimal *command.AppContext
// suitable for exercising a single command handler directly, without
// needing readline, a real login session, or any of main.go's other
// startup machinery. State is left nil, since nothing registered by this
// package touches ctx.State; every setting this package's handlers read or
// write, PageLines, TerminalWidth, HistorySize, FilterMode, HistoryFile,
// lives directly on AppContext itself, local to one connection rather than
// a project's shared application state. See example/cmd/session/doc.go for the
// full reasoning behind that boundary.
//
// This is the same shape as cmd/core's newTestContext, one independent
// copy per package rather than a shared export, the same reasoning that
// helper's doc comment gives: a test-only helper cannot be exported across
// two independent, non-test-importing packages without pulling a test file
// into a non-test build.
func newTestContext() *command.AppContext {
	return &command.AppContext{
		Logger: log.New(io.Discard, "", 0),
	}
}

// loadTestCommand - This function resolves handlerName into its
// *command.Command by writing a throwaway, one command tree file and
// loading it through command.LoadTree, the same loader main.go uses at
// startup. This is not a direct call into the handler closure itself,
// since command.Register stores it only in an unexported registry inside
// package command, reachable from outside that package through this same
// "run:" resolution path a real tree file uses. handlerName must already
// be registered, which every cmd_*.go file in this package's init() does
// automatically once this package is imported, exactly as it is here.
func loadTestCommand(t *testing.T, handlerName string) *command.Command {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tree.yaml")
	body := "commands:\n  test:\n    run: " + handlerName + "\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("failed to write test tree file: %v", err)
	}
	tree, err := command.LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned error: %v", err)
	}
	return tree["test"]
}

// testTranslator - This function builds a minimal real *i18n.Translator
// carrying only the "show.terminal.*" keys terminalStatusLines reads.
//
// A test that needs to see real interpolated text uses this. A nil
// Translator produces the bracketed "[[key]]" placeholder instead, which
// is fine for a test that only checks structure.
func testTranslator(t *testing.T) *i18n.Translator {
	t.Helper()
	return i18n.New(map[string]i18n.Catalog{
		"en": {
			"show.terminal.enabled":               "enabled",
			"show.terminal.disabled":              "disabled",
			"show.terminal.geometry_line":         "Length: %d, Width: %d",
			"show.terminal.paging_line":           "Paging: session-%s, global-%s",
			"show.terminal.filter_mode_line":      "Filter Mode: %s",
			"show.terminal.filter_mode_substring": "substring",
			"show.terminal.filter_mode_regex":     "regex",
		},
	}, "en", "en")
}
