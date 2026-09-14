// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotermcli/routercli/paging"
	"github.com/gotermcli/routercli/tokenize"
)

// ReplayLines - This function runs each line through the same tokenize,
// resolve, validate, run sequence a live session uses, against whatever
// Command Level the session is currently in. It advances Position and
// Session.CommandLevel exactly as typing those lines would.
//
// An empty line, or a Cisco style "!" comment, is skipped.
//
// The first line that fails to tokenize, resolve, validate, or run stops
// replay and returns that error wrapped with the offending line. A caller
// applying a whole block of configuration needs to know it did not fully
// apply, rather than ending up silently half applied.
//
// trusted controls whether a password protected Command Level along the
// way is waved through. ReplayLines never prompts either way; the
// difference is handled inside EnterCommandLevel. When trusted is true,
// ReplayingStartupConfig is set for the duration of the call and always
// reset through a defer.
//
// An individual command's own PasswordHash gate is never enforced here,
// whatever trusted says. That is a scope boundary. This function replays
// configuration text, which by construction holds only level navigation
// and state setting lines, never a command carrying its own password.
// Feeding it arbitrary interactive commands is outside what it is for.
func ReplayLines(ctx *AppContext, lines []string, trusted bool) error {
	if trusted {
		ctx.ReplayingStartupConfig = true
		defer func() { ctx.ReplayingStartupConfig = false }()
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}

		tokens, terr := tokenize.Tokenize(line)
		if terr != nil {
			return fmt.Errorf("replaying %q: %w", line, terr)
		}

		res := Resolve(ctx.Position.Current().Tree, tokens)
		if res.Command == nil || res.Command.RunFunc == nil {
			return fmt.Errorf("replaying %q: did not resolve to a runnable command at Command Level %q", line, ctx.Position.Current().Name)
		}
		if !res.Negated {
			if verr := ValidateArgs(res.Command, res.Args); verr != nil {
				return fmt.Errorf("replaying %q: %w", line, verr)
			}
		}

		ctx.Negated = res.Negated
		runErr := res.Command.RunFunc(ctx, res.Args)
		ctx.Negated = false
		if runErr != nil {
			return fmt.Errorf("replaying %q: %w", line, runErr)
		}
	}
	return nil
}

// LoadStartupConfig - This function reads a saved startup-config and
// replays it into ctx.State, the same way pasting the text in would. A
// path that does not exist is not an error.
//
// Replay runs trusted. Nothing has typed a password yet, so no credential
// check is bypassed, only waived.
//
// Two callers reach this: startup, before any session exists, and
// "reboot", which re-reads every persistent file before ending the
// connection. Nothing in a dying session reads the result, but
// re-validating still catches a startup-config a hand edit broke.
//
// Position and Session.CommandLevel are reset to the base level both
// before and after replay.
//
// The reset before matters because a startup-config begins with "enable"
// and walks down from there, so replay has to start at base. "reboot" can
// call this from any level.
//
// The reset after matters because replaying "enable" and
// "configure terminal" leaves Position deep inside whatever levels the
// configuration touched, which is not where a session should be left.
//
// Only navigation state is undone. Every mutation replay made stays.
//
// The whole replay runs inside paging.CaptureOutput, so the confirmation
// text each handler prints never reaches a terminal nobody asked. Each
// captured line is logged at debug level instead.
func LoadStartupConfig(ctx *AppContext, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	base := ctx.Levels.Base()
	resetToBase := func() {
		ctx.Position = NewCommandLevelStack(base.Name, base.PromptSuffix, base.Tree)
		ctx.Session.CommandLevel = base.Name
	}
	resetToBase()
	defer resetToBase()

	lines := strings.Split(string(data), "\n")
	var replayErr error
	captured, cerr := paging.CaptureOutput(func() {
		replayErr = ReplayLines(ctx, lines, true)
	})
	if cerr != nil {
		return cerr
	}
	for _, line := range captured {
		ctx.Logger.Debugln("DEBUG: startup-config replay:", line)
	}
	return replayErr
}
