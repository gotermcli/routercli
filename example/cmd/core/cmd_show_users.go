// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// init registers "show users", the last of the daemon aware commands the
// daemon supports: every currently attached session, real daemon required,
// see example/var/tree/level_exec.yaml's "requires: daemon" on this command and
// main.go's featureFlags, which prunes this command out of the tree
// entirely for a deployment with no daemon configured, so
// ctx.DaemonClient.ListUsers reporting command.ErrDaemonNotConfigured here
// is defense in depth, not a path an ordinary deployment ever reaches.
func init() {
	command.Register("show.users", func(ctx *command.AppContext, args []string) error {
		sessions, err := ctx.DaemonClient.ListUsers()
		if err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("show.users.failed", err))
		}
		for _, line := range usersLines(ctx, sessions) {
			fmt.Println(line)
		}
		return nil
	})
}

// usersLines - This function builds "show users" output: one header row,
// then one row per attached session, in the order ListUsers returned them,
// oldest connection first.
//
// Every column is padded to the longest value it holds on this call rather
// than to a fixed width.
//
// The five column headers are left as plain English rather than
// translated, matching Cisco and HP, where a "show users" table's headers
// are part of the command's fixed output shape rather than prose.
func usersLines(ctx *command.AppContext, sessions []command.SessionInfo) []string {
	if len(sessions) == 0 {
		return []string{ctx.Translator.T("show.users.empty")}
	}

	const (
		sessionHeader  = "Session"
		usernameHeader = "Username"
		levelHeader    = "Level"
		connectedWidth = len("2006-01-02 15:04:05")
	)
	widthSession, widthUsername, widthLevel := len(sessionHeader), len(usernameHeader), len(levelHeader)
	for _, s := range sessions {
		if len(s.ID) > widthSession {
			widthSession = len(s.ID)
		}
		if len(s.Username) > widthUsername {
			widthUsername = len(s.Username)
		}
		if len(s.CommandLevel) > widthLevel {
			widthLevel = len(s.CommandLevel)
		}
	}

	lines := make([]string, 0, len(sessions)+1)
	lines = append(lines, fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		widthSession, sessionHeader, widthUsername, usernameHeader, widthLevel, levelHeader, connectedWidth, "Connected", "Idle"))
	for _, s := range sessions {
		lines = append(lines, fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
			widthSession, s.ID, widthUsername, s.Username, widthLevel, s.CommandLevel,
			connectedWidth, s.ConnectedAt.Format("2006-01-02 15:04:05"), auth.RoundForDisplay(s.IdleFor)))
	}
	return lines
}
