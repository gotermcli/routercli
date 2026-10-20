// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gotermcli/routercli/i18n"
)

// ----------------------------------------------------------------------
// Public Methods - Command
// ----------------------------------------------------------------------

// ResolvedDesc - This method returns this command's description, resolved
// through the translator if DescKey is set, otherwise the literal Desc
// field. A nil translator is handled the same as an empty DescKey, falling
// back to Desc, since this package works perfectly well with i18n never
// wired in at all, translation being additive rather than required.
func (c *Command) ResolvedDesc(t *i18n.Translator) string {
	if c.DescKey != "" && t != nil {
		return t.T(c.DescKey)
	}
	return c.Desc
}

// ResolvedHelp - This method does the same thing as ResolvedDesc, for Help
// and HelpKey.
func (c *Command) ResolvedHelp(t *i18n.Translator) string {
	if c.HelpKey != "" && t != nil {
		return t.T(c.HelpKey)
	}
	return c.Help
}

// ResolvedArgHelp - This method does the same thing as ResolvedDesc, for
// ArgHelp and ArgHelpKey.
func (c *Command) ResolvedArgHelp(t *i18n.Translator) string {
	if c.ArgHelpKey != "" && t != nil {
		return t.T(c.ArgHelpKey)
	}
	return c.ArgHelp
}

// ----------------------------------------------------------------------
// Public Functions - Command
// ----------------------------------------------------------------------

// Resolve - Command execution and tab completion need the same question
// answered: given these tokens, which command do they refer to, and is
// that answer unambiguous. Answering it twice would let the two disagree.
//
// This is the single answer both use.
//
// It walks tokens against tree by exact match, then unique prefix match,
// then follows any alias, then descends into Subcommands. The first token
// that matches nothing, and is not ambiguous, becomes the start of Args.
//
// An empty string token is valid input. The completer passes one to mean
// nothing has been typed at this position, which lists every child.
//
// A leading "no" is stripped first and the rest resolved normally, with
// Negated set on the result. FullName, Command, and Args always describe
// the real command; "no" never appears in them. Whether that command may
// be negated is not checked here.
//
// "no" MUST be typed in full. Cisco does not abbreviate it either.
func Resolve(tree map[string]*Command, tokens []string) (result ResolveResult) {
	defer func() {
		result.RunnableAsIs = runnableAsIs(result.Command, result.Args)
	}()

	if len(tokens) > 0 && tokens[0] == "no" {
		inner := Resolve(tree, tokens[1:])
		inner.Negated = true
		result = inner
		return result
	}

	current := tree
	var directives *Command

	for i, tok := range tokens {
		cmd, exact := current[tok]
		if !exact {
			// Not an exact match, so look for a unique, non-hidden prefix
			// match. A hidden command is still reachable by exact match
			// above, it just cannot be abbreviated to.
			var candidates []string
			for name, c := range current {
				if strings.HasPrefix(name, tok) && !c.Hidden {
					candidates = append(candidates, name)
				}
			}
			sort.Strings(candidates)

			switch len(candidates) {
			case 1:
				// Nothing typed yet here, but what has matched so far is
				// already runnable on its own: "totp enable" is complete
				// even though "totp enable qr" exists below it.
				//
				// Descending into the sole remaining subcommand would hide
				// that "enable" is already complete. Cisco and HP never do
				// that; both show "qr" alongside "<cr>".
				//
				// This surfaces it the same way genuine ambiguity is
				// surfaced, through the empty-token branch every caller
				// already handles.
				//
				// It never fires for a partial word. Abbreviating "sh" to
				// "show" when that is the only match is exactly what Cisco
				// and HP do.
				if tok == "" && runnableAsIs(directives, nil) {
					result.Ambiguous = candidates
					result.AmbiguousTree = current
					result.AmbigAt = i
					result.Command = directives
					return result
				}
				tok = candidates[0]
				cmd = current[tok]
			case 0:
				// Nothing matches, so the remaining tokens are arguments.
				result.Args = append(result.Args, tokens[i:]...)
				result.Command = directives
				return result
			default:
				// Ambiguous: more than one command could match this token.
				result.Ambiguous = candidates
				result.AmbiguousTree = current
				result.AmbigAt = i
				result.Command = directives
				return result
			}
		}

		// Chase any alias to the real command it points at.
		for cmd.Alias != "" {
			cmd = current[cmd.Alias]
		}

		directives = cmd
		result.FullName = append(result.FullName, tok)

		if len(cmd.Subcommands) > 0 {
			current = cmd.Subcommands
		} else {
			// No children below this command, so command matching stops
			// here. Anything left over is arguments, for example "show
			// version extra".
			result.Args = append(result.Args, tokens[i+1:]...)
			result.Command = directives
			return result
		}
	}

	result.Command = directives
	return result
}

// AcceptsMoreArgs - This function reports whether cmd could still take
// another argument beyond the ones in args.
//
// A "?" or a Tab shows the argument hint only when the answer is yes.
// Showing "<name>" after "hostname test" tells a session it may type
// something the command would then refuse, since MaxArgs is already
// satisfied.
//
// One trailing empty string is stripped first, the same synthetic "nothing
// typed yet" placeholder runnableAsIs strips, so a trailing space does not
// count as an argument.
//
// A nil MaxArgs means unbounded, so this is always true for a command such
// as "alias" that takes the rest of the line.
func AcceptsMoreArgs(cmd *Command, args []string) bool {
	if cmd == nil {
		return false
	}
	if cmd.MaxArgs == nil {
		return true
	}

	supplied := args
	if n := len(supplied); n > 0 && supplied[n-1] == "" {
		supplied = supplied[:n-1]
	}

	return len(supplied) < *cmd.MaxArgs
}

// ValidateArgs - This function enforces MinArgs, MaxArgs, and
// MaxArgLength for a resolved command.
//
// It is separate from Resolve because which command was meant and whether
// its arguments are acceptable are different questions with different
// failure messages.
//
// A caller MUST NOT call it when Negated is true. "no X" often takes a
// different argument shape than "X": Cisco's "no description" takes none
// to clear a value while "description <text>" requires one to set it.
// Forcing one MinArgs and MaxArgs to describe both would make the positive
// form accept arguments it should not, or the negated form reject the ones
// it needs.
//
// A negatable handler checks len(args) itself.
func ValidateArgs(cmd *Command, args []string) error {
	if cmd.MinArgs != nil && len(args) < *cmd.MinArgs {
		return fmt.Errorf("not enough arguments: need at least %d, got %d", *cmd.MinArgs, len(args))
	}
	if cmd.MaxArgs != nil && len(args) > *cmd.MaxArgs {
		return fmt.Errorf("too many arguments: accepts at most %d, got %d", *cmd.MaxArgs, len(args))
	}
	if cmd.MaxArgLength > 0 {
		for _, a := range args {
			if len([]rune(a)) > cmd.MaxArgLength {
				return fmt.Errorf("argument exceeds maximum length of %d characters: %q", cmd.MaxArgLength, a)
			}
		}
	}
	return nil
}

// ----------------------------------------------------------------------
// Private Functions - Command
// ----------------------------------------------------------------------

// runnableAsIs - This function reports whether cmd can be run right now
// with args, which is what Cisco and HP's "<cr>" notation means.
//
// cmd needs a RunFunc of its own, so a pure container is never runnable as
// is, and args must satisfy MinArgs and MaxArgs.
//
// One trailing empty string is stripped before counting. Both callers
// append a synthetic "" token to mean nothing typed yet at this position.
// That placeholder is not a real argument, so counting it would report a
// command such as plain "exit" as not runnable the moment a trailing space
// is typed after it.
//
// Stripping at most one is always safe, since tokenizing never produces an
// empty string as a real argument.
func runnableAsIs(cmd *Command, args []string) bool {
	if cmd == nil || cmd.RunFunc == nil {
		return false
	}

	effective := args
	if n := len(effective); n > 0 && effective[n-1] == "" {
		effective = effective[:n-1]
	}

	if cmd.MinArgs != nil && len(effective) < *cmd.MinArgs {
		return false
	}
	if cmd.MaxArgs != nil && len(effective) > *cmd.MaxArgs {
		return false
	}
	return true
}
