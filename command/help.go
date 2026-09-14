// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gotermcli/routercli/i18n"
)

// ----------------------------------------------------------------------
// Public Functions - Help
// ----------------------------------------------------------------------

// SortCommandNames - Three paths print a list of command names: HelpText,
// the Tab completion candidate list, and the "?" word help list. If each
// sorted its own way, the same commands would appear in different orders
// depending on how the user asked.
//
// All three funnel through here, so one pair of settings controls the
// order everywhere. names is never mutated; a new slice is returned.
//
// With opts.Alphabetical true, the default, this sorts by name. With it
// false, it sorts by DefIndex, the order the tree file declared.
//
// Common commands sort after level specific ones whenever
// opts.MergeCommon is false, and always when sorting by DefIndex.
// DefIndex is only comparable within one file, so mixing the two groups
// would compare numbers that mean nothing to each other.
func SortCommandNames(names []string, tree map[string]*Command, opts ListOptions) []string {
	sorted := append([]string(nil), names...)

	sort.SliceStable(sorted, func(i, j int) bool {
		ni, nj := sorted[i], sorted[j]
		ci, cj := tree[ni], tree[nj]

		commonI := ci != nil && ci.IsCommonCommand
		commonJ := cj != nil && cj.IsCommonCommand

		// Definition order always separates the two groups, and so does
		// alphabetical order when common commands are being appended
		// rather than merged, see this function's doc comment for why.
		// Alphabetical-and-merged, this project's default, is the one
		// combination that never partitions here.
		if (!opts.Alphabetical || !opts.MergeCommon) && commonI != commonJ {
			return !commonI
		}

		if !opts.Alphabetical {
			di, dj := 0, 0
			if ci != nil {
				di = ci.DefIndex
			}
			if cj != nil {
				dj = cj.DefIndex
			}
			if di != dj {
				return di < dj
			}
		}

		return ni < nj
	})

	return sorted
}

// HelpText - "help" has to show what is reachable right here, not the
// whole tree. Listing everything recursively would bury the answer.
//
// This builds a listing of the commands at one level, that level only.
// Hidden commands are skipped, the same as in tab completion.
//
// One level deep matches Cisco and HP. "help" at the top shows "show" as a
// single line, not every subcommand. To see inside a container, descend
// into it and run "help" again.
//
// It lives here rather than in the help command because it is a property
// of the tree, so a different command set gets it for free.
//
// t may be nil, in which case descriptions fall back to each Command's
// literal Desc. width is the terminal width; a zero or unusable value
// falls back to 80 columns. A description too long to fit continues on the
// next line, indented under the description column.
func HelpText(tree map[string]*Command, t *i18n.Translator, opts ListOptions, width int) string {
	var names []string
	for name, cmd := range tree {
		if cmd.Hidden {
			continue
		}
		names = append(names, name)
	}
	names = SortCommandNames(names, tree, opts)

	entries := make([]helpEntry, len(names))
	for i, name := range names {
		entries[i] = helpEntry{name, tree[name].ResolvedDesc(t)}
	}

	longest := 0
	for _, e := range entries {
		if displayWidth(e.path) > longest {
			longest = displayWidth(e.path)
		}
	}

	// "  " before the name, the name itself padded to longest, then
	// "  " again before the description, matching the Fprintf format
	// string below exactly, is how wide a continuation line's
	// leading indent needs to be to land right under where the
	// description column itself starts.
	indent := strings.Repeat(" ", 2+longest+2)
	descWidth := bodyWidth(width, indent)

	var b strings.Builder
	header := "Available commands:"
	if t != nil {
		header = t.T("help.header")
	}
	fmt.Fprintln(&b, header)
	for _, e := range entries {
		if e.desc == "" {
			fmt.Fprintf(&b, "  %s\n", e.path)
			continue
		}
		descLines := wrapText(e.desc, descWidth)
		fmt.Fprintf(&b, "  %-*s  %s\n", longest, e.path, descLines[0])
		for _, line := range descLines[1:] {
			fmt.Fprintf(&b, "%s%s\n", indent, line)
		}
	}
	return b.String()
}

// HelpForPath - Pressing "?" is a keypress the completer intercepts, but
// piped or scripted input generates no keypresses. That path still needs
// the same answer.
//
// This builds the "?" listing as a pure function over an already split
// token path, with no readline instance involved. tokens excludes the
// trailing "?", which the caller strips.
//
// There are three cases, matching Cisco and HP:
//
//   - Ambiguous tokens list the candidate names alone, no descriptions.
//   - A container returns the full HelpText of its subcommands.
//   - A leaf returns its ArgHelp hint, or "<cr>" if it takes no argument.
//
// A "<cr>" line is appended to either of the first two when the container
// is already runnable as typed. "totp enable ?" shows both "qr" and
// "<cr>". It is always last and never sorted.
//
// An empty string means the tokens resolved to nothing.
func HelpForPath(tree map[string]*Command, tokens []string, t *i18n.Translator, opts ListOptions, width int) string {
	if len(tokens) == 0 {
		tokens = []string{""}
	}
	res := Resolve(tree, tokens)

	switch {
	case len(res.Ambiguous) > 0:
		// Resolve() reports "show " (a trailing empty token right after a
		// real container) as ambiguous against every one of that
		// container's children, the same as a genuine half typed word such
		// as "sh". Real Cisco and HP treat these two situations
		// differently for "?", see this function's doc comment. The
		// distinguishing signal is whether the ambiguous token itself is
		// empty, the same check completer.go's ambiguousTokenIsEmpty makes
		// for tab completion.
		ambigIdx := res.AmbigAt
		if res.Negated {
			ambigIdx++
		}
		if ambigIdx >= 0 && ambigIdx < len(tokens) && tokens[ambigIdx] == "" && res.AmbiguousTree != nil {
			// Full help, nothing typed yet for this position: the full,
			// described HelpText of res.AmbiguousTree, the very container
			// res.Ambiguous's candidates were drawn from, see
			// ResolveResult.AmbiguousTree's doc comment, instead of the
			// bare names res.Ambiguous itself carries.
			text := HelpText(res.AmbiguousTree, t, opts, width)
			if res.RunnableAsIs {
				text += "  <cr>\n"
			}
			return text
		}
		// Word help, a partial word: bare candidate names with no
		// descriptions, matching real Cisco's word help form and this
		// project's existing tab completion convention for the same
		// situation, see completer.go's Ambiguous branch.
		names := SortCommandNames(res.Ambiguous, res.AmbiguousTree, opts)
		var b strings.Builder
		for _, c := range names {
			fmt.Fprintf(&b, " %s\n", c)
		}
		if res.RunnableAsIs {
			fmt.Fprintf(&b, " <cr>\n")
		}
		return b.String()
	case res.Command != nil && len(res.Command.Subcommands) > 0:
		text := HelpText(res.Command.Subcommands, t, opts, width)
		if res.RunnableAsIs {
			text += "  <cr>\n"
		}
		return text
	case res.Command != nil:
		hint := res.Command.ResolvedArgHelp(t)
		if !AcceptsMoreArgs(res.Command, res.Args) {
			// Every argument slot is already filled, so offering the
			// hint would invite a session to type something the command
			// would then refuse.
			hint = ""
		}
		switch {
		case hint != "" && res.RunnableAsIs:
			// Optional argument: the command is already complete as typed,
			// but more could still follow, so both the hint and "<cr>" are
			// shown together, hint first, "<cr>" last, matching real
			// Cisco.
			return " " + hint + "\n <cr>\n"
		case hint != "":
			return " " + hint + "\n"
		case res.RunnableAsIs:
			return " <cr>\n"
		default:
			// MinArgs is not satisfied, and this command defines no
			// ArgHelp or ArgHelpKey of its own to hint with. Showing
			// "<cr>" here, the old behavior, would be misleading, since
			// pressing Enter right now is not a complete command, so this
			// shows nothing instead.
			return ""
		}
	default:
		return ""
	}
}

// defaultManPageWidth is the column width DetailedHelp's header line and
// wrapped body text fall back to whenever the width a caller passes in is
// not a real, usable terminal width, the same 80 column convention classic
// Unix man pages themselves default to, both nroff and groff, when neither
// MANWIDTH nor a real terminal is available to size against.
// minManPageWidth is the floor below which a passed in width is treated as
// unusable rather than honored literally, so an oddly small or zero value,
// a piped "help <command>" with no real terminal behind it for instance,
// still produces a readable page instead of wrapping every word onto its
// own line.
const (
	defaultManPageWidth = 80
	minManPageWidth     = 40

	// minBodyWidth is the narrowest a wrapped body column is ever allowed
	// to get once an indent has been subtracted from the usable width. An
	// indent unusually wide relative to width floors here rather than
	// wrapping one word per line.
	minBodyWidth = 20
)

// effectiveManPageWidth - This function returns width itself when it is at
// least minManPageWidth, and defaultManPageWidth otherwise. Both
// buildManPageHeader and the wrapping helpers below call this once on
// whatever width DetailedHelp itself was given, so a caller with no real
// terminal to measure, or one that passes zero, never needs its own
// fallback logic.
func effectiveManPageWidth(width int) int {
	if width < minManPageWidth {
		return defaultManPageWidth
	}
	return width
}

// bodyWidth - This function returns how many columns are left for wrapped
// body text once indent has been subtracted from width, never returning
// less than minBodyWidth. Both the column aligned listing in HelpText and
// the man page sections in wrapAndIndent need exactly this calculation, so
// it lives here once rather than in each.
func bodyWidth(width int, indent string) int {
	inner := effectiveManPageWidth(width) - displayWidth(indent)
	if inner < minBodyWidth {
		return minBodyWidth
	}
	return inner
}

// displayWidth - This function reports how many terminal columns s
// occupies, which is its count of runes, not its count of bytes. Every
// width calculation in this file goes through it.
//
// RouterCLI is translated, so a description in a language with
// non-ASCII characters has more bytes than it has columns. Measuring
// with len() overstates how much of the line a word has used, and
// wrapText then breaks to a new line while there is still room. That
// wraps help text short in exactly the languages that need the space
// most.
func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}

// manSectionIndent is how far a section's body text is indented under its
// all caps header. Four columns, narrower than a real man page's seven
// column tab stop, matches this project's preference, see the project
// instructions this whole codebase follows, for compact, easy to read
// output over faithfully reproducing groff's exact metrics.
const manSectionIndent = "    "

// buildManPageHeader - This function builds DetailedHelp's first line: the
// command name in capitals on both margins with a centered title between
// them, the way a Unix man page header reads. There is no section number,
// since RouterCLI has no man page sections.
//
// name is the full resolved command path, already space joined.
// productName is the deployment's display name and falls back to
// "RouterCLI", which is what a test that does not care about branding
// leaves it as. width has already been through effectiveManPageWidth, so
// there is no fallback to do here.
//
// A title too wide for the margins is truncated rather than wrapped, since
// a header that wraps stops looking like a header.
func buildManPageHeader(name, productName string, width int) string {
	if productName == "" {
		productName = "RouterCLI"
	}
	width = effectiveManPageWidth(width)
	upper := strings.ToUpper(name)
	title := productName + " Help Information"

	pad := width - displayWidth(upper)*2 - displayWidth(title)
	if pad < 2 {
		return upper + " " + title + " " + upper
	}
	leftPad := pad / 2
	rightPad := pad - leftPad
	return upper + strings.Repeat(" ", leftPad) + title + strings.Repeat(" ", rightPad) + upper
}

// indentManBody - This function prefixes every line of body with indent
// and does no rewrapping, so the SUBCOMMANDS listing keeps the column
// alignment a rewrap would break.
//
// A blank line stays blank rather than being padded with trailing
// whitespace. A trailing newline on body is trimmed first so this never
// doubles it.
//
// NAME, SYNOPSIS, and DESCRIPTION are prose rather than a table, so those
// use the wrapping helpers instead.
func indentManBody(body, indent string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			fmt.Fprintln(&b)
			continue
		}
		fmt.Fprintf(&b, "%s%s\n", indent, line)
	}
	return b.String()
}

// wrapText - This function breaks text into lines no wider than width,
// breaking only at whitespace, never splitting a word.
//
// A single word longer than width still gets its own line rather than
// being truncated, so this never loses content, only occasionally produces
// one line wider than asked for.
//
// Splitting through strings.Fields also collapses any run of whitespace,
// an embedded newline included, so a caller does not need to normalize
// text first.
func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := lines[len(lines)-1]
		if displayWidth(last)+1+displayWidth(word) > width {
			lines = append(lines, word)
			continue
		}
		lines[len(lines)-1] = last + " " + word
	}
	return lines
}

// wrapAndIndent - This function wraps text, a single prose line such as
// DetailedHelp's NAME or SYNOPSIS line, to width, run through
// effectiveManPageWidth first the same way buildManPageHeader's width is,
// minus indent's length, then prefixes every wrapped line with indent, so
// a continuation line lands under the section body's left margin instead
// of back at column zero the way an unwrapped, merely indented first line
// left it for a session's terminal to wrap on its own. A remaining width
// narrower than 20 columns, indent itself unusually wide relative to
// width, floors to minBodyWidth rather than wrapping one word per line.
func wrapAndIndent(text, indent string, width int) string {
	inner := bodyWidth(width, indent)

	var b strings.Builder
	for _, line := range wrapText(text, inner) {
		fmt.Fprintf(&b, "%s%s\n", indent, line)
	}
	return b.String()
}

// wrapAndIndentParagraphs - This function wraps and indents body, a
// longer, possibly multi paragraph Help body, the same way wrapAndIndent
// does a single line, except that a blank line inside body is honored as a
// genuine paragraph break: each paragraph is rewrapped independently,
// through wrapAndIndent, and paragraphs are separated by one blank line of
// their own in the result. A paragraph's internal line breaks, however
// body happened to be typed in its source YAML file, are collapsed first,
// through wrapText's strings.Fields call, so every paragraph rewraps
// cleanly at this deployment's width rather than keeping whatever
// arbitrary breaks the source file's line length left it with.
func wrapAndIndentParagraphs(body, indent string, width int) string {
	paragraphs := strings.Split(strings.TrimRight(body, "\n"), "\n\n")

	var b strings.Builder
	for i, p := range paragraphs {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		fmt.Fprint(&b, wrapAndIndent(p, indent, width))
	}
	return b.String()
}

// DetailedHelp - This function builds a man page style description of one
// command. It is what "help <command>" calls, and answers "what does this
// command do" rather than the "what can I type next" that "?" answers.
//
// tokens is the command path already split. productName is used in the
// header line and falls back to "RouterCLI" when empty. width is the
// terminal width; a zero value still produces a readable page.
//
// The block is framed by blank lines so it reads as a separated unit. It
// holds up to four sections:
//
//   - NAME always appears, with the full name and description.
//   - SYNOPSIS appears when the command takes an argument.
//   - DESCRIPTION appears when a longer body is set.
//   - SUBCOMMANDS appears when the command has children.
//
// A command that is both runnable and has children, "totp enable" for
// example, gets SYNOPSIS and SUBCOMMANDS together.
//
// Ambiguous tokens print the matching candidates instead, so a session
// sees what to narrow down to. Tokens resolving to nothing return an
// empty string, and the caller turns that into an error.
func DetailedHelp(tree map[string]*Command, tokens []string, t *i18n.Translator, opts ListOptions, productName string, width int) string {
	res := Resolve(tree, tokens)

	if len(res.Ambiguous) > 0 {
		names := SortCommandNames(res.Ambiguous, res.AmbiguousTree, opts)
		var b strings.Builder
		header := "Ambiguous; did you mean one of the following:"
		if t != nil {
			header = t.T("help.ambiguous")
		}
		fmt.Fprintln(&b, header)
		for _, name := range names {
			fmt.Fprintf(&b, "  %s\n", name)
		}
		return b.String()
	}

	if res.Command == nil {
		return ""
	}

	name := strings.Join(res.FullName, " ")
	var b strings.Builder

	nameHeader, synopsisHeader, descHeader, subcommandsHeader :=
		"NAME", "SYNOPSIS", "DESCRIPTION", "SUBCOMMANDS"
	if t != nil {
		nameHeader = t.T("help.section.name")
		synopsisHeader = t.T("help.section.synopsis")
		descHeader = t.T("help.section.description")
		subcommandsHeader = t.T("help.section.subcommands")
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, buildManPageHeader(name, productName, width))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, nameHeader)
	nameLine := name
	if desc := res.Command.ResolvedDesc(t); desc != "" {
		nameLine = name + " - " + desc
	}
	fmt.Fprint(&b, wrapAndIndent(nameLine, manSectionIndent, width))

	if hint := res.Command.ResolvedArgHelp(t); hint != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, synopsisHeader)
		fmt.Fprint(&b, wrapAndIndent(name+" "+hint, manSectionIndent, width))
	}

	if help := res.Command.ResolvedHelp(t); help != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, descHeader)
		fmt.Fprint(&b, wrapAndIndentParagraphs(help, manSectionIndent, width))
	}

	if len(res.Command.Subcommands) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, subcommandsHeader)
		fmt.Fprint(&b, indentManBody(subcommandDetailLines(res.Command.Subcommands, t, opts), manSectionIndent))
	}

	fmt.Fprintln(&b)

	return b.String()
}

// subcommandDetailLines - This function renders the one level listing
// DetailedHelp's SUBCOMMANDS section uses: hidden commands skipped,
// aligned on name, with each ResolvedArgHelp in parentheses where there is
// one. HelpText's plain listing never shows those.
//
// Lines carry no left margin. DetailedHelp applies the section indent
// afterward, the same one the other sections get, so a margin built in
// here would double it.
func subcommandDetailLines(subcommands map[string]*Command, t *i18n.Translator, opts ListOptions) string {
	var names []string
	for name, cmd := range subcommands {
		if cmd.Hidden {
			continue
		}
		names = append(names, name)
	}
	names = SortCommandNames(names, subcommands, opts)

	longest := 0
	for _, name := range names {
		if displayWidth(name) > longest {
			longest = displayWidth(name)
		}
	}

	var b strings.Builder
	for _, name := range names {
		cmd := subcommands[name]
		desc := cmd.ResolvedDesc(t)
		hint := cmd.ResolvedArgHelp(t)
		switch {
		case desc == "" && hint == "":
			fmt.Fprintf(&b, "%s\n", name)
		case hint == "":
			fmt.Fprintf(&b, "%-*s  %s\n", longest, name, desc)
		case desc == "":
			fmt.Fprintf(&b, "%-*s  (%s)\n", longest, name, hint)
		default:
			fmt.Fprintf(&b, "%-*s  %s  (%s)\n", longest, name, desc, hint)
		}
	}
	return b.String()
}
