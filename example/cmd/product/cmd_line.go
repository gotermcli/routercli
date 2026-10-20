// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"
	"strconv"

	"github.com/gotermcli/routercli/command"
)

// lineLengthMin, lineLengthMax, lineWidthMin, and lineWidthMax match the
// <0-512> range example/cmd/session/cmd_terminal.go enforces for the session scoped
// "terminal" commands.
//
// They are declared here rather than imported, because cmd/core,
// cmd/product, and cmd/session are independent siblings that never import
// each other. Four small integers cost less than inverting that boundary.
const (
	lineLengthMin = 0
	lineLengthMax = 512
	lineWidthMin  = 0
	lineWidthMax  = 512
)

// init - A deployment needs page height, terminal width, and pager on or
// off to survive a restart. The "terminal" commands are session scoped, so
// they cannot carry a default.
//
// This registers "line", which pushes config-line mode, and inside it
// "length <n>", "width <n>", and "paging", negatable as "no paging". These
// are the deployment wide defaults, persisted through running-config and
// startup-config, the role "line vty" and "line console" play on Cisco and
// HP.
//
// Each handler writes the value to State and then applies it immediately
// to the matching AppContext field, so a change takes effect in this
// process rather than only after a restart replays it.
//
// State goes through MutateProductState. The AppContext fields are session
// local, so they stay direct assignments.
//
// There is one global set of defaults, with no vty and console split.
// RouterCLI has no listener of its own, so a process cannot tell whether it
// was reached over a network or a local console. Splitting this stays open
// until it does. That is a known framework gap.
//
// This lives in cmd/product rather than cmd/core because a persisted
// deployment default is product state, the same as hostname and banner,
// not a framework capability every project needs.
func init() {
	command.Register("line", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["config-line"]
		if err := command.RequireCurrentCommandLevel(ctx, "config-line", level.Parent); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: entering line configuration")
		ctx.Position.Push(command.CommandLevelFrame{
			Name:         "config-line",
			PromptSuffix: level.PromptSuffix,
			Tree:         level.Tree,
		})
		return nil
	})

	command.Register("line.length", func(ctx *command.AppContext, args []string) error {
		n, err := parseLineGeometry(ctx, args[0], lineLengthMin, lineLengthMax)
		if err != nil {
			return err
		}
		if _, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)
			state.Line.Length = &n
			return nil, nil
		}); err != nil {
			return err
		}
		ctx.DefaultPageLines = n
		ctx.Logger.Debugln("DEBUG: line length default set to", n)
		fmt.Println(ctx.Translator.T("line.length.confirm", n))
		return nil
	})

	command.Register("line.width", func(ctx *command.AppContext, args []string) error {
		n, err := parseLineGeometry(ctx, args[0], lineWidthMin, lineWidthMax)
		if err != nil {
			return err
		}
		if _, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)
			state.Line.Width = &n
			return nil, nil
		}); err != nil {
			return err
		}
		ctx.DefaultTerminalWidth = n
		ctx.Logger.Debugln("DEBUG: line width default set to", n)
		fmt.Println(ctx.Translator.T("line.width.confirm", n))
		return nil
	})

	command.Register("line.paging", func(ctx *command.AppContext, args []string) error {
		enabled := !ctx.Negated
		if _, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)
			state.Line.Paging = &enabled
			return nil, nil
		}); err != nil {
			return err
		}
		ctx.PagingEnabled = enabled
		ctx.Logger.Debugln("DEBUG: line paging default set to", enabled)
		if enabled {
			fmt.Println(ctx.Translator.T("line.paging.confirm_enabled"))
		} else {
			fmt.Println(ctx.Translator.T("line.paging.confirm_disabled"))
		}
		return nil
	})
}

// parseLineGeometry - This function holds the parse and range check "line
// length" and "line width" both need.
//
// It duplicates cmd/session's parseTerminalGeometry rather than importing
// it, since those packages stay independent siblings.
//
// The error messages reuse the "terminal.not_a_number" and
// "terminal.out_of_range" catalog keys. Both are written generically, with
// no mention of "terminal" in the English text, so this is a genuinely
// shared message rather than a coincidental collision.
func parseLineGeometry(ctx *command.AppContext, raw string, min, max int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s", ctx.Translator.T("terminal.not_a_number", raw))
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s", ctx.Translator.T("terminal.out_of_range", raw, min, max))
	}
	return n, nil
}
