// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"crypto/ecdh"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/gotermcli/routercli/armorchan"
)

// socketPermissions is the file mode a real daemon's Unix domain socket is
// created with, restrictive by default, matching this project's transport
// design: "The daemon creates it with restrictive permissions, 0600 by
// default, owned by whatever account the daemon itself runs as." The peer
// credential check in Accept is a second, independent layer above this,
// never a substitute for it.
const socketPermissions = 0o600

// Listener is a RouterCLI daemon's Unix domain socket, opened by Listen,
// every accepted connection checked against a PeerCredentialChecker before
// a single byte of any application protocol above it is read. A zero
// Listener is not ready to use; construct one with Listen.
type Listener struct {
	path     string
	listener *net.UnixListener
	checker  PeerCredentialChecker
}

// Listen - This function opens a Unix domain socket at path with
// permissions restricted to socketPermissions, and returns a Listener
// ready to Accept connections checked against checker.
//
// A stale socket file left by a daemon that exited without cleaning up is
// removed before binding, since net.ListenUnix refuses a path that already
// exists.
//
// Listen does not try to tell a stale file from one a live process is
// using. That is the same limitation an operator has when starting any
// daemon twice against one path.
func Listen(path string, checker PeerCredentialChecker) (*Listener, error) {
	if checker == nil {
		return nil, errors.New("daemon: Listen requires a non-nil PeerCredentialChecker")
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("daemon: removing a stale socket file at %s: %w", path, err)
	}

	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, fmt.Errorf("daemon: resolving socket path %s: %w", path, err)
	}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("daemon: listening on %s: %w", path, err)
	}
	if err := os.Chmod(path, socketPermissions); err != nil {
		ln.Close()
		return nil, fmt.Errorf("daemon: setting permissions on %s: %w", path, err)
	}

	return &Listener{path: path, listener: ln, checker: checker}, nil
}

// Accept - This method blocks until a connection arrives, checks its peer
// credential, and returns it.
//
// A connection the checker refuses is closed and never returned. Accept
// waits for the next one rather than returning an error, since one bad
// attempt is not the Listener failing.
//
// It returns an error only when the underlying accept fails, most commonly
// because Close was called.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		conn, err := l.listener.AcceptUnix()
		if err != nil {
			return nil, fmt.Errorf("daemon: accept: %w", err)
		}
		if err := l.checker.CheckPeer(conn); err != nil {
			// A refused peer is not this Listener's failure; log the
			// reason at the caller's discretion by moving on, exactly the
			// "keep listening" behavior a real daemon needs so one hostile
			// or misconfigured connection attempt can never take the whole
			// socket down.
			conn.Close()
			continue
		}
		return conn, nil
	}
}

// Close stops accepting new connections on this Listener and removes its
// own socket file from disk, so a later Listen against the same path does
// not need to treat this Listener's, cleanly closed socket as a stale file
// left behind by a crash.
func (l *Listener) Close() error {
	err := l.listener.Close()
	if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) {
		if err == nil {
			err = rmErr
		}
	}
	return err
}

// AcceptAndHandshake - This method calls Accept, then runs the armorchan
// server handshake over the accepted connection, returning a ready
// Channel.
//
// A connection that fails its peer credential check never reaches the
// handshake. One that passes but fails the handshake is closed here and
// reported as this call's error, rather than returned half connected.
func AcceptAndHandshake(l *Listener, daemonStaticPrivate *ecdh.PrivateKey) (*armorchan.Channel, net.Conn, error) {
	conn, err := l.Accept()
	if err != nil {
		return nil, nil, err
	}
	ch, err := armorchan.ServerHandshake(conn, daemonStaticPrivate)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("daemon: handshake with accepted connection: %w", err)
	}
	return ch, conn, nil
}

// Dial connects to a RouterCLI daemon's Unix domain socket at path, this
// CLI process's config.SystemConfig.DaemonSocketPath, and runs
// armorchan.ClientHandshake against expectedDaemonStaticPublic, this
// daemon's known static public key, most read from the world readable key
// file the daemon itself writes at startup; see this project's handshake
// design. Dial returns armorchan.ErrHandshakeFailed, wrapped, rather than
// any Channel, if whatever answered on path could not prove it holds the
// private key matching expectedDaemonStaticPublic.
func Dial(path string, expectedDaemonStaticPublic *ecdh.PublicKey) (*armorchan.Channel, net.Conn, error) {
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: resolving socket path %s: %w", path, err)
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: dialing %s: %w", path, err)
	}
	ch, err := armorchan.ClientHandshake(conn, expectedDaemonStaticPublic)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("daemon: handshake with %s: %w", path, err)
	}
	return ch, conn, nil
}
