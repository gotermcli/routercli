// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"errors"
	"time"

	"github.com/gotermcli/routercli/auth"
)

// ErrDaemonNotConfigured is returned by ListUsers, DisconnectUser, and
// Reboot when nothing backing AppContext.DaemonClient has a real daemon on
// the other end.
//
// A deployment with no daemon never reaches it in practice: show.users and
// disconnect.user are pruned out of the tree entirely, and the reboot
// handler checks for this specific error to fall back to local behavior
// rather than surfacing it.
//
// It still exists, and StandaloneClient still returns it, as defense in
// depth. A bug in that pruning or that fallback then fails with a clear,
// named error rather than a nil pointer dereference or a silent no-op.
var ErrDaemonNotConfigured = errors.New("command: no daemon is configured for this deployment")

// SessionInfo - This type is everything "show users" reports about one
// attached session: a daemon assigned session ID, the username it logged
// in as, the Command Level it was last seen at, when it connected, and how
// long it has been idle.
//
// It is declared here rather than reusing daemon.SessionInfo, for the same
// import cycle reason DaemonClient is. Both the standalone and any remote
// implementation already import this package, so both can build one from
// their own richer session type.
type SessionInfo struct {
	ID           string
	Username     string
	CommandLevel string
	ConnectedAt  time.Time
	IdleFor      time.Duration
}

// DaemonClient - State that is shared across sessions cannot be read and
// written directly once a daemon owns it. Handlers need one way to reach
// that state that works whether or not a daemon is running.
//
// This is that interface. A handler calls into it instead of touching
// AppContext.State or AppContext.Levels.
//
// Each Mutate method takes a function to run against the current value and
// returns whatever that function returns, mirroring daemon.Store.Do. The
// remaining methods, ListUsers, DisconnectUser, Reboot, and
// FarewellChannel, take no closure, so each is a plain request a remote
// implementation can send across a socket.
//
// It is declared here rather than in package daemon because that package
// already imports this one, and the reverse would be an import cycle. Go
// satisfies interfaces structurally, so the daemon types match without
// importing this at all.
//
// Close is absent on purpose. Process lifecycle belongs to whatever
// constructs the client, never to anything reachable through AppContext.
type DaemonClient interface {
	// MutateProductState runs fn against the current State, whatever a
	// project built on this framework stores there, ProductState in this
	// project's shipped example, and returns whatever fn itself returned.
	MutateProductState(fn func(any) (any, error)) (any, error)

	// MutateLevels runs fn against the current *TreeStructure and returns
	// whatever fn itself returned.
	MutateLevels(fn func(*TreeStructure) (any, error)) (any, error)

	// MutateUsers runs fn against the current auth.Users and returns
	// whatever fn itself returned.
	MutateUsers(fn func(auth.Users) (any, error)) (any, error)

	// MutateRoles runs fn against the current *RoleSet and returns
	// whatever fn itself returned.
	MutateRoles(fn func(*RoleSet) (any, error)) (any, error)

	// ListUsers returns one SessionInfo per currently attached session,
	// everything "show users" prints, or ErrDaemonNotConfigured when no
	// real daemon backs this DaemonClient.
	ListUsers() ([]SessionInfo, error)

	// DisconnectUser asks the daemon to end one session belonging to
	// username. sessionID empty means "the one session belonging to
	// username", refusing with an error listing the candidates if more
	// than one matches; sessionID non-empty names one exactly, the same
	// short identifier ListUsers already reports per session, so a person
	// can always resolve the ambiguity by looking there first. Returns
	// ErrDaemonNotConfigured when no real daemon backs this DaemonClient.
	DisconnectUser(username, sessionID string) error

	// Reboot asks the daemon to reread its own canonical state from disk
	// and end every attached session, including this one, with a "Device
	// is rebooting" farewell; see this interface's doc comment above for
	// why this takes no newState of its own. A nil return means the daemon
	// accepted the request and this session's ending now arrives
	// asynchronously, through FarewellChannel, not through this call
	// returning; a non-nil return, including ErrDaemonNotConfigured, means
	// no reboot was triggered at all.
	Reboot() error

	// FarewellChannel returns the channel a session receives exactly one
	// push on, the human readable reason text, the moment the daemon ends
	// this connection on purpose, DisconnectUser or Reboot elsewhere
	// having targeted it, or the connection to the daemon is lost with no
	// explicit farewell at all. A DaemonClient with no real daemon behind
	// it, StandaloneClient among them, returns a nil channel here, which a
	// select statement never chooses, the same nil channel convention
	// AppContext.ReloadScheduler's FireChannel already establishes for
	// "not wired up in this context."
	FarewellChannel() <-chan string
}
