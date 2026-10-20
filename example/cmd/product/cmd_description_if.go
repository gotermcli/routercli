// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This registers "description <text>" for config-if mode,
// negatable.
//
// It registers under a different name from the root level
// "set description", even though both set a field called Description. The
// two are scoped to different state and reachable from different modes.
//
// ValidateArgs is skipped for a negated command, so args is usually empty
// on the "no description" path. Extra tokens after it are accepted and
// ignored, matching how lenient Cisco is about trailing tokens that change
// nothing.
func init() {
	command.Register("description.interface", func(ctx *command.AppContext, args []string) error {
		ifaceName := ctx.Position.Current().Context.(string)

		_, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
			state := productState.(*State)
			iface := state.Interface(ifaceName)

			if ctx.Negated {
				iface.Description = ""
				ctx.Logger.Debugln("DEBUG: description cleared for interface", ifaceName)
				fmt.Println(ctx.Translator.T("description_if.cleared"))
				return nil, nil
			}

			// MinArgs/MaxArgs (both 1) are enforced by the framework for
			// the non-negated path, so args[0] is guaranteed to exist
			// here.
			iface.Description = args[0]
			ctx.Logger.Debugln("DEBUG: description set for interface", ifaceName)
			fmt.Println(ctx.Translator.T("description_if.confirm"))
			return nil, nil
		})
		return err
	})
}
