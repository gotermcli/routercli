// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"strings"
	"testing"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/i18n"
)

// TestShowVersionHandlerReturnsNoError - This test verifies that "show
// version" runs without error and prints something, the minimum guarantee
// for a command with nothing else to observe.
func TestShowVersionHandlerReturnsNoError(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "show.version")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Errorf("show.version handler returned unexpected error: %v", runErr)
	}
	if out == "" {
		t.Error("expected show.version to print something")
	}
}

// ----------------------------------------------------------------------
//
// show.aliases / aliasesLines
//
// ----------------------------------------------------------------------

// TestShowAliasesHandlerNoAliasesPrintsEmptyMessage - This test verifies
// that a deployment with no runtime defined alias anywhere, the state
// every fresh CommandLevel starts in, prints the "no aliases" message
// rather than an empty, silent listing.
func TestShowAliasesHandlerNoAliasesPrintsEmptyMessage(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec"},
		},
	}
	cmd := loadTestCommand(t, "show.aliases")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.aliases handler returned unexpected error: %v", runErr)
	}
	if !strings.Contains(out, "[[show.aliases.empty]]") {
		t.Errorf("output = %q, want the empty aliases message", out)
	}
}

// TestAliasesLinesListsEveryLevelSortedByAliasName - This test verifies three
// things about aliasesLines: every level in ctx.Levels.Order carrying an
// alias contributes its own header in load order, alias names within a level
// are sorted alphabetically rather than left to Go's map iteration, and a
// level with no aliases contributes nothing, not even a header.
//
// It calls aliasesLines directly rather than going through "show aliases".
// ctx.Translator is nil in every handler test here, so T returns a bracketed
// key with no substitution, which could prove neither ordering nor content.
func TestAliasesLinesListsEveryLevelSortedByAliasName(t *testing.T) {
	ctx := newTestContext()
	// T() on a nil Translator, newTestContext's default, drops any format
	// args rather than applying them, see i18n.Translator.T's doc comment,
	// so a real Translator with a minimal catalog is needed here to see
	// the alias name and expansion text interpolated into the captured
	// output.
	ctx.Translator = i18n.New(map[string]i18n.Catalog{
		"en": {
			"show.aliases.level_header": "%s mode:",
			"show.aliases.line":         "  %s -> %s",
		},
	}, "en", "en")
	ctx.Levels = &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec", Aliases: map[string][]string{
				"wr": {"copy", "running-config", "startup-config"},
				"sh": {"show"},
			}},
			{Name: "config", Aliases: nil},
		},
	}

	lines := aliasesLines(ctx)
	joined := strings.Join(lines, "\n")

	shIdx := strings.Index(joined, "sh")
	wrIdx := strings.Index(joined, "wr")
	if shIdx == -1 || wrIdx == -1 || shIdx > wrIdx {
		t.Errorf("aliasesLines = %q, want \"sh\" listed before \"wr\" (alphabetical)", joined)
	}
	if strings.Contains(joined, "config mode:") {
		t.Errorf("aliasesLines = %q, want no header for \"config\", which has no aliases defined", joined)
	}
	if !strings.Contains(joined, "show") || !strings.Contains(joined, "copy running-config startup-config") {
		t.Errorf("aliasesLines = %q, want each alias's expansion in full", joined)
	}
}
