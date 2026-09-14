// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestHelpTextOneLevelOnly - This test verifies that HelpText lists only
// the level it is called from. A container command like "show" appears as
// one line, using its own Desc, and none of its children, "version" and
// "secret", appear at all. The line count is checked directly, by name,
// rather than by substring, since "show" is a prefix of "show version",
// and a naive substring check on that longer path could pass even if the
// recursive listing regression came back.
func TestHelpTextOneLevelOnly(t *testing.T) {
	tree := map[string]*Command{
		"show": {
			Desc: "Show things",
			Subcommands: map[string]*Command{
				"version": {Desc: "Show version", RunFunc: func(*AppContext, []string) error { return nil }},
				"secret":  {Desc: "Should never appear", Hidden: true, RunFunc: func(*AppContext, []string) error { return nil }},
			},
		},
		"exit": {Desc: "Exit the CLI", RunFunc: func(*AppContext, []string) error { return nil }},
	}

	text := HelpText(tree, nil, DefaultListOptions(), 0)
	lines := strings.Split(strings.TrimSpace(text), "\n")

	if !strings.Contains(text, "show") || !strings.Contains(text, "Show things") {
		t.Errorf("expected help text to include \"show\" with its own description, got:\n%s", text)
	}
	if strings.Contains(text, "version") {
		t.Errorf("help text recursed in to \"show\"'s children - it should list \"show\" only, got:\n%s", text)
	}
	if strings.Contains(text, "secret") {
		t.Errorf("hidden command leaked in to help text:\n%s", text)
	}
	// header + show + exit, nothing more.
	if len(lines) != 3 {
		t.Errorf("expected exactly 3 lines (header, show, exit), got %d:\n%s", len(lines), text)
	}
}

// TestHelpTextExcludesHiddenAtTopLevel - This test verifies that a Hidden
// node at the level being listed is excluded, the direct case rather than
// TestHelpTextOneLevelOnly's indirect one, where a hidden node several
// levels down never gets a chance to matter at all because HelpText does
// not recurse.
func TestHelpTextExcludesHiddenAtTopLevel(t *testing.T) {
	tree := map[string]*Command{
		"help":  {Desc: "Display available commands", RunFunc: func(*AppContext, []string) error { return nil }},
		"exit":  {Desc: "Exit the CLI", RunFunc: func(*AppContext, []string) error { return nil }},
		"\"?\"": {Desc: "Display available commands", Alias: "help", Hidden: true},
	}

	text := HelpText(tree, nil, DefaultListOptions(), 0)
	if !strings.Contains(text, "help") || !strings.Contains(text, "exit") {
		t.Errorf("expected help text to include \"help\" and \"exit\", got:\n%s", text)
	}
	if strings.Contains(text, "?") {
		t.Errorf("hidden alias leaked in to help text:\n%s", text)
	}
}

// TestHelpTextEmptyTree - This test verifies that an empty command tree
// still produces the header line, rather than an empty string.
func TestHelpTextEmptyTree(t *testing.T) {
	text := HelpText(map[string]*Command{}, nil, DefaultListOptions(), 0)
	if !strings.Contains(text, "Available commands:") {
		t.Errorf("expected a header even for an empty tree, got:\n%s", text)
	}
}

// TestHelpTextWrapsLongDescriptionWithHangingIndent - This test verifies
// that a description too long to fit next to its own command name within
// width continues on the next line, indented to line up under the
// description column, rather than left for a real terminal to hard wrap
// back at the left margin. The command name itself, "show", is never
// broken across lines, only the description that follows it.
func TestHelpTextWrapsLongDescriptionWithHangingIndent(t *testing.T) {
	tree := map[string]*Command{
		"show": {
			Desc:    "Display running system information, including the version, the configuration, and every interface",
			RunFunc: func(*AppContext, []string) error { return nil },
		},
	}

	text := HelpText(tree, nil, DefaultListOptions(), 40)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	if len(lines) < 3 {
		t.Fatalf("expected the long description to wrap onto more than one line, got %d lines:\n%s", len(lines), text)
	}
	if !strings.HasPrefix(lines[1], "  show  ") {
		t.Errorf("expected the first description line to start with the padded command name, got %q", lines[1])
	}
	wantIndent := strings.Repeat(" ", 2+len("show")+2)
	for _, line := range lines[2:] {
		if !strings.HasPrefix(line, wantIndent) {
			t.Errorf("continuation line %q does not start with the description column's indent %q", line, wantIndent)
		}
		if strings.HasPrefix(line, wantIndent+" ") {
			t.Errorf("continuation line %q is indented further than the description column itself", line)
		}
	}
	for _, line := range lines[1:] {
		if len(line) > 40 {
			t.Errorf("line %q is %d columns wide, want at most 40", line, len(line))
		}
	}
}

// TestHelpTextShortDescriptionsStayOnOneLineRegardlessOfWidth - This test
// verifies that a description that already fits stays on its own single
// line, unaffected by width, the same output this listing has always
// produced for the common case.
func TestHelpTextShortDescriptionsStayOnOneLineRegardlessOfWidth(t *testing.T) {
	tree := map[string]*Command{
		"exit": {Desc: "Exit the CLI", RunFunc: func(*AppContext, []string) error { return nil }},
	}

	text := HelpText(tree, nil, DefaultListOptions(), 80)
	if !strings.Contains(text, "  exit  Exit the CLI\n") {
		t.Errorf("expected the short description to stay on one line, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------
//
// HelpForPath, the "show ?" and "?" contextual help lookup
//
// ----------------------------------------------------------------------

func helpForPathTestTree() map[string]*Command {
	return map[string]*Command{
		"show": {
			Desc: "Show things",
			Subcommands: map[string]*Command{
				"version":        {Desc: "Show version", RunFunc: func(*AppContext, []string) error { return nil }},
				"running-config": {Desc: "Show running config", RunFunc: func(*AppContext, []string) error { return nil }},
				"startup-config": {Desc: "Show startup config", RunFunc: func(*AppContext, []string) error { return nil }},
			},
		},
		"terminal": {
			Subcommands: map[string]*Command{
				"length": {
					Desc:    "Set terminal length",
					RunFunc: func(*AppContext, []string) error { return nil },
					MinArgs: intPtr(1),
					ArgHelp: "<2-1000>  Enter a number for the 'length' command/parameter.",
				},
			},
		},
	}
}

// TestHelpForPathContainerListsSubcommandsWithDescriptions - This test
// verifies that "show ?" lists every subcommand of "show" along with its
// description, the real Cisco and HP "full help" form, rather than the
// bare names only "word help" form.
func TestHelpForPathContainerListsSubcommandsWithDescriptions(t *testing.T) {
	tree := helpForPathTestTree()
	got := HelpForPath(tree, []string{"show", ""}, nil, DefaultListOptions(), 0)

	for _, want := range []string{"version", "running-config", "startup-config", "Show version"} {
		if !strings.Contains(got, want) {
			t.Errorf("HelpForPath(show ?) = %q, expected it to contain %q", got, want)
		}
	}
}

// TestHelpForPathBarePromptListsTopLevelNames - This test verifies that
// "?" pressed at a bare prompt, or the equivalent piped "?" line, lists
// every top-level command name.
func TestHelpForPathBarePromptListsTopLevelNames(t *testing.T) {
	tree := helpForPathTestTree()
	got := HelpForPath(tree, nil, nil, DefaultListOptions(), 0)

	for _, want := range []string{"show", "terminal"} {
		if !strings.Contains(got, want) {
			t.Errorf("HelpForPath(?) = %q, expected it to contain %q", got, want)
		}
	}
}

// TestHelpForPathLeafWithArgumentShowsArgHelpHint - This test verifies
// that "terminal length ?", a leaf command that takes an argument, shows
// its ArgHelp hint. It has no subcommand listing to fall back to, and
// pressing Enter here is not valid since an argument is required, so
// neither of those forms should appear instead.
func TestHelpForPathLeafWithArgumentShowsArgHelpHint(t *testing.T) {
	tree := helpForPathTestTree()
	got := HelpForPath(tree, []string{"terminal", "length", ""}, nil, DefaultListOptions(), 0)

	if !strings.Contains(got, "<2-1000>") {
		t.Errorf("HelpForPath(terminal length ?) = %q, expected the ArgHelp hint", got)
	}
}

// TestHelpForPathUnknownPathReturnsEmpty - This test verifies that "bogus
// ?" returns an empty string, since nothing resolves at all and there is
// nothing meaningful to show. Callers treat an empty string as print
// nothing rather than guessing at an error message.
func TestHelpForPathUnknownPathReturnsEmpty(t *testing.T) {
	tree := helpForPathTestTree()
	got := HelpForPath(tree, []string{"bogus"}, nil, DefaultListOptions(), 0)
	if got != "" {
		t.Errorf("HelpForPath(bogus ?) = %q, want empty string", got)
	}
}

// TestHelpForPathLeafWithNoArgumentShowsCRHint - This test verifies that a
// leaf command taking no arguments, no ArgHelp or ArgHelpKey configured,
// shows the "<cr>" placeholder, real Cisco and HP notation for "you can
// press enter here", rather than an empty hint or the "you need an
// argument" form the other leaf test covers.
func TestHelpForPathLeafWithNoArgumentShowsCRHint(t *testing.T) {
	tree := map[string]*Command{
		"exit": {Desc: "Exit the CLI", RunFunc: func(*AppContext, []string) error { return nil }},
	}
	got := HelpForPath(tree, []string{"exit"}, nil, DefaultListOptions(), 0)
	if got != " <cr>\n" {
		t.Errorf("HelpForPath(exit ?) = %q, want %q", got, " <cr>\n")
	}
}

// TestHelpForPathNegatedAmbiguousEmptyTokenShowsFullHelp - This test
// verifies the negated companion to
// TestHelpForPathContainerListsSubcommandsWithDescriptions: "no show ?"
// must adjust AmbigAt for the stripped leading "no" before checking
// whether the ambiguous token is empty, or it would consult the wrong
// index into tokens and fall through to the bare word-help form instead of
// the full, described listing.
func TestHelpForPathNegatedAmbiguousEmptyTokenShowsFullHelp(t *testing.T) {
	tree := helpForPathTestTree()
	got := HelpForPath(tree, []string{"no", "show", ""}, nil, DefaultListOptions(), 0)

	for _, want := range []string{"version", "running-config", "startup-config", "Show version"} {
		if !strings.Contains(got, want) {
			t.Errorf("HelpForPath(no show ?) = %q, expected it to contain %q", got, want)
		}
	}
}

// TestHelpForPathNegatedAmbiguousPartialWordListsPlainNames - This test
// verifies the negated companion to
// TestHelpForPathAmbiguousListsPlainNames: "no s?" must apply the same
// AmbigAt adjustment for a partial word, landing on the bare, undescribed
// word-help form rather than panicking or reading the wrong token.
func TestHelpForPathNegatedAmbiguousPartialWordListsPlainNames(t *testing.T) {
	tree := map[string]*Command{
		"show": {Desc: "Show things", Negatable: true, RunFunc: func(*AppContext, []string) error { return nil }},
		"set":  {Desc: "Set things", Negatable: true, RunFunc: func(*AppContext, []string) error { return nil }},
	}
	got := HelpForPath(tree, []string{"no", "s"}, nil, DefaultListOptions(), 0)

	if !strings.Contains(got, "show") || !strings.Contains(got, "set") {
		t.Errorf("HelpForPath(no s?) = %q, expected both \"show\" and \"set\" listed", got)
	}
	if strings.Contains(got, "Show things") {
		t.Errorf("HelpForPath(no s?) = %q, expected bare names only (word help), not descriptions", got)
	}
}

// TestHelpForPathAmbiguousListsPlainNames - This test verifies that a
// partial word matching more than one candidate, for example "s?" when
// both "show" and "set" exist, is listed as bare names, matching Tab
// completion's ambiguous listing convention, rather than the full HelpText
// form with descriptions.
func TestHelpForPathAmbiguousListsPlainNames(t *testing.T) {
	tree := map[string]*Command{
		"show": {Desc: "Show things", RunFunc: func(*AppContext, []string) error { return nil }},
		"set":  {Desc: "Set things", RunFunc: func(*AppContext, []string) error { return nil }},
	}
	got := HelpForPath(tree, []string{"s"}, nil, DefaultListOptions(), 0)

	if !strings.Contains(got, "show") || !strings.Contains(got, "set") {
		t.Errorf("HelpForPath(s?) = %q, expected both \"show\" and \"set\" listed", got)
	}
	if strings.Contains(got, "Show things") {
		t.Errorf("HelpForPath(s?) = %q, expected bare names only (word help), not descriptions", got)
	}
}

// ----------------------------------------------------------------------
//
// "<cr>" alongside a runnable container's subcommands
//
// ----------------------------------------------------------------------

// crTestTree - This function mirrors real "totp enable" versus "totp
// enable qr": "enable" is a complete, runnable command on its own, but
// also has exactly one subcommand, "qr", below it.
func crTestTree() map[string]*Command {
	return map[string]*Command{
		"totp": {
			Desc: "TOTP settings",
			Subcommands: map[string]*Command{
				"enable": {
					Desc:    "Enable TOTP",
					RunFunc: func(*AppContext, []string) error { return nil },
					Subcommands: map[string]*Command{
						"qr": {Desc: "Show a QR code", RunFunc: func(*AppContext, []string) error { return nil }},
					},
				},
				// A second sibling, itself with no children of its own,
				// keeps "totp" from having only one subcommand, so "totp
				// ?" cannot auto-descend straight through it the way TestR
				// esolveNonRunnableContainerWithSoleSubcommandStillAutoDescendsin
				// command_test.go covers. Without this, "totp"
				// alone would never be exercised by
				// TestHelpForPathContainerRunnableAsIsAppendsCR below.
				"disable": {Desc: "Disable TOTP", RunFunc: func(*AppContext, []string) error { return nil }},
			},
		},
	}
}

// TestHelpForPathAmbiguousRunnableAsIsAppendsCR - This test verifies that
// "totp enable ?", nothing typed yet after "enable", shows both "qr" and
// "<cr>" together, matching real Cisco and HP: "enable" is already a
// complete command, but "qr" is also still available below it. "<cr>" must
// come after "qr", never sorted in among the real names.
func TestHelpForPathAmbiguousRunnableAsIsAppendsCR(t *testing.T) {
	got := HelpForPath(crTestTree(), []string{"totp", "enable", ""}, nil, DefaultListOptions(), 0)

	wantOrder := []string{"qr", "<cr>"}
	for i, want := range wantOrder {
		if !strings.Contains(got, want) {
			t.Fatalf("HelpForPath(totp enable ?) = %q, expected it to contain %q", got, want)
		}
		if i > 0 && strings.Index(got, wantOrder[i-1]) > strings.Index(got, want) {
			t.Errorf("HelpForPath(totp enable ?) = %q, expected %q before %q", got, wantOrder[i-1], want)
		}
	}
}

// TestHelpForPathContainerRunnableAsIsAppendsCR - This test verifies the
// same "<cr>" addition on the full, described HelpText form: "totp ?"
// where "totp" itself is not runnable shows no "<cr>", but a container
// that is itself directly runnable, exercised here by resolving straight
// to "enable" and asking for its own subcommand listing, does.
func TestHelpForPathContainerRunnableAsIsAppendsCR(t *testing.T) {
	tree := crTestTree()

	gotTotp := HelpForPath(tree, []string{"totp", ""}, nil, DefaultListOptions(), 0)
	if strings.Contains(gotTotp, "<cr>") {
		t.Errorf("HelpForPath(totp ?) = %q, expected no <cr>: \"totp\" itself is not runnable", gotTotp)
	}

	gotEnable := HelpForPath(tree, []string{"totp", "enable", ""}, nil, DefaultListOptions(), 0)
	if !strings.Contains(gotEnable, "<cr>") {
		t.Errorf("HelpForPath(totp enable ?) = %q, expected <cr>: \"enable\" is itself runnable", gotEnable)
	}
}

// TestHelpForPathLeafWithUnsatisfiedMinArgsAndNoArgHelpReturnsEmpty - This
// test verifies the fix to a latent bug: a leaf command whose MinArgs is
// not yet satisfied, and which defines no ArgHelp or ArgHelpKey to hint
// with, must show nothing at all, not the old behavior of falling back to
// a misleading "<cr>" that implied Enter could be pressed when it could
// not.
func TestHelpForPathLeafWithUnsatisfiedMinArgsAndNoArgHelpReturnsEmpty(t *testing.T) {
	tree := map[string]*Command{
		"secret": {RunFunc: func(*AppContext, []string) error { return nil }, MinArgs: intPtr(1)},
	}
	got := HelpForPath(tree, []string{"secret"}, nil, DefaultListOptions(), 0)
	if got != "" {
		t.Errorf("HelpForPath(secret ?) = %q, want empty string, not a misleading <cr>", got)
	}
}

// TestHelpForPathLeafWithOptionalArgumentShowsHintAndCR - This test
// verifies that a leaf command which is already runnable as is, MinArgs
// unset or zero, but which also defines an ArgHelp for an optional
// argument, shows both the hint and "<cr>" together, hint first, matching
// real Cisco's notation for "you can press enter here, or keep typing an
// optional argument".
func TestHelpForPathLeafWithOptionalArgumentShowsHintAndCR(t *testing.T) {
	tree := map[string]*Command{
		"traceroute": {
			RunFunc: func(*AppContext, []string) error { return nil },
			ArgHelp: "<host>  Optional destination to trace",
		},
	}
	got := HelpForPath(tree, []string{"traceroute"}, nil, DefaultListOptions(), 0)

	if !strings.Contains(got, "<host>") || !strings.Contains(got, "<cr>") {
		t.Errorf("HelpForPath(traceroute ?) = %q, expected both the ArgHelp hint and <cr>", got)
	}
	if strings.Index(got, "<host>") > strings.Index(got, "<cr>") {
		t.Errorf("HelpForPath(traceroute ?) = %q, expected the argument hint before <cr>", got)
	}
}

// ----------------------------------------------------------------------
//
// DetailedHelp
//
// ----------------------------------------------------------------------

// TestDetailedHelpLeafShowsNameDescAndUsage - This test verifies that
// DetailedHelp for a plain leaf command with an ArgHelp set prints a man
// page style header line, a NAME section holding its own name, a hyphen,
// and its description, and a SYNOPSIS section built from its ArgHelp, and
// nothing else, no DESCRIPTION section since no Help body is set here, and
// no SUBCOMMANDS section since it has none. It also verifies the one blank
// line before the header and the one blank line after the whole block.
func TestDetailedHelpLeafShowsNameDescAndUsage(t *testing.T) {
	tree := map[string]*Command{
		"alias": {
			Desc:    "Set a runtime defined short name for a command",
			ArgHelp: "<alias> <word...>  The short name to type from this Command Level from now on.",
		},
	}
	got := DetailedHelp(tree, []string{"alias"}, nil, DefaultListOptions(), "", 0)

	if !strings.HasPrefix(got, "\nALIAS") {
		t.Errorf("DetailedHelp(alias) = %q, expected one blank line before the mirrored ALIAS header", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("DetailedHelp(alias) = %q, expected one blank line after the whole block", got)
	}
	if !strings.Contains(got, "RouterCLI Help Information") {
		t.Errorf("DetailedHelp(alias) = %q, expected the default RouterCLI title in the header", got)
	}
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "alias - Set a runtime defined short name for a command") {
		t.Errorf("DetailedHelp(alias) = %q, expected a NAME section with the name, a hyphen, and the description", got)
	}
	if !strings.Contains(got, "SYNOPSIS") || !strings.Contains(got, "<alias> <word...>") {
		t.Errorf("DetailedHelp(alias) = %q, expected a SYNOPSIS section built from ArgHelp", got)
	}
	if strings.Contains(got, "DESCRIPTION") {
		t.Errorf("DetailedHelp(alias) = %q, expected no DESCRIPTION section: no Help body is set", got)
	}
	if strings.Contains(got, "SUBCOMMANDS") {
		t.Errorf("DetailedHelp(alias) = %q, expected no SUBCOMMANDS section for a plain leaf", got)
	}
}

// TestDetailedHelpLeafWithNoArgHelpShowsOnlyNameAndDesc - This test
// verifies that a leaf command with no ArgHelp set, "enable" for instance,
// shows the man page header and a NAME section only, never the bare,
// unhelpful "<cr>" HelpForPath itself would show for the same command, the
// exact complaint that led to DetailedHelp existing as its own function
// separate from HelpForPath. It also pins the exact output, blank line
// framing included, for a short command that fits on one wrapped line.
func TestDetailedHelpLeafWithNoArgHelpShowsOnlyNameAndDesc(t *testing.T) {
	tree := map[string]*Command{
		"enable": {Desc: "Enter privileged exec mode"},
	}
	got := DetailedHelp(tree, []string{"enable"}, nil, DefaultListOptions(), "", 0)

	want := "\n" + buildManPageHeader("enable", "", 0) + "\n\nNAME\n    enable - Enter privileged exec mode\n\n"
	if got != want {
		t.Errorf("DetailedHelp(enable) = %q, want %q", got, want)
	}
	if strings.Contains(got, "SYNOPSIS") || strings.Contains(got, "DESCRIPTION") || strings.Contains(got, "SUBCOMMANDS") {
		t.Errorf("DetailedHelp(enable) = %q, expected no other sections", got)
	}
}

// TestDetailedHelpIncludesResolvedHelpBodyWhenSet - This test verifies
// that a command with a Help body set, the long form man page style text
// ResolvedHelp resolves, has that body printed, indented, under its own
// DESCRIPTION section.
func TestDetailedHelpIncludesResolvedHelpBodyWhenSet(t *testing.T) {
	tree := map[string]*Command{
		"configure": {
			Desc: "Enter configuration mode",
			Help: "This is the long form explanation of what configuration mode actually does.",
		},
	}
	got := DetailedHelp(tree, []string{"configure"}, nil, DefaultListOptions(), "", 0)

	if !strings.Contains(got, "DESCRIPTION") {
		t.Errorf("DetailedHelp(configure) = %q, expected a DESCRIPTION section", got)
	}
	if !strings.Contains(got, manSectionIndent+"This is the long form explanation") {
		t.Errorf("DetailedHelp(configure) = %q, expected the command's Help body, indented", got)
	}
}

// TestDetailedHelpWrapsLongDescriptionAndIndentsContinuationLines - This
// test verifies that a DESCRIPTION body longer than one line wraps to the
// given width, and that every wrapped line carries the section indent.
//
// Without that, only the first line is indented and the terminal hard wraps
// the rest back to column zero, breaking the man page shape.
func TestDetailedHelpWrapsLongDescriptionAndIndentsContinuationLines(t *testing.T) {
	tree := map[string]*Command{
		"configure": {
			Desc: "Enter configuration mode",
			Help: "This sentence is deliberately long enough that it must wrap across more than one line at a narrow width.",
		},
	}
	got := DetailedHelp(tree, []string{"configure"}, nil, DefaultListOptions(), "", 40)

	descIdx := strings.Index(got, "DESCRIPTION\n")
	if descIdx == -1 {
		t.Fatalf("DetailedHelp(configure) = %q, expected a DESCRIPTION section", got)
	}
	body := got[descIdx+len("DESCRIPTION\n"):]
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("DetailedHelp(configure) DESCRIPTION body = %q, expected it to wrap across more than one line at width 40", body)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, manSectionIndent) {
			t.Errorf("DetailedHelp(configure) DESCRIPTION line %q is missing its own indent, the exact bug this test guards against", line)
		}
		if len(line) > 40 {
			t.Errorf("DetailedHelp(configure) DESCRIPTION line %q is %d columns wide, wider than the requested width 40", line, len(line))
		}
	}
}

// TestDetailedHelpWrapsSynopsisAndKeepsMultipleParagraphsSeparate - This
// test verifies two more parts of the same wrapping fix: a SYNOPSIS line
// built from a long ArgHelp wraps and indents its own continuation lines
// the same way DESCRIPTION does, and a DESCRIPTION body with more than one
// paragraph, separated by a blank line in the source text, keeps that same
// blank line between the two paragraphs in the wrapped, indented result,
// rather than running them together.
func TestDetailedHelpWrapsSynopsisAndKeepsMultipleParagraphsSeparate(t *testing.T) {
	tree := map[string]*Command{
		"alias": {
			Desc:    "Set a runtime defined short name for a command",
			ArgHelp: "<alias> <word...>  The short name to type from this Command Level from now on, and the real command it expands to.",
			Help:    "First paragraph explaining one thing at some length so it needs to wrap.\n\nSecond paragraph explaining something else, also long enough to wrap on its own.",
		},
	}
	got := DetailedHelp(tree, []string{"alias"}, nil, DefaultListOptions(), "", 40)

	synIdx := strings.Index(got, "SYNOPSIS\n")
	descIdx := strings.Index(got, "DESCRIPTION\n")
	if synIdx == -1 || descIdx == -1 || descIdx < synIdx {
		t.Fatalf("DetailedHelp(alias) = %q, expected SYNOPSIS before DESCRIPTION", got)
	}
	synopsis := got[synIdx+len("SYNOPSIS\n") : descIdx]
	synLines := strings.Split(strings.TrimRight(synopsis, "\n"), "\n")
	if len(synLines) < 2 {
		t.Fatalf("DetailedHelp(alias) SYNOPSIS = %q, expected it to wrap across more than one line at width 40", synopsis)
	}
	for _, line := range synLines {
		if !strings.HasPrefix(line, manSectionIndent) {
			t.Errorf("DetailedHelp(alias) SYNOPSIS line %q is missing its own indent", line)
		}
	}

	description := got[descIdx+len("DESCRIPTION\n"):]
	if !strings.Contains(description, "\n\n") {
		t.Errorf("DetailedHelp(alias) DESCRIPTION = %q, expected the two source paragraphs to stay separated by a blank line", description)
	}
}

// TestDetailedHelpContainerListsSubcommandsWithUsage - This test verifies
// that a container command's SUBCOMMANDS section lists each child's name,
// description, and its own ArgHelp in parentheses when it has one, one
// level deep, indented under the section header, a Hidden child excluded
// entirely, and that this listing is indented only, never rewrapped, so
// its own name column stays aligned even when a narrow width is passed in,
// unlike the prose sections above.
func TestDetailedHelpContainerListsSubcommandsWithUsage(t *testing.T) {
	tree := map[string]*Command{
		"show": {
			Desc: "Show information",
			Subcommands: map[string]*Command{
				"version": {Desc: "Show the running software version"},
				"interface": {
					Desc:    "Show one interface's state",
					ArgHelp: "<name>  The interface to show.",
				},
				"secret": {Desc: "Should never appear", Hidden: true},
			},
		},
	}
	got := DetailedHelp(tree, []string{"show"}, nil, DefaultListOptions(), "", 40)

	if !strings.Contains(got, "SUBCOMMANDS") {
		t.Errorf("DetailedHelp(show) = %q, expected a SUBCOMMANDS section", got)
	}
	if !strings.Contains(got, "version") || !strings.Contains(got, "Show the running software version") {
		t.Errorf("DetailedHelp(show) = %q, expected version's name and description", got)
	}
	if !strings.Contains(got, "interface") || !strings.Contains(got, "(<name>  The interface to show.)") {
		t.Errorf("DetailedHelp(show) = %q, expected interface's name, description, and usage hint in parentheses", got)
	}
	if strings.Contains(got, "secret") {
		t.Errorf("DetailedHelp(show) = %q, a Hidden subcommand must never appear", got)
	}
	if !strings.Contains(got, "Show the running software version") {
		t.Errorf("DetailedHelp(show) = %q, expected the SUBCOMMANDS listing to stay on one line per entry, unwrapped, even at a narrow width", got)
	}
	if !strings.Contains(got, "\n"+manSectionIndent+"version") {
		t.Errorf("DetailedHelp(show) = %q, expected the SUBCOMMANDS listing indented by exactly manSectionIndent, not doubled up with subcommandDetailLines's now removed margin", got)
	}
}

// TestDetailedHelpRunnableContainerShowsBothUsageAndSubcommands - This
// test verifies that a command that is both directly runnable and has its
// own subcommands, "totp enable" for example, gets its own SYNOPSIS
// section, from its own ArgHelp, together with the SUBCOMMANDS section
// listing what is nested below it, rather than one replacing the other.
func TestDetailedHelpRunnableContainerShowsBothUsageAndSubcommands(t *testing.T) {
	tree := map[string]*Command{
		"totp": {
			Subcommands: map[string]*Command{
				"enable": {
					Desc:    "Enroll this account in TOTP",
					ArgHelp: "[qr]  Optionally also show a scannable QR code.",
					RunFunc: func(*AppContext, []string) error { return nil },
					Subcommands: map[string]*Command{
						"qr": {Desc: "Also show a scannable QR code"},
					},
				},
			},
		},
	}
	got := DetailedHelp(tree, []string{"totp", "enable"}, nil, DefaultListOptions(), "", 0)

	if !strings.Contains(got, "SYNOPSIS") || !strings.Contains(got, "[qr]") {
		t.Errorf("DetailedHelp(totp enable) = %q, expected its own SYNOPSIS section", got)
	}
	if !strings.Contains(got, "SUBCOMMANDS") || !strings.Contains(got, "qr") {
		t.Errorf("DetailedHelp(totp enable) = %q, expected a SUBCOMMANDS section listing qr", got)
	}
}

// TestDetailedHelpContainerWithNoDescShowsBareName - This test verifies
// that a container command with no description of its own prints its bare
// name alone in the NAME section, no trailing hyphen with nothing after
// it, matching HelpText's established bare-name fallback for the same
// situation.
func TestDetailedHelpContainerWithNoDescShowsBareName(t *testing.T) {
	tree := map[string]*Command{
		"show": {
			Subcommands: map[string]*Command{
				"version": {Desc: "Show the running software version"},
			},
		},
	}
	got := DetailedHelp(tree, []string{"show"}, nil, DefaultListOptions(), "", 0)

	if !strings.Contains(got, "NAME\n"+manSectionIndent+"show\n") {
		t.Errorf("DetailedHelp(show) = %q, expected the bare name alone under NAME", got)
	}
}

// TestDetailedHelpAmbiguousListsCandidates - This test verifies that a
// partial name matching more than one command prints the matching
// candidate names, the same real candidates "?" would show for the same
// partial word, rather than a detail block or an outright refusal, and
// prints no man page header, or its framing blank lines, since there is no
// single command to build one for.
func TestDetailedHelpAmbiguousListsCandidates(t *testing.T) {
	tree := map[string]*Command{
		"show": {Desc: "Show information"},
		"set":  {Desc: "Set something"},
	}
	got := DetailedHelp(tree, []string{"s"}, nil, DefaultListOptions(), "", 0)

	if !strings.Contains(got, "show") || !strings.Contains(got, "set") {
		t.Errorf("DetailedHelp(s) = %q, expected both candidate names listed", got)
	}
	if strings.Contains(got, "Help Information") {
		t.Errorf("DetailedHelp(s) = %q, expected no man page header for an ambiguous match", got)
	}
	if strings.HasPrefix(got, "\n") {
		t.Errorf("DetailedHelp(s) = %q, expected no leading blank line for an ambiguous match", got)
	}
}

// TestDetailedHelpUnknownPathReturnsEmpty - This test verifies that a path
// resolving to nothing at all returns an empty string, the same "nothing
// to show" convention HelpForPath already uses, leaving the caller,
// example/cmd/core/cmd_help.go's "help" handler, to turn that into a real,
// translated error.
func TestDetailedHelpUnknownPathReturnsEmpty(t *testing.T) {
	tree := map[string]*Command{
		"show": {Desc: "Show information"},
	}
	got := DetailedHelp(tree, []string{"bogus"}, nil, DefaultListOptions(), "", 0)

	if got != "" {
		t.Errorf("DetailedHelp(bogus) = %q, want empty string", got)
	}
}

// ----------------------------------------------------------------------
//
// buildManPageHeader
//
// ----------------------------------------------------------------------

// TestBuildManPageHeaderMirrorsNameAndCentersTitle - This test verifies
// the header line's classic, mirrored man page shape: the command's name
// in all capitals appears at both the very start and the very end of the
// line, and the centered title in between contains the product name
// followed by "Help Information". The exact column widths are covered
// separately, below; this test only checks the shape a person reads.
func TestBuildManPageHeaderMirrorsNameAndCentersTitle(t *testing.T) {
	got := buildManPageHeader("hostname", "RouterCLI", 0)

	if !strings.HasPrefix(got, "HOSTNAME") {
		t.Errorf("buildManPageHeader(hostname) = %q, expected it to start with HOSTNAME", got)
	}
	if !strings.HasSuffix(got, "HOSTNAME") {
		t.Errorf("buildManPageHeader(hostname) = %q, expected it to end with HOSTNAME", got)
	}
	if !strings.Contains(got, "RouterCLI Help Information") {
		t.Errorf("buildManPageHeader(hostname) = %q, expected the centered RouterCLI Help Information title", got)
	}
	if strings.Count(got, "HOSTNAME") != 2 {
		t.Errorf("buildManPageHeader(hostname) = %q, expected exactly two HOSTNAME copies bracketing the title", got)
	}
}

// TestBuildManPageHeaderUsesConfiguredProductName - This test verifies
// that a deployment's configured product name, not the literal word
// "RouterCLI", drives the centered title, so a project built on top of
// this framework sees its own name there.
func TestBuildManPageHeaderUsesConfiguredProductName(t *testing.T) {
	got := buildManPageHeader("hostname", "AcmeRouter", 0)

	if !strings.Contains(got, "AcmeRouter Help Information") {
		t.Errorf("buildManPageHeader(hostname, AcmeRouter) = %q, expected the AcmeRouter Help Information title", got)
	}
	if strings.Contains(got, "RouterCLI") {
		t.Errorf("buildManPageHeader(hostname, AcmeRouter) = %q, expected no trace of the RouterCLI default", got)
	}
}

// TestBuildManPageHeaderEmptyProductNameFallsBackToRouterCLI - This test
// verifies that an empty product name, exactly what a hand built
// AppContext in a test never setting ProductName produces, falls back to
// "RouterCLI" itself, the same default config.DefaultSystemConfig ships,
// rather than leaving a blank hole in the title.
func TestBuildManPageHeaderEmptyProductNameFallsBackToRouterCLI(t *testing.T) {
	got := buildManPageHeader("hostname", "", 0)

	if !strings.Contains(got, "RouterCLI Help Information") {
		t.Errorf("buildManPageHeader(hostname, \"\") = %q, expected the RouterCLI fallback title", got)
	}
}

// TestBuildManPageHeaderIsExactlyDefaultWidthWhenWidthIsUnusable - This
// test verifies that, for an ordinary short command name, a width below
// minManPageWidth, zero included, falls back to defaultManPageWidth, the
// same fixed 80 column convention this function always used before width
// became a parameter, so a caller with no real terminal to measure still
// gets a page formatted at a sane, conventional width rather than a
// degenerate one.
func TestBuildManPageHeaderIsExactlyDefaultWidthWhenWidthIsUnusable(t *testing.T) {
	for _, width := range []int{0, -1, minManPageWidth - 1} {
		got := buildManPageHeader("hostname", "RouterCLI", width)
		if len(got) != defaultManPageWidth {
			t.Errorf("buildManPageHeader(hostname, RouterCLI, %d) has length %d, want exactly defaultManPageWidth (%d)", width, len(got), defaultManPageWidth)
		}
	}
}

// TestBuildManPageHeaderHonorsARealWidth - This test verifies that a width
// at or above minManPageWidth is honored literally rather than replaced by
// defaultManPageWidth, so a session with a wide or narrow terminal gets a
// header sized to it.
func TestBuildManPageHeaderHonorsARealWidth(t *testing.T) {
	for _, width := range []int{minManPageWidth, 60, 120} {
		got := buildManPageHeader("cmd", "RouterCLI", width)
		if len(got) != width {
			t.Errorf("buildManPageHeader(cmd, RouterCLI, %d) has length %d, want exactly %d", width, len(got), width)
		}
	}
}

// TestBuildManPageHeaderLongNameDoesNotPanicOrGoNegative - This test
// verifies that a command name long enough that two mirrored copies plus
// the title would overflow width still produces a sane,
// single-space-padded line, wider than width itself, rather than panicking
// on a negative repeat count or producing a mangled, overlapping result.
func TestBuildManPageHeaderLongNameDoesNotPanicOrGoNegative(t *testing.T) {
	longName := "show running-config interface gigabitethernet0/0/1 statistics detail"
	got := buildManPageHeader(longName, "RouterCLI", 0)

	upper := strings.ToUpper(longName)
	want := upper + " RouterCLI Help Information " + upper
	if got != want {
		t.Errorf("buildManPageHeader(longName) = %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------
//
// indentManBody
//
// ----------------------------------------------------------------------

// TestIndentManBodyIndentsEveryNonBlankLineAndPreservesBlankOnes - This
// test verifies that every non-blank line of a multi line body gets indent
// prefixed, a blank line, a paragraph break, is left blank rather than
// padded, and the result carries no doubled trailing newline.
// indentManBody itself never rewraps; TestIndentManBodyNeverRewraps below
// covers that directly.
func TestIndentManBodyIndentsEveryNonBlankLineAndPreservesBlankOnes(t *testing.T) {
	body := "First paragraph.\n\nSecond paragraph, two lines,\nstill one section.\n"
	got := indentManBody(body, "    ")

	want := "    First paragraph.\n\n    Second paragraph, two lines,\n    still one section.\n"
	if got != want {
		t.Errorf("indentManBody(...) = %q, want %q", got, want)
	}
}

// TestIndentManBodyNeverRewraps - This test verifies that indentManBody
// leaves an already long line exactly as long as it started, indent aside,
// since it backs the SUBCOMMANDS section's column aligned table, see
// subcommandDetailLines, where rewrapping would break the alignment
// between the name and description columns.
func TestIndentManBodyNeverRewraps(t *testing.T) {
	line := "version         Show the running software version  (<n>  A very long argument hint that would wrap at a narrow width)"
	got := indentManBody(line+"\n", "    ")

	want := "    " + line + "\n"
	if got != want {
		t.Errorf("indentManBody(...) = %q, want %q, unwrapped", got, want)
	}
}

// ----------------------------------------------------------------------
//
// wrapText, wrapAndIndent, and wrapAndIndentParagraphs
//
// ----------------------------------------------------------------------

// TestWrapTextBreaksOnlyAtWhitespace - This test verifies the basic word
// wrap shape: words are packed onto each line up to width, a line never
// exceeds width unless a single word alone already does, and no word is
// ever split or hyphenated.
func TestWrapTextBreaksOnlyAtWhitespace(t *testing.T) {
	got := wrapText("one two three four five six seven eight nine ten", 20)

	if len(got) < 2 {
		t.Fatalf("wrapText(...) = %v, expected more than one line at width 20", got)
	}
	for _, line := range got {
		if len(line) > 20 {
			t.Errorf("wrapText(...) produced line %q, %d columns wide, wider than width 20", line, len(line))
		}
		for _, word := range strings.Fields(line) {
			if strings.Contains("one two three four five six seven eight nine ten", word) == false {
				t.Errorf("wrapText(...) produced word %q not present in the source text; a word was corrupted", word)
			}
		}
	}
	rejoined := strings.Join(got, " ")
	if rejoined != "one two three four five six seven eight nine ten" {
		t.Errorf("wrapText(...) rejoined = %q, want the original text unchanged, no words lost or reordered", rejoined)
	}
}

// TestWrapTextSingleWordLongerThanWidthOverflowsRatherThanSplitting - This
// test verifies that a single word longer than width is placed on its own
// line rather than truncated, corrupted, or split with a hyphen this
// project's style guide reserves for real compound words only.
func TestWrapTextSingleWordLongerThanWidthOverflowsRatherThanSplitting(t *testing.T) {
	got := wrapText("short averyveryverylongwordindeed short", 10)

	found := false
	for _, line := range got {
		if line == "averyveryverylongwordindeed" {
			found = true
		}
	}
	if !found {
		t.Errorf("wrapText(...) = %v, expected the long word to survive intact on its own line", got)
	}
}

// TestWrapTextEmptyTextReturnsNil - This test verifies that empty or all
// whitespace text produces no lines at all, rather than one empty line, so
// a caller such as wrapAndIndentParagraphs never prints a stray indent
// with nothing after it.
func TestWrapTextEmptyTextReturnsNil(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\n"} {
		if got := wrapText(text, 40); got != nil {
			t.Errorf("wrapText(%q, 40) = %v, want nil", text, got)
		}
	}
}

// TestWrapAndIndentPrefixesEveryWrappedLine - This test verifies that
// wrapAndIndent both wraps text to width and prefixes every resulting
// line, continuation lines included, with indent, the fix for the original
// complaint: a continuation line landing back at column zero instead of
// under the section's left margin.
func TestWrapAndIndentPrefixesEveryWrappedLine(t *testing.T) {
	got := wrapAndIndent("one two three four five six seven eight", "    ", minManPageWidth)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	if len(lines) < 2 {
		t.Fatalf("wrapAndIndent(...) = %q, expected more than one line", got)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "    ") {
			t.Errorf("wrapAndIndent(...) line %q is missing its own indent", line)
		}
	}
}

// TestWrapAndIndentFloorsAtTwentyColumnsWhenIndentIsWide - This test
// verifies that an indent wide enough, relative to width, to leave less
// than 20 columns for the text itself still wraps at a floor of 20 rather
// than degenerating into one word per line.
func TestWrapAndIndentFloorsAtTwentyColumnsWhenIndentIsWide(t *testing.T) {
	got := wrapAndIndent("one two three four five six", strings.Repeat(" ", 30), 40)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	oneWordLines := 0
	for _, line := range lines {
		if len(strings.Fields(line)) == 1 {
			oneWordLines++
		}
	}
	if oneWordLines == len(lines) {
		t.Errorf("wrapAndIndent(...) = %q, every line held exactly one word; expected the 20 column floor to pack more than one word per line", got)
	}
}

// TestWrapAndIndentParagraphsSeparatesParagraphsWithOneBlankLine - This
// test verifies that a blank line in body, a real paragraph break,
// survives as exactly one blank line in the wrapped, indented result, and
// that each paragraph rewraps independently rather than being joined into
// one continuous run of text.
func TestWrapAndIndentParagraphsSeparatesParagraphsWithOneBlankLine(t *testing.T) {
	body := "First paragraph, several words long, enough to wrap.\n\nSecond paragraph, also several words long, enough to wrap on its own."
	got := wrapAndIndentParagraphs(body, "    ", 30)

	if !strings.Contains(got, "\n\n") {
		t.Fatalf("wrapAndIndentParagraphs(...) = %q, expected a blank line between the two paragraphs", got)
	}
	parts := strings.SplitN(got, "\n\n", 2)
	if !strings.Contains(parts[0], "First paragraph") {
		t.Errorf("wrapAndIndentParagraphs(...) first part = %q, expected it to hold the first paragraph", parts[0])
	}
	if !strings.Contains(parts[1], "Second paragraph") {
		t.Errorf("wrapAndIndentParagraphs(...) second part = %q, expected it to hold the second paragraph", parts[1])
	}
}

// TestWrapAndIndentParagraphsCollapsesEachParagraphsOwnLineBreaks - This
// test verifies that a paragraph's internal line break, however its source
// YAML file happened to be typed, is treated as just a word boundary and
// rewrapped cleanly, rather than being kept as a forced break independent
// of the requested width.
func TestWrapAndIndentParagraphsCollapsesEachParagraphsOwnLineBreaks(t *testing.T) {
	body := "One two three\nfour five six seven eight nine ten eleven twelve."
	got := wrapAndIndentParagraphs(body, "    ", minManPageWidth)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	for _, line := range lines {
		if len(line) > minManPageWidth {
			t.Errorf("wrapAndIndentParagraphs(...) line %q is %d columns wide, wider than width %d", line, len(line), minManPageWidth)
		}
	}
	rejoined := strings.Join(strings.Fields(got), " ")
	want := strings.Join(strings.Fields(body), " ")
	if rejoined != want {
		t.Errorf("wrapAndIndentParagraphs(...) words = %q, want %q, unchanged aside from rewrapping", rejoined, want)
	}
}

// ----------------------------------------------------------------------
//
// effectiveManPageWidth
//
// ----------------------------------------------------------------------

// TestEffectiveManPageWidthFallsBackBelowTheFloor - This test verifies
// that any width below minManPageWidth, including zero and a negative
// value, resolves to defaultManPageWidth, and that minManPageWidth itself,
// and anything above it, is returned unchanged.
func TestEffectiveManPageWidthFallsBackBelowTheFloor(t *testing.T) {
	for _, width := range []int{-10, -1, 0, minManPageWidth - 1} {
		if got := effectiveManPageWidth(width); got != defaultManPageWidth {
			t.Errorf("effectiveManPageWidth(%d) = %d, want defaultManPageWidth (%d)", width, got, defaultManPageWidth)
		}
	}
	for _, width := range []int{minManPageWidth, minManPageWidth + 1, 80, 200} {
		if got := effectiveManPageWidth(width); got != width {
			t.Errorf("effectiveManPageWidth(%d) = %d, want %d unchanged", width, got, width)
		}
	}
}

// ----------------------------------------------------------------------
//
// SortCommandNames
//
// ----------------------------------------------------------------------

// sortCommandNamesTestTree - This function gives SortCommandNames a small
// tree with two ordinary commands and two common commands, IsCommonCommand
// true, DefIndex values chosen so that alphabetical order and DefIndex
// order disagree, so a test can tell which one drove a given result.
func sortCommandNamesTestTree() map[string]*Command {
	return map[string]*Command{
		"zebra": {DefIndex: 0},
		"alpha": {DefIndex: 1},
		"help":  {DefIndex: 0, IsCommonCommand: true},
		"exit":  {DefIndex: 1, IsCommonCommand: true},
	}
}

// TestSortCommandNamesAlphabeticalMerged - This test verifies this
// project's default: every name sorted together, purely alphabetically,
// common commands included, matching real Cisco and HP's top-level
// listings.
func TestSortCommandNamesAlphabeticalMerged(t *testing.T) {
	tree := sortCommandNamesTestTree()
	names := []string{"zebra", "alpha", "help", "exit"}

	got := SortCommandNames(names, tree, ListOptions{Alphabetical: true, MergeCommon: true})
	want := []string{"alpha", "exit", "help", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortCommandNames(alphabetical, merged) = %v, want %v", got, want)
	}
}

// TestSortCommandNamesAlphabeticalAppended - This test verifies that
// turning MergeCommonCommands off keeps alphabetical order within each
// group, but moves every common command after every non-common one.
func TestSortCommandNamesAlphabeticalAppended(t *testing.T) {
	tree := sortCommandNamesTestTree()
	names := []string{"zebra", "alpha", "help", "exit"}

	got := SortCommandNames(names, tree, ListOptions{Alphabetical: true, MergeCommon: false})
	want := []string{"alpha", "zebra", "exit", "help"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortCommandNames(alphabetical, appended) = %v, want %v", got, want)
	}
}

// TestSortCommandNamesDefinitionOrder - This test verifies that
// AlphabeticalCommandOrder false sorts by DefIndex instead, own commands
// before common commands regardless of MergeCommon, since there is no
// single true combined definition order across two separate tree files.
func TestSortCommandNamesDefinitionOrder(t *testing.T) {
	tree := sortCommandNamesTestTree()
	names := []string{"zebra", "alpha", "help", "exit"}

	for _, mergeCommon := range []bool{true, false} {
		got := SortCommandNames(names, tree, ListOptions{Alphabetical: false, MergeCommon: mergeCommon})
		want := []string{"zebra", "alpha", "help", "exit"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SortCommandNames(definition order, MergeCommon=%v) = %v, want %v", mergeCommon, got, want)
		}
	}
}

// TestSortCommandNamesDoesNotMutateInput - This test verifies that
// SortCommandNames returns a new slice rather than sorting names in place,
// so a caller holding on to the original, such as ResolveResult.Ambiguous,
// never sees it silently reordered.
func TestSortCommandNamesDoesNotMutateInput(t *testing.T) {
	tree := sortCommandNamesTestTree()
	names := []string{"zebra", "alpha"}
	original := append([]string(nil), names...)

	SortCommandNames(names, tree, ListOptions{Alphabetical: true, MergeCommon: true})

	if !reflect.DeepEqual(names, original) {
		t.Errorf("SortCommandNames mutated its input: got %v, want unchanged %v", names, original)
	}
}

// TestWrapTextMeasuresRunesNotBytes - This test verifies that wrapping
// measures a line in terminal columns rather than in bytes. RouterCLI is
// translated, see package i18n, so a description in a language with
// non-ASCII characters has more bytes than it has columns. Measuring with
// len() overstates how much of the line has been used and breaks to a new
// line while there is still room. The text below is 30 runes wide and fits
// a 32 column line, but is 39 bytes, so byte based measurement wraps it
// into two lines.
func TestWrapTextMeasuresRunesNotBytes(t *testing.T) {
	const width = 32
	text := "ééééééééée mot mot mot mot mot"

	if got := utf8.RuneCountInString(text); got != width-2 {
		t.Fatalf("test fixture drifted: text is %d runes, want %d", got, width-2)
	}
	if len(text) <= width {
		t.Fatalf("test fixture drifted: text is %d bytes, needs to exceed %d for this test to mean anything", len(text), width)
	}

	lines := wrapText(text, width)
	if len(lines) != 1 {
		t.Errorf("wrapText wrapped a %d column line at width %d into %d lines, want 1: %q",
			utf8.RuneCountInString(text), width, len(lines), lines)
	}
}

// TestHelpForPathHidesArgHintOnceMaxArgsIsSatisfied - This test verifies
// that "?" stops offering the argument hint once every argument slot is
// filled.
//
// "hostname ?" should offer <name>, since none has been supplied.
// "hostname test ?" should offer only <cr>, because MaxArgs is 1 and
// offering the hint would invite a session to type a second argument the
// command would then refuse.
func TestHelpForPathHidesArgHintOnceMaxArgsIsSatisfied(t *testing.T) {
	one := 1
	tree := map[string]*Command{
		"hostname": {
			Desc:    "set the hostname",
			ArgHelp: "<name>  The new hostname",
			MinArgs: &one,
			MaxArgs: &one,
			RunFunc: func(*AppContext, []string) error { return nil },
		},
	}
	opts := DefaultListOptions()

	noArg := HelpForPath(tree, []string{"hostname", ""}, nil, opts, 80)
	if !strings.Contains(noArg, "<name>") {
		t.Errorf("with no argument supplied, expected the hint, got %q", noArg)
	}

	withArg := HelpForPath(tree, []string{"hostname", "test", ""}, nil, opts, 80)
	if strings.Contains(withArg, "<name>") {
		t.Errorf("with MaxArgs already satisfied, the hint should be gone, got %q", withArg)
	}
	if !strings.Contains(withArg, "<cr>") {
		t.Errorf("with MaxArgs satisfied the command is runnable, expected <cr>, got %q", withArg)
	}
}

// TestHelpForPathKeepsArgHintForAnOptionalSecondArgument - This test
// verifies the middle case: a command already runnable that could still
// take another argument shows both the hint and <cr>.
func TestHelpForPathKeepsArgHintForAnOptionalSecondArgument(t *testing.T) {
	zero, two := 0, 2
	tree := map[string]*Command{
		"reboot": {
			Desc:    "reboot",
			ArgHelp: "<seconds>  Optional delay",
			MinArgs: &zero,
			MaxArgs: &two,
			RunFunc: func(*AppContext, []string) error { return nil },
		},
	}
	opts := DefaultListOptions()

	got := HelpForPath(tree, []string{"reboot", "60", ""}, nil, opts, 80)
	if !strings.Contains(got, "<seconds>") {
		t.Errorf("a second argument is still accepted, expected the hint, got %q", got)
	}
	if !strings.Contains(got, "<cr>") {
		t.Errorf("already runnable, expected <cr> too, got %q", got)
	}
}

// TestAcceptsMoreArgsTreatsNilMaxArgsAsUnbounded - This test verifies that
// a command with no MaxArgs, such as one taking the rest of the line, keeps
// offering its hint however many arguments have been typed.
func TestAcceptsMoreArgsTreatsNilMaxArgsAsUnbounded(t *testing.T) {
	cmd := &Command{ArgHelp: "<word...>"}
	if !AcceptsMoreArgs(cmd, []string{"a", "b", "c"}) {
		t.Error("nil MaxArgs means unbounded, expected true")
	}
}

// TestAcceptsMoreArgsIgnoresTheTrailingPlaceholder - This test verifies
// that the synthetic empty token meaning "nothing typed yet" is not counted
// as a supplied argument.
func TestAcceptsMoreArgsIgnoresTheTrailingPlaceholder(t *testing.T) {
	one := 1
	cmd := &Command{MaxArgs: &one}
	if !AcceptsMoreArgs(cmd, []string{""}) {
		t.Error("a lone placeholder is not an argument, expected true")
	}
	if AcceptsMoreArgs(cmd, []string{"real", ""}) {
		t.Error("one real argument fills MaxArgs 1, expected false")
	}
}
