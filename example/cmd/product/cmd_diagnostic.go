// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This function registers "self-test" for the "diagnostic" Command
// Level. See example/var/tree/level_diagnostic.yaml and
// example/var/tree/tree_structure.yaml. This is a trivial example command whose
// only real purpose is to be something to run from inside a third-tier,
// non-inheriting privilege level, proving that tier is more than an empty
// shell. A real project's diagnostic mode would presumably do something
// considerably more interesting than print a canned string.
func init() {
	command.Register("diagnostic.self-test", func(ctx *command.AppContext, args []string) error {
		ctx.Logger.Debugln("DEBUG: running self-test")
		fmt.Println(ctx.Translator.T("self_test.result"))
		return nil
	})
}
