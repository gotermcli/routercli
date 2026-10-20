// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// historySizeMin and historySizeMax bound "terminal history size <n>".
//
// Cisco IOS accepts <0-256> for the same command, because its history is a
// small in-memory ring buffer for the current session. RouterCLI's history
// is a persistent file appended to as each line is submitted, shared
// across sessions, so a wider range is right here.
//
// The range comfortably contains the 500 line default. The departure from
// Cisco is intended, not an oversight.
const (
	historySizeMin = 0
	historySizeMax = 1000
)

// init - This registers "terminal history size <n>" for config mode.
//
// It sets AppContext.HistorySize, session scoped and never persisted, the
// same as "terminal length" and "terminal width". It governs how many
// lines "show history" prints.
//
// Zero empties what "show history" reports, the same "zero means none"
// convention "terminal length 0" follows. The file on disk is untouched
// either way.
//
// It has no effect on Up and Down arrow recall. That limit is fixed when
// the session's readline instance is built and is never reassigned.
// Reassigning it is a real data race, not a theoretical one: readline
// reads that field from an unsynchronized background goroutine for the
// life of the instance, and "go test -race" has caught it.
func init() {
	command.Register("terminal.history.size", func(ctx *command.AppContext, args []string) error {
		n, err := parseTerminalGeometry(ctx, args[0], historySizeMin, historySizeMax)
		if err != nil {
			return err
		}
		ctx.HistorySize = &n
		ctx.Logger.Debugln("DEBUG: terminal history size set to", n)
		fmt.Println(ctx.Translator.T("terminal.confirm", "history size", n))
		return nil
	})
}
