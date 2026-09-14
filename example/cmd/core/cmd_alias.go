// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"
	"strings"

	"github.com/gotermcli/routercli/command"
)

// init - This function registers "alias <alias-name> <word...>", reachable
// from every Command Level and negatable as "no alias <alias-name>".
//
// An alias belongs to whichever Command Level the session is standing in
// when it is defined. There is no level argument to get wrong.
//
// alias-name is the word typed from then on. word... is what it expands to,
// taken literally.
//
// Nothing is persisted here. An alias lives on the level for the rest of
// the process, and "write memory" is what saves it.
//
// Redefining an existing alias is refused. "no alias <alias-name>" MUST
// remove it first. This is a security measure rather than the "define
// again to change it" convention Cisco uses: otherwise anyone at the
// session could quietly change what a trusted alias expands to, and a
// session that never runs "show aliases" would not notice.
//
// That check is waived while replaying a startup-config, which restates
// aliases the session already has.
//
// Tab completion does not expand an alias the way the read loop does.
// Typing one still runs the command it expands to. This is a known gap.
func init() {
	command.Register("alias", func(ctx *command.AppContext, args []string) error {
		currentName := ctx.Position.Current().Name
		level, ok := ctx.Levels.ByName[currentName]
		if !ok {
			return fmt.Errorf("%s", ctx.Translator.T("alias.unknown_level", currentName))
		}

		if ctx.Negated {
			// ValidateArgs is never called for a negated command, see
			// command.ValidateArgs's doc comment, so the minimum shape "no
			// alias <alias-name>" needs is checked by hand here, before
			// args[0] is read.
			if len(args) < 1 {
				return fmt.Errorf("%s", ctx.Translator.T("alias.negate_usage"))
			}
			aliasName := args[0]
			if _, defined := level.Aliases[aliasName]; !defined {
				return fmt.Errorf("%s", ctx.Translator.T("alias.not_defined", aliasName, currentName))
			}
			_, err := ctx.DaemonClient.MutateLevels(func(levels *command.TreeStructure) (any, error) {
				delete(levels.ByName[currentName].Aliases, aliasName)
				return nil, nil
			})
			if err != nil {
				return err
			}
			ctx.Logger.Debugln("DEBUG: alias removed:", aliasName, "from level", currentName)
			fmt.Println(ctx.Translator.T("alias.removed", aliasName, currentName))
			return nil
		}

		// MinArgs (2, see example/var/tree/level_common.yaml) guarantees args[0]
		// and args[1] both exist here.
		aliasName, expansion := args[0], args[1:]

		if aliasName == "no" {
			// "no" is a reserved word, see command.Resolve's doc comment,
			// never a real command name, so it can never be a real alias
			// name either, an alias called "no" could never be typed and
			// reached, since every leading "no" is always stripped and
			// treated as negation first.
			return fmt.Errorf("%s", ctx.Translator.T("alias.reserved_name", aliasName))
		}
		if _, exists := ctx.Position.Current().Tree[aliasName]; exists {
			// A real command already answers to this name at this Command
			// Level, "show" for instance, and silently shadowing it with
			// an alias would surprise a session far more than refusing the
			// alias outright.
			return fmt.Errorf("%s", ctx.Translator.T("alias.collides_with_command", aliasName, currentName))
		}
		if _, defined := level.Aliases[aliasName]; defined && !ctx.ReplayingStartupConfig {
			// See this function's doc comment for the security reasoning:
			// a session must remove an existing alias before it can be
			// redefined, rather than silently overwriting it in one step.
			return fmt.Errorf("%s", ctx.Translator.T("alias.already_defined", aliasName, currentName, "no alias "+aliasName))
		}

		_, err := ctx.DaemonClient.MutateLevels(func(levels *command.TreeStructure) (any, error) {
			target := levels.ByName[currentName]
			if target.Aliases == nil {
				target.Aliases = make(map[string][]string)
			}
			target.Aliases[aliasName] = expansion
			return nil, nil
		})
		if err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: alias defined:", aliasName, "->", strings.Join(expansion, " "), "for level", currentName)
		fmt.Println(ctx.Translator.T("alias.confirm", aliasName, currentName, strings.Join(expansion, " ")))
		return nil
	})
}
