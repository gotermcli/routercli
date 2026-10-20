// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"github.com/gotermcli/routercli/command"
)

// ConnectedClient - Session tracking works across a real socket today.
// Product state, levels, users, and roles do not yet, because no handler
// beyond hostname has been reshaped into a wire compatible request.
//
// This is the command.DaemonClient used when DaemonSocketPath is set. It
// embeds a *StandaloneClient for the four Mutate methods and a
// *RemoteClient for ListUsers, DisconnectUser, Reboot, and
// FarewellChannel, so the two halves together satisfy the interface.
//
// So with a daemon configured, session and connection tracking are shared,
// while the rest still lives only in this process. That is a disclosed
// limitation, not an oversight.
//
// Reshaping a handler moves it from the embedded StandaloneClient to the
// RemoteClient one at a time. It does not replace this type.
type ConnectedClient struct {
	*StandaloneClient
	*RemoteClient
}

// NewConnectedClient combines an already constructed StandaloneClient and
// RemoteClient into one command.DaemonClient. Neither is constructed here;
// a caller builds each its own way, a StandaloneClient from whatever local
// state this CLI process loaded at its own startup exactly as it always
// has, a RemoteClient from an already dialed and handshaken connection to
// the real daemon, see NewRemoteClient, then hands both to this function
// once, the same division main.go's startup sequence already makes between
// loading local state and wiring a Client around it.
func NewConnectedClient(standalone *StandaloneClient, remote *RemoteClient) *ConnectedClient {
	return &ConnectedClient{StandaloneClient: standalone, RemoteClient: remote}
}

// Close - This method closes both halves: the embedded StandaloneClient's
// Store and the embedded RemoteClient's connection.
//
// This method, and the four below it, are declared explicitly rather than
// left to embedding promotion. Both embedded types have methods with these
// exact names at the same depth, which Go treats as an ambiguous selector
// and does not promote at all, so each needs its own forwarding method.
func (c *ConnectedClient) Close() error {
	standaloneErr := c.StandaloneClient.Close()
	remoteErr := c.RemoteClient.Close()
	if standaloneErr != nil {
		return standaloneErr
	}
	return remoteErr
}

// ListUsers forwards to the embedded RemoteClient, the real, wire
// connected implementation; see this type's doc comment on Close for why
// this cannot be left to Go's embedding promotion.
func (c *ConnectedClient) ListUsers() ([]command.SessionInfo, error) {
	return c.RemoteClient.ListUsers()
}

// DisconnectUser forwards to the embedded RemoteClient; see ListUsers.
func (c *ConnectedClient) DisconnectUser(username, sessionID string) error {
	return c.RemoteClient.DisconnectUser(username, sessionID)
}

// Reboot forwards to the embedded RemoteClient; see ListUsers.
func (c *ConnectedClient) Reboot() error {
	return c.RemoteClient.Reboot()
}

// FarewellChannel forwards to the embedded RemoteClient; see ListUsers.
func (c *ConnectedClient) FarewellChannel() <-chan string {
	return c.RemoteClient.FarewellChannel()
}

var _ command.DaemonClient = (*ConnectedClient)(nil)
