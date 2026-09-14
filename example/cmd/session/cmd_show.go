// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package session

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/paging"
)

func init() {
	command.Register("show.terminal", func(ctx *command.AppContext, args []string) error {
		for _, line := range terminalStatusLines(ctx) {
			fmt.Println(line)
		}
		return nil
	})

	command.Register("show.history", func(ctx *command.AppContext, args []string) error {
		lines, err := historyLines(ctx)
		if err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("show.history.read_failed", err))
		}
		if len(lines) == 0 {
			fmt.Println(ctx.Translator.T("show.history.empty"))
			return nil
		}
		for _, line := range lines {
			fmt.Println(line)
		}
		return nil
	})
}

// terminalStatusLines - This function builds "show terminal" output:
// Length and Width on one line, then whether the pager is active for this
// session and for the deployment.
//
// That two tier split matches HP's "(session)" and "(global)" reporting,
// and RouterCLI already keeps the same distinction.
//
// Length and Width both report the live resolved values, detected from the
// real terminal when nothing has been typed this session, rather than
// whatever override happens to be set.
//
// Filter mode is a RouterCLI addition beyond what Cisco or HP report here.
// It is included because it directly changes how "| include", "| exclude",
// and "| begin" behave for the rest of the session.
func terminalStatusLines(ctx *command.AppContext) []string {
	length := paging.EffectivePageLines(int(os.Stdin.Fd()), ctx.PageLines, ctx.DefaultPageLines)
	width := paging.EffectiveTerminalWidth(int(os.Stdin.Fd()), ctx.TerminalWidth, ctx.DefaultTerminalWidth)

	sessionPaging := ctx.Translator.T("show.terminal.enabled")
	if ctx.PageLines != nil && *ctx.PageLines == 0 {
		sessionPaging = ctx.Translator.T("show.terminal.disabled")
	}
	globalPaging := ctx.Translator.T("show.terminal.disabled")
	if ctx.PagingEnabled {
		globalPaging = ctx.Translator.T("show.terminal.enabled")
	}

	filterMode := ctx.Translator.T("show.terminal.filter_mode_substring")
	if ctx.FilterMode == paging.FilterModeRegex {
		filterMode = ctx.Translator.T("show.terminal.filter_mode_regex")
	}

	return []string{
		ctx.Translator.T("show.terminal.geometry_line", length, width),
		ctx.Translator.T("show.terminal.paging_line", sessionPaging, globalPaging),
		ctx.Translator.T("show.terminal.filter_mode_line", filterMode),
	}
}

// historyLines - This function reads the history file fresh from disk and
// returns its last EffectiveHistorySize lines, oldest first, the order
// Cisco and HP print in.
//
// Readline appends each submitted line to that file immediately, so
// reading it here always reflects what this session, and any earlier one
// sharing the file, has typed. Keeping a second in-memory copy would only
// give something to drift.
//
// A missing or empty file returns an empty slice rather than an error, the
// same as "show startup-config" for its own file.
//
// An EffectiveHistorySize of zero returns empty without reading the file
// at all, since nothing would be shown either way.
func historyLines(ctx *command.AppContext) ([]string, error) {
	size := command.EffectiveHistorySize(ctx)
	if size <= 0 {
		return nil, nil
	}

	data, err := os.ReadFile(ctx.HistoryFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil, nil
	}

	all := strings.Split(trimmed, "\n")
	if len(all) > size {
		all = all[len(all)-size:]
	}
	return all, nil
}
