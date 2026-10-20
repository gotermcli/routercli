// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This registers "banner motd <text>" and "banner login <text>" for
// config mode, both negatable.
//
// Cisco and HP print a MOTD to every connection before authentication,
// since a message of the day is about the connection rather than about who
// is logging in. They print a separate login banner immediately before the
// username prompt, and only when one is going to run.
//
// RouterCLI follows the same two banner, two moment design. Neither is
// shown again once a session is past that point, and there is no
// "banner exec" equivalent.
func init() {
	command.Register("banner.motd", func(ctx *command.AppContext, args []string) error {
		_, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)

			if ctx.Negated {
				state.BannerMOTD = ""
				ctx.Logger.Debugln("DEBUG: banner motd cleared")
				fmt.Println(ctx.Translator.T("banner.motd.cleared"))
				return nil, nil
			}

			// MinArgs/MaxArgs (both 1, see example/var/tree/level_config.yaml)
			// guarantee args[0] exists here on the non-negated path. A
			// banner containing spaces or newlines is typed as one quoted
			// token, the same convention "hostname" and "set description"
			// already establish for free text arguments.
			state.BannerMOTD = args[0]
			ctx.Logger.Debugln("DEBUG: banner motd set")
			fmt.Println(ctx.Translator.T("banner.motd.confirm"))
			return nil, nil
		})
		return err
	})

	command.Register("banner.login", func(ctx *command.AppContext, args []string) error {
		_, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)

			if ctx.Negated {
				state.BannerLogin = ""
				ctx.Logger.Debugln("DEBUG: banner login cleared")
				fmt.Println(ctx.Translator.T("banner.login.cleared"))
				return nil, nil
			}

			state.BannerLogin = args[0]
			ctx.Logger.Debugln("DEBUG: banner login set")
			fmt.Println(ctx.Translator.T("banner.login.confirm"))
			return nil, nil
		})
		return err
	})
}
