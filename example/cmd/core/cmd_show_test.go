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

// TestAliasesLinesListsEveryLevelSortedByAliasName - This test verifies
// aliasesLines directly, rather than through "show aliases" itself, since
// ctx.Translator is nil in every handler test in this package, see
// newTestContext's doc comment, which would leave i18n.Translator.T
// returning only a bracketed key with no argument substitution, unable to
// prove ordering or content either way. It verifies that every level in
// ctx.Levels.Order carrying at least one alias contributes its own header,
// in Order's load order, that alias names within one level are sorted
// alphabetically, not in whatever order Go's unordered map iteration
// happens to produce, and that a level with no aliases defined contributes
// nothing at all, not even its own header.
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
