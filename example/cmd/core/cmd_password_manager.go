// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"
	"os"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// init - This function registers "password manager" and
// "password manager hash" for config mode. Both set the PasswordHash on
// whichever Command Level the session is inside.
//
// "password manager" always prompts for the secret rather than taking it
// on the same line. A same line argument is recorded verbatim by both the
// audit log and readline history, which would leave the plaintext on
// disk. No secret in RouterCLI is ever a same line argument.
//
// "password manager hash <hash>" takes an already hashed value in
// "$id$encoded" form and never sees a plaintext password. It exists so a
// saved configuration can be pasted back in and restore the same access,
// without anyone needing to know or type the real password.
//
// A hash typed this way is never proof of knowing the password. It only
// records what the stored secret equals. Treating it as proof would be a
// pass the hash weakness.
//
// "no password manager" clears the secret. "password manager hash" is not
// itself negatable, since that already covers clearing either form.
//
// Neither form is allowed once a level carries a
// VendorDefinedPasswordHash. Refusing outright matters: letting the
// command appear to succeed against a hash that is then ignored would let
// a user believe they had changed the level's access.
func init() {
	command.Register("password-manager", func(ctx *command.AppContext, args []string) error {
		level, err := currentUserSettableLevel(ctx)
		if err != nil {
			return err
		}
		levelName := level.Name

		if ctx.Negated {
			if _, err := ctx.DaemonClient.MutateLevels(func(levels *command.TreeStructure) (any, error) {
				levels.ByName[levelName].PasswordHash = ""
				return nil, nil
			}); err != nil {
				return err
			}
			ctx.Logger.Debugln("DEBUG: password cleared for Command Level", levelName)
			fmt.Println(ctx.Translator.T("password_manager.cleared"))
			return nil
		}

		password, err := auth.PromptSecret(os.Stdout, int(os.Stdin.Fd()), ctx.Translator)
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if _, err := ctx.DaemonClient.MutateLevels(func(levels *command.TreeStructure) (any, error) {
			levels.ByName[levelName].PasswordHash = hash
			return nil, nil
		}); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: password set for Command Level", levelName)
		fmt.Println(ctx.Translator.T("password_manager.confirm"))
		return nil
	})

	command.Register("password-manager.hash", func(ctx *command.AppContext, args []string) error {
		level, err := currentUserSettableLevel(ctx)
		if err != nil {
			return err
		}
		levelName := level.Name

		hash := args[0]
		if !auth.IsRecognizedHash(hash) {
			return fmt.Errorf("%s", ctx.Translator.T("password_manager.hash.not_recognized"))
		}

		if _, err := ctx.DaemonClient.MutateLevels(func(levels *command.TreeStructure) (any, error) {
			levels.ByName[levelName].PasswordHash = hash
			return nil, nil
		}); err != nil {
			return err
		}
		ctx.Logger.Debugln("DEBUG: password hash set directly for Command Level", levelName)
		fmt.Println(ctx.Translator.T("password_manager.confirm"))
		return nil
	})
}

// currentUserSettableLevel - This function is the lookup and permission
// check "password manager" and "password manager hash" both start with:
// resolve ctx.Session.CommandLevel to its real *command.CommandLevel, then
// refuse before doing anything else if that level's UserSettablePassword
// reports false. Factored out once here rather than duplicated in both
// handlers above, the same reasoning cmd_totp.go's shared helper between
// "totp enable" and "totp enable qr" follows.
func currentUserSettableLevel(ctx *command.AppContext) (*command.CommandLevel, error) {
	level, ok := ctx.Levels.ByName[ctx.Session.CommandLevel]
	if !ok {
		// Session.CommandLevel is always kept in sync with an actual,
		// currently loaded CommandLevel by whichever cmd/cmd_*.go file
		// last called command.EnterCommandLevel or ExitCommandLevel, so
		// this should not happen. Checked rather than assumed.
		return nil, fmt.Errorf("%s", ctx.Translator.T("password_manager.no_current_level"))
	}
	if !level.UserSettablePassword() {
		return nil, fmt.Errorf("%s", ctx.Translator.T("password_manager.not_user_settable"))
	}
	return level, nil
}
