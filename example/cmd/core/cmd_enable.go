// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This registers "enable" and "disable", the exec level's entry and
// exit commands.
//
// This is an ordinary command file. Nothing in package command special
// cases "exec". The file could be named cmd_operator.go entering a level
// called "operator" and nothing beyond the string literals would change.
//
// EnterCommandLevel and ExitCommandLevel do the mechanical work: the
// parent check, the password check, updating the session, and swapping the
// root frame. They report what happened and print nothing.
//
// Every word printed below is this file's decision. A different deployment
// might log it to the audit trail instead, or say nothing.
func init() {
	command.Register("enable", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["exec"]
		entered, err := command.EnterCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !entered {
			fmt.Println(ctx.Translator.T("enable.already_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("enable.entered"))
		return nil
	})

	command.Register("disable", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["exec"]
		exited, err := command.ExitCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !exited {
			fmt.Println(ctx.Translator.T("disable.not_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("disable.left"))
		return nil
	})
}
