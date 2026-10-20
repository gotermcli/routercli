// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gotermcli/routercli/command"
)

// show.terminal and show.history, and the terminalStatusLines and
// historyLines functions that built their own output, moved to
// example/cmd/session/cmd_show.go: both report state local to one connection,
// never shared canonical state a daemon will one day own. See
// example/cmd/session/doc.go for the full reasoning behind that boundary.
func init() {
	command.Register("show.version", func(ctx *command.AppContext, args []string) error {
		fmt.Println(ctx.Translator.T("show.version.text"))
		return nil
	})

	command.Register("show.aliases", func(ctx *command.AppContext, args []string) error {
		lines := aliasesLines(ctx)
		if len(lines) == 0 {
			fmt.Println(ctx.Translator.T("show.aliases.empty"))
			return nil
		}
		for _, line := range lines {
			fmt.Println(line)
		}
		return nil
	})
}

// aliasesLines - This function builds "show aliases" output: one header
// line per Command Level that has any alias, then one indented line per
// alias, name and what it expands to.
//
// Levels appear in manifest load order, the same order configModeLines
// walks password hashes in. Within a level, alias names are sorted, so the
// listing is stable even though Aliases is a plain map.
//
// A level with no aliases contributes nothing, not even a header.
func aliasesLines(ctx *command.AppContext) []string {
	var lines []string
	if ctx.Levels == nil {
		return lines
	}

	for _, level := range ctx.Levels.Order {
		if len(level.Aliases) == 0 {
			continue
		}

		names := make([]string, 0, len(level.Aliases))
		for name := range level.Aliases {
			names = append(names, name)
		}
		sort.Strings(names)

		lines = append(lines, ctx.Translator.T("show.aliases.level_header", level.Name))
		for _, name := range names {
			lines = append(lines, ctx.Translator.T("show.aliases.line", name, strings.Join(level.Aliases[name], " ")))
		}
	}

	return lines
}
