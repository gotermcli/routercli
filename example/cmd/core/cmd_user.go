// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// init - This function registers "user", which enters the user Command
// Level.
//
// This is a nested mode reachable directly from the base level, using the
// same stack push cmd_configure.go and cmd_interface.go use, not a root
// swap level such as exec or diagnostic. The tree and prompt suffix come
// from the loaded tree structure rather than being hardcoded here.
//
// Beyond the parent check every Command Level entry performs, this one
// requires that the session already be logged in. Every command reachable
// only from inside this level acts on the current session's entry in the
// user database, which only means something once a session knows who it
// is.
//
// That requirement is waived while ctx.ReplayingStartupConfig is true, the
// same trust a boot time replay already extends to every other Command
// Level's password gate. A saved startup-config can only contain a "user"
// block wrapping a runtime defined alias, never a live action such as
// "totp enable" or "password change", so the waiver grants no more than
// replaying that alias back in.
//
// The trust behind it is the same one boot time replay rests on: the
// process was already allowed to run, and to read this file, by the
// operating system. A boot time replay happens before any session has
// logged in at all.
func init() {
	command.Register("user", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["user"]
		if err := command.RequireCurrentCommandLevel(ctx, "user", level.Parent); err != nil {
			return err
		}
		if !ctx.ReplayingStartupConfig {
			if err := requireLoggedIn(ctx); err != nil {
				return err
			}
		}
		username := "<none, replaying startup-config>"
		if ctx.Session != nil {
			username = ctx.Session.Username
		}
		ctx.Logger.Debugln("DEBUG: entering user mode for user", username)
		ctx.Position.Push(command.CommandLevelFrame{
			Name:         "user",
			PromptSuffix: level.PromptSuffix,
			Tree:         level.Tree,
		})
		return nil
	})
}

// requireLoggedIn - This function returns an error unless ctx.Session is
// non-nil and Authenticated.
//
// It is shared by "user" and by every command reachable only from inside
// that level, since all of them act on the current session's identity and
// must refuse for a session that never logged in.
//
// Authenticated is only ever true once a real username and password have
// been verified, which happens only when AuthRequired is set. Checking it
// alone therefore enforces both conditions, since nothing else sets it.
func requireLoggedIn(ctx *command.AppContext) error {
	if ctx.Session == nil || !ctx.Session.Authenticated {
		return fmt.Errorf("%s", ctx.Translator.T("user.login_required"))
	}
	return nil
}
