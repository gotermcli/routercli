// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package daemon implements the RouterCLI daemon, the persistent process a
deployment runs alongside its CLI clients. Both sides agree on the Unix
domain socket named by DaemonSocketPath in the system configuration.

With a daemon running, every connected session shares one running
configuration, one set of accounts, and one set of roles. Without one,
each session holds its own copy, which drifts as soon as two sessions
change different things.

# Canonical State

Store is the concurrency primitive. One long running goroutine owns a
value of a caller supplied type in ordinary unshared memory, with no
mutex, because nothing outside that goroutine ever touches it. Reads and
mutations arrive as functions submitted through Do and run one at a time
in the order received. This removes a class of race by construction
rather than by locking discipline spread across dozens of mutation
sites.

State is what a real daemon holds. Its field types come from the command
and auth packages rather than being duplicated here. Product state stays
an opaque any, so this package carries none of a deployment's Cisco
or HP flavored concepts.

# Transport and Wire Protocol

Package armorchan provides the encrypted channel. Every connection
performs an X25519 handshake against the daemon's persisted static
identity, then speaks AES-256-GCM for the rest of its life.

Listen and Dial open and connect to the socket. Every accepted
connection is peer credential checked through a PeerCredentialChecker,
most commonly AllowedUIDs.

Each message is a one byte kind tag followed by a JSON payload:

  - Hello and HelloResponse, the registration exchange every connection
    begins with
  - Goodbye, a one way notice that a session is ending normally
  - AuditEvent, one per dispatched command
  - ListUsersRequest and ListUsersResponse
  - DisconnectUserRequest and DisconnectUserResponse
  - RebootRequest and RebootResponse
  - Farewell, pushed to a session immediately before the daemon closes
    its connection

# Daemon Side

SessionDirectory is the session and connection registry, offering
Register, Touch, Unregister, List, Farewell for a targeted disconnect,
and BroadcastFarewell for a reboot or clean shutdown. It is built on
Store.

Server accepts connections and dispatches messages against a real
SessionDirectory, a canonical Store, and an audit log. A privileged
session's reboot request and the daemon's SIGHUP handler both
converge on TriggerReboot.

# Client Side

RemoteClient is one live connection from the CLI side. It forwards every
dispatched command to the daemon as an AuditEvent instead of writing a
local file, and it carries ListUsers, DisconnectUser, Reboot, and
FarewellChannel across the socket.

StandaloneClient is the implementation used when no daemon is
configured, reading and writing an in process Store directly.

ConnectedClient combines the two. Session and connection tracking run
across the socket through RemoteClient, while product state, levels,
users, and roles are still backed by StandaloneClient and still live only
in one CLI process's memory. That is a disclosed limitation, not an
oversight.
*/
package daemon
