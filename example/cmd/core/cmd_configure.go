// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"github.com/gotermcli/routercli/command"
)

// init - This function registers "configure terminal", which enters config
// mode by pushing a new frame onto ctx.Position.
//
// The tree and prompt suffix for that frame come from the loaded tree
// structure rather than being hardcoded here. A broken level_config.yaml
// then fails the program at startup rather than the first time someone
// types this command, and the prompt suffix lives in one place.
//
// The string "config" is still a literal in this file. Which Command Level
// this command enters is its own business, the same way "configure" and
// "terminal" are literals a moment later. Nothing in package command names
// "config" anywhere.
//
// The current level check below is the same one cmd_enable.go and
// cmd_diagnostic_mode.go make through EnterCommandLevel. It is called
// directly here because entering config also has to push a frame rather
// than swap the root tree, which is why a nested, stacking mode cannot use
// EnterCommandLevel.
//
// That check should already be unreachable, since the base level never
// lists "configure" as a command at all. It is defense in depth, not the
// only thing between a base level session and config mode.
func init() {
	command.Register("configure.terminal", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["config"]
		if err := command.RequireCurrentCommandLevel(ctx, "config", level.Parent); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: entering configuration mode")
		ctx.Position.Push(command.CommandLevelFrame{
			Name:         "config",
			PromptSuffix: level.PromptSuffix,
			Tree:         level.Tree,
		})
		return nil
	})
}
