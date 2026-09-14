// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"fmt"
	"strconv"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/paging"
)

// terminalLengthMin, terminalLengthMax, terminalWidthMin, and
// terminalWidthMax match the <0-512> range shown in the ArgHelp hint and
// the range Cisco IOS accepts for both commands.
//
// They are named constants so the validation and the hint text cannot
// drift apart the way two hand typed numbers eventually do.
//
// Zero is not just the bottom of the range. For "terminal length" it
// carries the well known meaning "never pause".
const (
	terminalLengthMin = 0
	terminalLengthMax = 512
	terminalWidthMin  = 0
	terminalWidthMax  = 512
)

// init - This registers "terminal length <n>", "terminal width <n>", and
// "terminal filter-mode <substring|regex>" for config mode. All three are
// session scoped and never persisted, the same as Cisco and HP, which keep
// them out of running-config.
//
// "terminal length" sets the page size the pager uses instead of detecting
// the terminal height. Zero disables the pager, the Cisco convention.
//
// "terminal width" sets the width. Nothing paces output by column width
// today; the field exists so an implementation that formats to a fixed
// width has one place to find the session override.
//
// "terminal filter-mode" chooses how "| include", "| exclude", and
// "| begin" patterns are matched for the rest of the session.
//
// ValidateArgs checks argument count and length only. Whether an argument
// parses as a number in range, or is one of a fixed set of words, is each
// handler's own job.
func init() {
	command.Register("terminal.length", func(ctx *command.AppContext, args []string) error {
		n, err := parseTerminalGeometry(ctx, args[0], terminalLengthMin, terminalLengthMax)
		if err != nil {
			return err
		}
		ctx.PageLines = &n
		ctx.Logger.Debugln("DEBUG: terminal length set to", n)
		fmt.Println(ctx.Translator.T("terminal.confirm", "length", n))
		return nil
	})

	command.Register("terminal.width", func(ctx *command.AppContext, args []string) error {
		n, err := parseTerminalGeometry(ctx, args[0], terminalWidthMin, terminalWidthMax)
		if err != nil {
			return err
		}
		ctx.TerminalWidth = &n
		ctx.Logger.Debugln("DEBUG: terminal width set to", n)
		fmt.Println(ctx.Translator.T("terminal.confirm", "width", n))
		return nil
	})

	command.Register("terminal.filter-mode", func(ctx *command.AppContext, args []string) error {
		switch args[0] {
		case "substring":
			ctx.FilterMode = paging.FilterModeSubstring
		case "regex":
			ctx.FilterMode = paging.FilterModeRegex
		default:
			return fmt.Errorf("%s", ctx.Translator.T("terminal.filter_mode.invalid", args[0]))
		}
		ctx.Logger.Debugln("DEBUG: terminal filter-mode set to", args[0])
		fmt.Println(ctx.Translator.T("terminal.filter_mode.confirm", args[0]))
		return nil
	})
}

// parseTerminalGeometry - This function holds the parse and range check
// both "terminal length" and "terminal width" need, so it exists once
// rather than twice with two chances to drift.
//
// Each handler still owns its own assignment and confirmation message,
// since one sets PageLines and the other TerminalWidth.
func parseTerminalGeometry(ctx *command.AppContext, raw string, min, max int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s", ctx.Translator.T("terminal.not_a_number", raw))
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s", ctx.Translator.T("terminal.out_of_range", raw, min, max))
	}
	return n, nil
}
