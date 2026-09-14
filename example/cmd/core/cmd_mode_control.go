// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"github.com/gotermcli/routercli/command"
)

// init - This registers "exit" and "end", present in every mode through
// example/var/tree/level_common.yaml rather than duplicated into each tree file,
// matching Cisco IOS.
//
// "exit" goes up exactly one level, and quits the program only when
// already at the root. "end" jumps straight back to exec from any depth.
//
// "exit" signals a quit by returning command.ErrQuit, which runLoop checks
// after every handler. runLoop has no idea a command named "exit" exists;
// it only knows that sentinel. That is what lets "exit" behave differently
// depending on the mode it runs from.
func init() {
	command.Register("exit", func(ctx *command.AppContext, args []string) error {
		if ctx.Position.AtRoot() {
			return command.ErrQuit
		}
		ctx.Position.Pop()
		return nil
	})

	command.Register("end", func(ctx *command.AppContext, args []string) error {
		ctx.Position.PopToRoot()
		for ctx.Levels != nil && ctx.Session != nil && ctx.Session.CommandLevel != "exec" {
			level, ok := ctx.Levels.ByName[ctx.Session.CommandLevel]
			if !ok || level.Parent == "" {
				break
			}
			parent, ok := ctx.Levels.ByName[level.Parent]
			if !ok {
				break
			}
			exited, err := command.ExitCommandLevel(ctx, level, parent)
			if err != nil || !exited {
				break
			}
		}
		return nil
	})
}
