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

// init - This function registers "language list" and "language set
// <code>", a runtime control over the active language, which is otherwise
// only configured at startup through CurrentLanguage, DefaultLanguage, and
// LanguageDir in the system configuration file. Every catalog is already
// loaded in memory at startup, see i18n.LoadCatalogs, so switching
// languages is just pointing at a different already loaded map, not
// rereading anything from disk. This is the i18n counterpart to "audit-log
// enable" and "audit-log disable".
func init() {
	command.Register("language.list", func(ctx *command.AppContext, args []string) error {
		langs := ctx.Translator.AvailableLanguages()
		fmt.Println(ctx.Translator.T("language.list.header"))
		for _, l := range langs {
			marker := "  "
			if l == ctx.Translator.CurrentLanguage() {
				marker = "* "
			}
			fmt.Println(marker + l)
		}
		return nil
	})

	command.Register("language.set", func(ctx *command.AppContext, args []string) error {
		// MinArgs and MaxArgs, both 1, see example/var/tree/level_base.yaml,
		// guarantee args[0] exists here, the same pattern as
		// set.description.
		requested := strings.ToLower(args[0])
		if err := ctx.Translator.SetLanguage(requested); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: language switched to", requested)
		fmt.Println(ctx.Translator.T("language.set.confirm", requested))
		return nil
	})
}
