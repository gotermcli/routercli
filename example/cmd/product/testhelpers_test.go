// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/daemon"
)

// newTestContext - This function builds a minimal *command.AppContext
// suitable for exercising a single command handler directly, without
// needing readline, a real login session, or any of main.go's other
// startup machinery. An implementer writing their own command handler
// tests can copy this same pattern for their own handler tests.
//
// This is the same shape as cmd/core's newTestContext, one independent
// copy per package rather than a shared export, since the two disagree on
// what State should hold, State here, nil in package core, and package
// product cannot export a test-only helper for package core to import
// without pulling a test file into a non-test build.
//
// DaemonClient wraps the exact same *State pointer State itself holds,
// daemon.NewStandaloneClient given no Levels, Users, or Roles of its own,
// empty here since a plain newTestContext, on its own, never reaches them
// through DaemonClient. Sharing the one pointer between State and
// DaemonClient's wrapped State.State keeps a test free to read or set
// State.(*State) fields directly, the pre-migration pattern most of this
// package's tests still use, while a handler already migrated to
// ctx.DaemonClient.MutateProductState, cmd_hostname.go's "hostname" for
// instance, observes and mutates that identical object.
//
// A test that goes on to assign a real ctx.Levels of its own, then replays
// saved configuration text through command.ReplayLines rather than only
// rendering it, can reach a Levels-mutating handler this package never
// registers itself, cmd/core's "alias" among them, through this file's
// blank import of package core. Such a test must call rewireDaemonClient
// below after assigning ctx.Levels, the same way cmd/core's tests do for
// the handlers this package's tests exercise the mirror image of; see that
// function's doc comment.
func newTestContext() *command.AppContext {
	state := &State{}
	return &command.AppContext{
		State:        state,
		DaemonClient: daemon.NewStandaloneClient(daemon.NewState(state, nil, nil, nil, nil)),
		Logger:       log.New(io.Discard, "", 0),
	}
}

// rewireDaemonClient - This function replaces ctx.DaemonClient with a
// fresh daemon.NewStandaloneClient whose own Store shares the exact same
// ctx.State, ctx.Levels, ctx.Users, and ctx.Roles a test has already
// assigned, rather than the disconnected copy newTestContext started out
// holding. See cmd/core's identically named, identically documented helper
// in that package's testhelpers_test.go for the full reasoning; this is
// that same helper, duplicated here rather than shared, for the same
// reason newTestContext itself is duplicated rather than exported: package
// product and package core stay independent siblings, neither importing
// the other.
func rewireDaemonClient(ctx *command.AppContext) {
	ctx.DaemonClient = daemon.NewStandaloneClient(daemon.NewState(ctx.State, ctx.Levels, ctx.Users, ctx.Roles, nil))
}

// loadTestCommand - This function resolves handlerName into its
// *command.Command by writing a throwaway, one command tree file and
// loading it through command.LoadTree, the same loader main.go uses at
// startup. This is not a direct call into the handler closure itself,
// since command.Register stores it only in an unexported registry inside
// package command, reachable from outside that package through this same
// "run:" resolution path a real tree file uses. handlerName must already
// be registered, which every cmd_*.go file in this package's init() does
// automatically once this package is imported, exactly as it is here.
func loadTestCommand(t *testing.T, handlerName string) *command.Command {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tree.yaml")
	body := "commands:\n  test:\n    run: " + handlerName + "\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("failed to write test tree file: %v", err)
	}
	tree, err := command.LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned error: %v", err)
	}
	return tree["test"]
}
