// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// Client - The daemon owns state that is shared across sessions. Handlers
// need one way to reach it that works whether or not a daemon is running.
type Client interface {
	// MutateProductState runs fn against the current ProductState and
	// returns whatever fn itself returned.
	MutateProductState(fn func(any) (any, error)) (any, error)

	// MutateLevels runs fn against the current *command.TreeStructure and
	// returns whatever fn itself returned.
	MutateLevels(fn func(*command.TreeStructure) (any, error)) (any, error)

	// MutateUsers runs fn against the current auth.Users and returns
	// whatever fn itself returned.
	MutateUsers(fn func(auth.Users) (any, error)) (any, error)

	// MutateRoles runs fn against the current *command.RoleSet and returns
	// whatever fn itself returned.
	MutateRoles(fn func(*command.RoleSet) (any, error)) (any, error)

	// ReplaceState replaces the canonical state wholesale with newState.
	// That is what a reboot means with a daemon running: reread the files
	// from disk and replace outright, not merge field by field.
	//
	// Loading newState is the caller's job; this performs only the
	// replacement.
	//
	// It is named differently from command.DaemonClient's Reboot, a bare
	// trigger, because Go does not allow one type to declare two methods
	// sharing a name with different signatures, and StandaloneClient
	// satisfies both interfaces at once.
	ReplaceState(newState State) error

	// Close releases whatever resources this Client holds; a
	// StandaloneClient closes its own private Store. A caller should call
	// Close exactly once, when a session ends.
	Close() error
}

// StandaloneClient - This type is the Client a CLI process uses when no
// daemon is configured. State lives only in this process, for the life of
// this connection.
//
// It wraps a private Store, reusing this package's single writer safety
// even though nothing outside this process touches it, rather than
// inventing a second unsynchronized path for the no-daemon case.
//
// A zero StandaloneClient is not ready to use; construct one with
// NewStandaloneClient.
type StandaloneClient struct {
	store *Store[State]
}

// NewStandaloneClient returns a ready to use StandaloneClient, its own
// private Store already running, starting from initial as the current
// state.
func NewStandaloneClient(initial State) *StandaloneClient {
	return &StandaloneClient{store: NewStore(initial)}
}

// MutateProductState implements Client.
func (c *StandaloneClient) MutateProductState(fn func(any) (any, error)) (any, error) {
	return c.store.Do(func(s *State) (any, error) {
		return fn(s.ProductState)
	})
}

// MutateLevels implements Client.
func (c *StandaloneClient) MutateLevels(fn func(*command.TreeStructure) (any, error)) (any, error) {
	return c.store.Do(func(s *State) (any, error) {
		return fn(s.Levels)
	})
}

// MutateUsers implements Client.
func (c *StandaloneClient) MutateUsers(fn func(auth.Users) (any, error)) (any, error) {
	return c.store.Do(func(s *State) (any, error) {
		return fn(s.Users)
	})
}

// MutateRoles implements Client.
func (c *StandaloneClient) MutateRoles(fn func(*command.RoleSet) (any, error)) (any, error) {
	return c.store.Do(func(s *State) (any, error) {
		return fn(s.Roles)
	})
}

// ReplaceState implements Client.
func (c *StandaloneClient) ReplaceState(newState State) error {
	_, err := c.store.Do(func(s *State) (any, error) {
		*s = newState
		return nil, nil
	})
	return err
}

// Close implements Client.
func (c *StandaloneClient) Close() error {
	c.store.Close()
	return nil
}

// ListUsers implements command.DaemonClient. A CLI process running
// standalone has no session registry at all, only ever knowing about its
// own one connection, so this always reports
// command.ErrDaemonNotConfigured; see that error's doc comment for why a
// real deployment never reaches this in practice.
func (c *StandaloneClient) ListUsers() ([]command.SessionInfo, error) {
	return nil, command.ErrDaemonNotConfigured
}

// DisconnectUser implements command.DaemonClient, always reporting
// command.ErrDaemonNotConfigured for the same reason ListUsers does.
func (c *StandaloneClient) DisconnectUser(username, sessionID string) error {
	return command.ErrDaemonNotConfigured
}

// Reboot implements command.DaemonClient, always reporting
// command.ErrDaemonNotConfigured; standalone mode's reboot behavior,
// rereading files and ending this one connection directly, runs entirely
// inside example/cmd/core/cmd_admin.go instead, falling back to exactly that the
// moment this method reports this same error. This is a different method
// from ReplaceState above, see Client's doc comment on this file for why
// the two could not share one name.
func (c *StandaloneClient) Reboot() error {
	return command.ErrDaemonNotConfigured
}

// FarewellChannel implements command.DaemonClient. Standalone mode has no
// daemon that could ever push a farewell, so this always returns nil, a
// channel a select statement never chooses, matching this method's doc
// comment in command/daemonclient.go.
func (c *StandaloneClient) FarewellChannel() <-chan string {
	return nil
}

var _ Client = (*StandaloneClient)(nil)

// StandaloneClient also satisfies command.DaemonClient, the narrower,
// Reboot- and Close-free interface AppContext.DaemonClient holds, declared
// in package command itself rather than here since package command cannot
// import package daemon back without a cycle; see command.DaemonClient's
// doc comment in command/daemonclient.go for the full reasoning. Go's
// interface satisfaction is structural, so this assertion needs no new
// code on StandaloneClient itself, only this line confirming the match at
// compile time.
var _ command.DaemonClient = (*StandaloneClient)(nil)
