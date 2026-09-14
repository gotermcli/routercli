// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"github.com/gotermcli/routercli/command"
)

// init - This registers "interface <name>", a config mode command that
// pushes config-if, the second level of nesting.
//
// Context on the pushed frame is the interface name. cmd_description_if.go
// and cmd_shutdown.go read it back to know which interface they are
// editing, so neither this file nor CommandLevelStack needs to know what
// an interface is. That meaning lives entirely in this package.
//
// The tree and prompt suffix come from the loaded level rather than being
// hardcoded.
//
// RequireCurrentCommandLevel enforces config-if's declared parent, the
// same way cmd_configure.go enforces config's. This command only appears
// inside config's tree anyway, so that is defense in depth rather than the
// only thing stopping it running elsewhere.
func init() {
	command.Register("interface", func(ctx *command.AppContext, args []string) error {
		// MinArgs/MaxArgs (both 1, see example/var/tree/level_config.yaml)
		// guarantee args[0] exists here.
		name := args[0]
		level := ctx.Levels.ByName["config-if"]
		if err := command.RequireCurrentCommandLevel(ctx, "config-if", level.Parent); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: entering interface configuration for", name)
		ctx.Position.Push(command.CommandLevelFrame{
			Name:         "config-if",
			PromptSuffix: level.PromptSuffix,
			Tree:         level.Tree,
			Context:      name,
		})
		return nil
	})
}
