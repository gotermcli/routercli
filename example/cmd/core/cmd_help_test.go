// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"os"
	"strings"
	"testing"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/paging"
)

// TestHelpHandlerListsCurrentPositionTree - This test verifies that the
// registered "help" handler builds its listing from
// ctx.Position.Current().Tree, the session's current mode, not from
// ctx.Levels or any other tree, by giving it a tree with one known command
// and confirming command.HelpText reflects it in the printed output. This
// is what makes "help" show different output depending on which mode a
// session is currently in.
func TestHelpHandlerListsCurrentPositionTree(t *testing.T) {
	ctx := newTestContext()
	tree := map[string]*command.Command{
		"show-me-in-help": {Desc: "A marker command only this test's tree defines"},
	}
	ctx.Position = command.NewCommandLevelStack("exec", "", tree)
	cmd := loadTestCommand(t, "help")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Errorf("help handler returned unexpected error: %v", runErr)
	}
	if !strings.Contains(out, "show-me-in-help") {
		t.Errorf("help output = %q, expected it to contain %q", out, "show-me-in-help")
	}
}

// TestHelpHandlerWithCommandNameShowsDetailedHelp - This test verifies
// that "help" typed with a command name after it prints exactly what
// command.DetailedHelp itself returns for that same path, a man page style
// description rather than the "what can I type next" answer
// command.HelpForPath, and "<command> ?", still give. The width passed to
// command.DetailedHelp here is computed the same way the real handler
// computes it, through paging.EffectiveTerminalWidth, rather than a
// hardcoded literal, so this test's "want" stays byte for byte in sync
// with the real handler regardless of whether the test process's stdin
// happens to be a real terminal.
func TestHelpHandlerWithCommandNameShowsDetailedHelp(t *testing.T) {
	ctx := newTestContext()
	minArgs := 2
	tree := map[string]*command.Command{
		"greet": {
			Desc:    "Say something to someone",
			ArgHelp: "<name> <greeting>  Say something to someone.",
			MinArgs: &minArgs,
		},
	}
	ctx.Position = command.NewCommandLevelStack("exec", "", tree)
	cmd := loadTestCommand(t, "help")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, []string{"greet"}) })
	if runErr != nil {
		t.Fatalf("help handler returned unexpected error: %v", runErr)
	}
	width := paging.EffectiveTerminalWidth(int(os.Stdin.Fd()), ctx.TerminalWidth, ctx.DefaultTerminalWidth)
	want := command.DetailedHelp(tree, []string{"greet"}, ctx.Translator, ctx.ListOptions, ctx.ProductName, width)
	if out != want {
		t.Errorf("help greet output = %q, want %q (command.DetailedHelp's output)", out, want)
	}
	if !strings.Contains(out, "Say something to someone") {
		t.Errorf("help greet output = %q, expected it to contain the command's description", out)
	}
}

// TestHelpHandlerWithAmbiguousCommandNamePrintsCandidates - This test
// verifies that "help" typed with a partial name matching more than one
// command prints the matching candidate names, through
// command.DetailedHelp, rather than being refused as unknown the way a
// name matching nothing at all is.
func TestHelpHandlerWithAmbiguousCommandNamePrintsCandidates(t *testing.T) {
	ctx := newTestContext()
	tree := map[string]*command.Command{
		"show": {Desc: "Show something"},
		"set":  {Desc: "Set something"},
	}
	ctx.Position = command.NewCommandLevelStack("exec", "", tree)
	cmd := loadTestCommand(t, "help")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, []string{"s"}) })
	if runErr != nil {
		t.Fatalf("help handler returned unexpected error: %v", runErr)
	}
	if !strings.Contains(out, "show") || !strings.Contains(out, "set") {
		t.Errorf("help s output = %q, expected it to list both candidate names", out)
	}
}

// TestHelpHandlerWithUnknownCommandNameReturnsError - This test verifies
// that "help" typed with a command name that resolves to nothing at all is
// refused with an error, rather than printing nothing silently, so a typo
// is visible as a typo instead of an empty, unexplained line.
func TestHelpHandlerWithUnknownCommandNameReturnsError(t *testing.T) {
	ctx := newTestContext()
	tree := map[string]*command.Command{
		"show-me-in-help": {Desc: "A marker command only this test's tree defines"},
	}
	ctx.Position = command.NewCommandLevelStack("exec", "", tree)
	cmd := loadTestCommand(t, "help")

	if err := cmd.RunFunc(ctx, []string{"bogus"}); err == nil {
		t.Fatal("expected an error for a command name that does not resolve to anything, got nil")
	}
}
