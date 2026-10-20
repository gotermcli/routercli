// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This registers "diagnostic-mode" and "exit-diagnostic-mode", the
// diagnostic level's entry and exit commands, by their registered names.
//
// The word a session types to leave, "return", is decided by the diagnostic
// tree file, whose "return" entry points at "exit-diagnostic-mode". Nothing
// here changes if that word does.
//
// The file cmd_diagnostic.go, note the different name, registers
// "self-test", a command reachable from inside the mode. That is a
// different thing from entering or leaving it.
func init() {
	command.Register("diagnostic-mode", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["diagnostic"]
		entered, err := command.EnterCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !entered {
			fmt.Println(ctx.Translator.T("diagnostic_mode.already_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("diagnostic_mode.entered"))
		return nil
	})

	command.Register("exit-diagnostic-mode", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["diagnostic"]
		exited, err := command.ExitCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !exited {
			fmt.Println(ctx.Translator.T("exit_diagnostic_mode.not_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("exit_diagnostic_mode.left"))
		return nil
	})
}
