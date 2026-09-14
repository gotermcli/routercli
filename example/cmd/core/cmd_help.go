// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"
	"os"
	"strings"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/paging"
)

// init - This registers "help", present in every mode through
// example/var/tree/level_common.yaml.
//
// Typed alone, the listing walks the current position rather than the
// whole tree, so "help" shows config mode commands while in config mode.
// Adding a command to any tree file makes it appear here with no change
// to this file.
//
// Typed with a command name, it answers a different question: what does
// this command do, rather than what can I type next, which is what "?"
// answers. Cisco and HP have no "help <command>" form; theirs comes from
// a trailing "?", which RouterCLI still supports.
//
// An ambiguous name prints the candidates. A name matching nothing is an
// error, so a typo is visible as a typo.
//
// The terminal width is resolved on every call rather than cached, so a
// mid session resize is reflected next time.
func init() {
	command.Register("help", func(ctx *command.AppContext, args []string) error {
		width := paging.EffectiveTerminalWidth(int(os.Stdin.Fd()), ctx.TerminalWidth, ctx.DefaultTerminalWidth)
		if len(args) == 0 {
			fmt.Print(command.HelpText(ctx.Position.Current().Tree, ctx.Translator, ctx.ListOptions, width))
			return nil
		}
		text := command.DetailedHelp(ctx.Position.Current().Tree, args, ctx.Translator, ctx.ListOptions, ctx.ProductName, width)
		if text == "" {
			return fmt.Errorf("%s", ctx.Translator.T("help.unknown_command", strings.Join(args, " ")))
		}
		fmt.Print(text)
		return nil
	})
}
