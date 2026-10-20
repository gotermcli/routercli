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

// sameWords - This function reports whether two token slices are identical,
// which is how the alias shadow check decides an alias changes nothing.
func sameWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

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
		// An exact name match is not enough. Commands resolve by unique
		// prefix, so "ena" reaches "enable" even though no command is
		// named "ena". ExpandAlias matches the typed word exactly and
		// runs before Resolve, so an alias named "ena" takes that word
		// away from "enable" at this Command Level.
		//
		// With a daemon, a level's Aliases are shared canonical state, so
		// an unprivileged session could otherwise stop every operator
		// elevating, and the failure would surface as an unrelated
		// looking error rather than as anything pointing at the alias.
		//
		// What matters is redirection, not overlap. "alias int interface"
		// is idiomatic, already present in deployed configurations, and
		// harmless: "int" resolves to "interface" either way, so the
		// alias changes nothing. "alias int show" is the attack, because
		// it sends an established abbreviation somewhere else.
		//
		// So an alias whose name reaches a command is allowed only when
		// its expansion reaches the same command. Anything that would
		// redirect is refused, and an ambiguous name is refused outright,
		// since resolving what the CLI deliberately declines to guess is
		// itself a redirect.
		if nameRes := command.Resolve(ctx.Position.Current().Tree, []string{aliasName}); nameRes.Command != nil || len(nameRes.Ambiguous) > 0 {
			// Equivalent means the expansion is exactly the full name the
			// alias name already resolves to. "alias int interface"
			// passes, since "int" resolves to "interface" either way.
			// "alias int show" and "alias show show version" both fail,
			// since each sends the word somewhere it did not go before.
			if !sameWords(expansion, nameRes.FullName) {
				return fmt.Errorf("%s", ctx.Translator.T("alias.collides_with_command", aliasName, currentName))
			}
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
