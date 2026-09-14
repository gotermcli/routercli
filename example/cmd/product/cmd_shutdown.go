// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This registers "shutdown" for config-if mode, negatable, so
// "no shutdown" runs this same handler with ctx.Negated set rather than
// being a separate registration. Resolve treats "no" as a modifier, so one
// registration covers both directions.
//
// State is reached through MutateProductState. The interface name comes
// from ctx.Position, which is session local, so it is resolved once,
// outside the closure.
func init() {
	command.Register("interface.shutdown", func(ctx *command.AppContext, args []string) error {
		ifaceName := ctx.Position.Current().Context.(string)

		_, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)
			iface := state.Interface(ifaceName)

			if ctx.Negated {
				iface.Shutdown = false
				ctx.Logger.Debugln("DEBUG: interface administratively enabled", ifaceName)
				fmt.Println(ctx.Translator.T("no_shutdown.confirm"))
				return nil, nil
			}

			iface.Shutdown = true
			ctx.Logger.Debugln("DEBUG: interface administratively shut down", ifaceName)
			fmt.Println(ctx.Translator.T("shutdown.confirm"))
			return nil, nil
		})
		return err
	})
}
