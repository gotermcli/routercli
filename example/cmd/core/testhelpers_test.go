// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/daemon"
)

// newTestContext - This function builds a minimal *command.AppContext
// suitable for exercising a single command handler directly, without
// needing readline, a real login session, or any of main.go's other
// startup machinery. State is left nil, since nothing registered by this
// package touches ctx.State; a project's application state, see
// example/cmd/product/model.go's State for a working example, has no place
// in package core. An implementer writing their own command handler tests
// can copy this same pattern, setting State to whatever their own project
// needs.
//
// DaemonClient is a plain daemon.NewStandaloneClient, no daemon
// configured, the same default main.go itself falls back to whenever
// config.SystemConfig.DaemonSocketPath is empty; every one of its
// ListUsers, DisconnectUser, and Reboot methods returns
// command.ErrDaemonNotConfigured, matching a real standalone deployment
// exactly, so a handler that already branches on that error, runReboot in
// cmd_admin.go for instance, exercises its own standalone path by default
// here with no special test setup of its own needed. A test exercising the
// daemon-configured path builds its own fakeDaemonClient instead, see
// testhelpers_test.go's fakeDaemonClient, and assigns it to
// ctx.DaemonClient directly.
//
// This is the same shape as cmd/product's newTestContext, one independent
// copy per package rather than a shared export, since the two disagree on
// what State should hold and package core cannot import package product,
// an implementation specific package, without inverting the dependency
// this framework is built around.
func newTestContext() *command.AppContext {
	return &command.AppContext{
		DaemonClient: daemon.NewStandaloneClient(daemon.NewState(nil, nil, nil, nil, nil)),
		Logger:       log.New(io.Discard, "", 0),
	}
}

// rewireDaemonClient - This function replaces ctx.DaemonClient with a
// fresh daemon.NewStandaloneClient whose own Store shares the exact same
// ctx.State, ctx.Levels, ctx.Users, and ctx.Roles a test has already
// assigned, rather than the disconnected, entirely separate copy
// newTestContext's plain daemon.NewStandaloneClient started out holding,
// daemon.NewState(nil, nil, nil, nil, nil). Several handlers in this
// package, cmd_admin.go's account management commands, cmd_password.go's
// finishPasswordChange, cmd_totp.go's finishTOTPEnable and
// finishTOTPDisable, cmd_alias.go's "alias", and cmd_password_manager.go's
// "password manager" among them, now reach shared state exclusively
// through ctx.DaemonClient.MutateUsers or MutateLevels, never through
// ctx.Users or ctx.Levels directly. A test exercising one of those
// handlers must call this, after assigning the real Users, Levels, or
// both, a test cares about, so that closure sees the same map or struct
// the test itself asserts against afterward, rather than a nil or empty
// one of the Store's. See cmd_hostname.go's doc comment, in cmd/product,
// for the migration this follows, and cmd/product's testhelpers_test.go,
// whose newTestContext already wires State and DaemonClient together from
// construction, for the equivalent already in place there.
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

// fakeDaemonClient is a command.DaemonClient test double a test builds
// directly, sets whichever of its four exported *Err or *Result fields it
// cares about, and assigns to ctx.DaemonClient in place of the plain
// daemon.NewStandaloneClient newTestContext already provides, to exercise
// a handler's daemon-configured path, one where a real daemon is answering
// rather than command.ErrDaemonNotConfigured. The three Mutate methods are
// left unimplemented, panicking if called, since nothing in this package's
// tests today reaches them through a fakeDaemonClient; a future test that
// needs one adds it then, rather than this type guessing at a shape
// nothing yet exercises.
type fakeDaemonClient struct {
	// RebootErr is returned by Reboot; RebootCalls counts how many times
	// it was called, so a test can confirm a handler asked the daemon to
	// reboot without also asserting on a session-ending side effect that
	// only a real daemon connection ever produces.
	RebootErr   error
	RebootCalls int

	// ListUsersResult and ListUsersErr are returned by ListUsers together,
	// in that order.
	ListUsersResult []command.SessionInfo
	ListUsersErr    error

	// DisconnectUserErr is returned by DisconnectUser.
	// DisconnectUserUsername and DisconnectUserSessionID record its two
	// arguments from the most recent call, so a test can confirm exactly
	// what a handler asked the daemon to disconnect.
	DisconnectUserErr       error
	DisconnectUserUsername  string
	DisconnectUserSessionID string

	// FarewellCh is returned by FarewellChannel as is, nil by default, the
	// same "not wired up" convention a real StandaloneClient already
	// follows.
	FarewellCh chan string
}

func (f *fakeDaemonClient) MutateProductState(fn func(any) (any, error)) (any, error) {
	panic("fakeDaemonClient: MutateProductState not implemented, no test needs it yet")
}

func (f *fakeDaemonClient) MutateLevels(fn func(*command.TreeStructure) (any, error)) (any, error) {
	panic("fakeDaemonClient: MutateLevels not implemented, no test needs it yet")
}

func (f *fakeDaemonClient) MutateUsers(fn func(auth.Users) (any, error)) (any, error) {
	panic("fakeDaemonClient: MutateUsers not implemented, no test needs it yet")
}

func (f *fakeDaemonClient) MutateRoles(fn func(*command.RoleSet) (any, error)) (any, error) {
	panic("fakeDaemonClient: MutateRoles not implemented, no test needs it yet")
}

func (f *fakeDaemonClient) ListUsers() ([]command.SessionInfo, error) {
	return f.ListUsersResult, f.ListUsersErr
}

func (f *fakeDaemonClient) DisconnectUser(username, sessionID string) error {
	f.DisconnectUserUsername = username
	f.DisconnectUserSessionID = sessionID
	return f.DisconnectUserErr
}

func (f *fakeDaemonClient) Reboot() error {
	f.RebootCalls++
	return f.RebootErr
}

func (f *fakeDaemonClient) FarewellChannel() <-chan string {
	return f.FarewellCh
}
