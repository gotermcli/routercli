// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package completer

import (
	"fmt"
	"strings"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/paging"

	"github.com/chzyer/readline"
)

// ----------------------------------------------------------------------
// Public Methods - NoopCompleter
// ----------------------------------------------------------------------

func (NoopCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	return nil, 0
}

// ----------------------------------------------------------------------
// Public Methods - TreeListener
// ----------------------------------------------------------------------

// SetPrompt - The help listing, the candidate list, and the argument hint
// all print above the current line. Without the prompt text they read as
// detached from it.
//
// This records the prompt so those lines can be prefixed with it.
// TreeListener keeps its own copy because readline exposes no getter for
// the prompt it was last given.
//
// A caller MUST call this every time it calls rl.SetPrompt, since a
// command may have changed the mode or elevation and left the two out of
// sync.
func (l *TreeListener) SetPrompt(prompt string) {
	l.currentPrompt = prompt
}

// OnChange - This method implements readline.Listener, called on every
// keypress with the buffer as readline has already handled it. See the
// type's doc comment for why this is used instead of AutoComplete.
//
// A "?" delegates to handleHelp. Any other non-Tab key resets the double
// Tab state and returns false.
//
// A Tab resolves what has been typed against the current Command Level.
// If it resolves to one command, the buffer is rewritten to the full name,
// with a trailing space and an argument hint where those apply. If it is
// ambiguous, repeated Tab presses on the same input are counted so the
// candidate list prints on the second, matching Cisco and HP.
func (l *TreeListener) OnChange(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool) {
	if key == '?' {
		return l.handleHelp(line, pos)
	}
	if key != readline.CharTab {
		// Any non-Tab key breaks a double Tab sequence in progress.
		l.tapCount = 0
		l.lastAmbiguousInput = ""
		return nil, 0, false
	}

	typed := string(line[:pos])
	trailingSpace := strings.HasSuffix(typed, " ")
	tokens := strings.Fields(typed)
	if len(tokens) == 0 {
		// An empty line, Tab at a bare prompt, lists every top-level
		// command. This is always shown immediately, since there is
		// nothing ambiguous about it needing a confirmation tap.
		tokens = []string{""}
	} else if trailingSpace {
		// strings.Fields silently drops trailing whitespace, but a
		// trailing space is meaningful here: it means list what can come
		// next.
		tokens = append(tokens, "")
	}

	tree := l.position.Current().Tree
	tokens, linePrefix := unwrapHelpTokens(tree, tokens)
	res := command.Resolve(tree, tokens)

	// "no " stays on the rewritten line exactly as typed. Resolve strips it
	// internally to find the real command, but the user should still see
	// what they typed, with the rest completing after it.
	noPrefix := ""
	if res.Negated {
		noPrefix = "no "
	}

	if len(res.Ambiguous) > 0 {
		return l.completeAmbiguous(typed, tokens, res, linePrefix, noPrefix)
	}

	return l.completeResolved(line, pos, typed, trailingSpace, res, linePrefix, noPrefix)
}

// ----------------------------------------------------------------------
// Private Methods - TreeListener
// ----------------------------------------------------------------------

// completeAmbiguous - This method handles the Tab case where the line so
// far matches more than one command. It rewrites the buffer to the longest
// unambiguous prefix the candidates share, and, on the second consecutive
// Tab against the same input, prints the candidate list.
//
// The two tap requirement matches real Cisco and HP: a half typed word
// lists only once the user asks twice. Nothing typed yet for this token, a
// bare prompt or right after a trailing space, lists immediately, since
// there is nothing ambiguous needing a confirmation tap.
func (l *TreeListener) completeAmbiguous(typed string, tokens []string, res command.ResolveResult, linePrefix, noPrefix string) (newLine []rune, newPos int, ok bool) {
	newBuf := linePrefix + ambiguousRewriteBuffer(tokens, res, noPrefix)

	if typed == l.lastAmbiguousInput {
		l.tapCount++
	} else {
		l.tapCount = 1
		l.lastAmbiguousInput = typed
	}
	l.logger.Debugln("DEBUG: ambiguous completion for", tokens, "tap", l.tapCount)

	if ambiguousTokenIsEmpty(tokens, res) || l.tapCount >= 2 {
		names := command.SortCommandNames(res.Ambiguous, res.AmbiguousTree, l.listOptions)
		var list strings.Builder
		list.WriteString(l.currentPrompt)
		list.WriteString(typed)
		list.WriteString("\n")
		for _, candidate := range names {
			list.WriteString(" ")
			list.WriteString(candidate)
			list.WriteString("\n")
		}
		if res.RunnableAsIs {
			// "<cr>" goes last and is never sorted among the real names,
			// matching Cisco and HP. A command can be both ambiguous and
			// runnable at once: "totp enable " has "qr" below it and is
			// also complete on its own.
			list.WriteString(" <cr>\n")
		}
		fmt.Fprint(l.instance.Stdout(), list.String())
	}

	if newBuf == typed {
		// Nothing to expand, so do not fight the cursor on repeat taps.
		return nil, 0, false
	}
	return []rune(newBuf), len([]rune(newBuf)), true
}

// completeResolved - This method handles the Tab case where the line
// resolves to exactly one command.
//
// It expands the line to the full name, adds a trailing space once
// nothing is left half typed so the next Tab shows what comes next, and
// prints the argument hint, "<cr>", or both once the line reaches a leaf.
func (l *TreeListener) completeResolved(line []rune, pos int, typed string, trailingSpace bool, res command.ResolveResult, linePrefix, noPrefix string) (newLine []rune, newPos int, ok bool) {
	// Not ambiguous, so any double Tab sequence in progress is no longer
	// relevant.
	l.tapCount = 0
	l.lastAmbiguousInput = ""

	resolvedLine := linePrefix + noPrefix + strings.Join(res.FullName, " ")
	if len(res.Args) > 0 {
		// linePrefix already ends in a space when it carries anything,
		// "help " for one level of unwrapping, so only add a separator
		// when there is not one there already. Adding it unconditionally
		// doubles the space for a line whose tokens all resolved into the
		// prefix, leaving FullName empty: "help foo" would come back as
		// "help  foo".
		if resolvedLine != "" && !strings.HasSuffix(resolvedLine, " ") {
			resolvedLine += " "
		}
		resolvedLine += strings.Join(res.Args, " ")
	}

	// Once every token has resolved and nothing is left half typed, add a
	// trailing space whether or not the user typed one. That lets the next
	// Tab show what comes next.
	if len(res.Args) == 0 && resolvedLine != "" && !strings.HasSuffix(resolvedLine, " ") {
		resolvedLine += " "
	}
	rest := string(line[pos:])
	full := resolvedLine + rest

	// The argument hint, "<cr>", or both. This fires once the line resolves
	// to one command with nothing below it to descend into. A command that
	// has subcommands goes through the ambiguous branch instead.
	//
	// Unlike the candidate list above, this prints on the first trailing
	// space rather than waiting for a second Tab, the same as HelpForPath's
	// equivalent case.
	if trailingSpace && len(res.Args) > 0 && res.Args[len(res.Args)-1] == "" &&
		res.Command != nil && res.Command.RunFunc != nil && len(res.Command.Subcommands) == 0 {
		hint := res.Command.ResolvedArgHelp(l.translator)
		if !command.AcceptsMoreArgs(res.Command, res.Args) {
			// Every argument slot is filled. See HelpForPath for the
			// same rule on the "?" path.
			hint = ""
		}
		var out string
		switch {
		case hint != "" && res.RunnableAsIs:
			out = " " + hint + "\n <cr>\n"
		case hint != "":
			out = " " + hint + "\n"
		case res.RunnableAsIs:
			out = " <cr>\n"
		}
		if out != "" {
			fmt.Fprint(l.instance.Stdout(), l.currentPrompt+typed+"\n"+out)
		}
	}

	if full == string(line) {
		return nil, 0, false
	}

	l.logger.Debugln("DEBUG: expanding", typed, "to", resolvedLine)
	return []rune(full), len([]rune(resolvedLine)), true
}

// handleHelp - Pressing "?" on Cisco and HP shows contextual help right
// away, with no Enter, and the "?" never becomes part of the line.
//
// This implements that. It prints the help for whatever has been typed so
// far and returns the line with the "?" removed.
//
// Removing it is necessary because "?" is an ordinary printable key, so
// readline has already inserted it into the buffer by the time this runs.
// Tab never has this problem, since readline inserts nothing for it.
//
// The help itself comes from command.HelpForPath, the same function the
// non-interactive path uses for a piped "show ?". That path exists
// separately because readline only calls a Listener for a real terminal.
//
// A partial word before the "?", such as "sh?", falls through to whatever
// HelpForPath makes of the still partial tokens. That behaves like full
// help one level too high rather than the word help Cisco shows. It is a
// known gap.
func (l *TreeListener) handleHelp(line []rune, pos int) (newLine []rune, newPos int, ok bool) {
	// Any double Tab sequence in progress is no longer relevant, the same
	// reasoning as the non-Tab branch below.
	l.tapCount = 0
	l.lastAmbiguousInput = ""

	tokens, restored, restoredPos := helpTokensAndRestoredBuffer(line, pos)

	tree := l.position.Current().Tree
	tokens, _ = unwrapHelpTokens(tree, tokens)

	width := paging.EffectiveTerminalWidth(l.terminalFD, l.terminalWidthOverride, l.defaultTerminalWidth)
	help := command.HelpForPath(tree, tokens, l.translator, l.listOptions, width)
	if help != "" {
		fmt.Fprint(l.instance.Stdout(), l.currentPrompt+string(line[:pos-1])+"?\n"+help)
	}

	return restored, restoredPos, true
}

// ----------------------------------------------------------------------
// Private Functions - TreeListener
// ----------------------------------------------------------------------

// unwrapHelpTokens - Typing a command name after "help" should complete
// and show contextual help the same way typing that name directly does.
// Without this, everything after "help" is just its free-form argument,
// with nothing to complete against.
//
// This detects tokens resolving to "help" with leftover arguments and
// resolves those leftovers against the same tree instead, repeating while
// that keeps happening, so "help help con" also works.
//
// Both OnChange and handleHelp call it first, rather than each
// reimplementing the unwrap.
//
// The "help" entry is matched by identity against the literal tree entry,
// not by a resolved command name, because the word a session types to
// reach this is always the literal "help" from
// example/var/tree/level_common.yaml. A deployment renaming it gets no unwrapping,
// which is correct: the feature is tied to that word.
func unwrapHelpTokens(tree map[string]*command.Command, tokens []string) (resolvedTokens []string, prefix string) {
	resolvedTokens = tokens
	for {
		res := command.Resolve(tree, resolvedTokens)
		if len(res.Ambiguous) > 0 || res.Command == nil || len(res.Args) == 0 || !isHelpCommand(tree, res.Command) {
			return resolvedTokens, prefix
		}
		prefix += strings.Join(res.FullName, " ") + " "
		resolvedTokens = res.Args
	}
}

// isHelpCommand - This function reports whether cmd is the command
// reachable as "help" in tree, matched by pointer identity rather than by
// name, so a node that merely happens to be named "help" elsewhere is not
// mistaken for it.
//
// A tree with no "help" entry, a level built with skip_common for
// instance, never matches: tree["help"] is nil, and cmd is never nil at
// either call site.
func isHelpCommand(tree map[string]*command.Command, cmd *command.Command) bool {
	target, ok := tree["help"]
	return ok && target == cmd
}

// ambiguousRewriteBuffer - This function builds the rewritten buffer for
// an ambiguous match.
//
// It must include the ambiguous token itself, exactly as typed, not just
// the resolved prefix before it. res.FullName covers only tokens that
// resolved unambiguously, so when the ambiguity is on the first token,
// "en" matching both "enable" and "end", FullName is empty. Using it
// alone would wipe out what the user typed.
//
// res.AmbigAt indexes the tokens Resolve walked, which is tokens[1:] when
// Negated, since "no" was stripped before Resolve recursed. Translating
// back to the original slice needs one added in that case.
//
// This is separate from OnChange so it can be unit tested without a real
// readline instance.
func ambiguousRewriteBuffer(tokens []string, res command.ResolveResult, noPrefix string) string {
	resolvedPrefix := strings.Join(res.FullName, " ")

	ambigIdx := res.AmbigAt
	if res.Negated {
		ambigIdx++
	}
	ambiguousToken := ""
	if ambigIdx >= 0 && ambigIdx < len(tokens) {
		ambiguousToken = tokens[ambigIdx]
	}

	newBuf := noPrefix + resolvedPrefix
	if resolvedPrefix != "" {
		newBuf += " "
	}
	newBuf += ambiguousToken
	return newBuf
}

// ambiguousTokenIsEmpty - Two different situations both surface as
// ambiguous, and Cisco and HP treat them differently.
//
// Nothing typed yet for this position, Tab right after a space or at a
// bare prompt, has no partial word to be uncertain about, so a device
// lists what is available on the first Tab.
//
// A partial word that could grow into more than one command, "s" for show
// or set, should not dump a list on one Tab, since the user may still be
// typing. That is what the double Tab confirmation is for.
//
// This reports which case applies, adjusting for a leading "no" the same
// way ambiguousRewriteBuffer does. It is separated out for the same
// reason: pure logic, testable without a readline instance.
func ambiguousTokenIsEmpty(tokens []string, res command.ResolveResult) bool {
	ambigIdx := res.AmbigAt
	if res.Negated {
		ambigIdx++
	}
	return ambigIdx >= 0 && ambigIdx < len(tokens) && tokens[ambigIdx] == ""
}

// helpTokensAndRestoredBuffer - This function works out the token path to
// look up help for, and the buffer and cursor readline should be left
// with, for a handleHelp call. before and after split the original buffer
// where the "?" was inserted.
//
// It is separate from handleHelp so it can be unit tested. Constructing a
// readline.Instance needs real raw mode terminal control, and handleHelp
// needs one only to print through.
func helpTokensAndRestoredBuffer(line []rune, pos int) (tokens []string, restored []rune, restoredPos int) {
	before := line[:pos-1]
	after := line[pos:]
	typed := string(before)

	tokens = strings.Fields(typed)
	if strings.HasSuffix(typed, " ") || len(tokens) == 0 {
		tokens = append(tokens, "")
	}

	restored = make([]rune, 0, len(before)+len(after))
	restored = append(restored, before...)
	restored = append(restored, after...)
	return tokens, restored, pos - 1
}
