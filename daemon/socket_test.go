// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gotermcli/routercli/armorchan"
)

func socketPath(t *testing.T) string {
	t.Helper()
	// A real Unix domain socket path has a length limit, 104 bytes on
	// macOS, 108 on Linux, see unix(7)'s sockaddr_un, well under what
	// t.TempDir() alone can produce on some platforms. A short, fixed
	// socket file name inside that directory is not enough on its own to
	// stay clear of that limit, since t.TempDir() itself folds this test's
	// function name into the directory it creates, and, on macOS,
	// os.TempDir() already returns a long, per user path under
	// /var/folders/... before that name is even added; the two together
	// regularly exceed 104 bytes even with a short trailing file name.
	// /tmp, unlike os.TempDir(), stays short on every platform this
	// project targets, so this builds a private, short named directory
	// directly under it instead, cleaned up through t.Cleanup the same way
	// t.TempDir() would be, rather than folding this test's name into the
	// path at all.
	dir, err := os.MkdirTemp("/tmp", "rcd-")
	if err != nil {
		t.Fatalf("creating a short lived socket directory under /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "routercli.sock")
}

// TestListenCreatesSocketWithRestrictivePermissions - This test verifies
// that Listen creates its own socket file with exactly socketPermissions,
// 0600.
func TestListenCreatesSocketWithRestrictivePermissions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	path := socketPath(t)
	l, err := Listen(path, NewAllowedUIDs(uint32(os.Getuid())))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket file: %v", err)
	}
	if got := info.Mode().Perm(); got != socketPermissions {
		t.Errorf("socket file permissions = %o, want %o", got, socketPermissions)
	}
}

// TestListenRemovesStaleSocketFile - This test verifies that Listen
// removes a pre-existing file at path before binding, standing in for a
// socket file a previous, killed-outright daemon left behind.
func TestListenRemovesStaleSocketFile(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("writing stale file: %v", err)
	}

	l, err := Listen(path, NewAllowedUIDs(0))
	if err != nil {
		t.Fatalf("Listen over a stale file: %v", err)
	}
	l.Close()
}

// TestListenFailsWithNilChecker - This test verifies that Listen refuses
// to construct a Listener with no PeerCredentialChecker at all, rather
// than silently accepting every connection from every UID, the same fail
// loudly convention this project applies everywhere else to a malformed or
// missing required setting.
func TestListenFailsWithNilChecker(t *testing.T) {
	_, err := Listen(socketPath(t), nil)
	if err == nil {
		t.Fatal("expected Listen(path, nil) to fail")
	}
}

// TestAcceptAndHandshakeAndDialRoundTrip - This is this package's first
// real, end to end test against an actual Unix domain socket, rather than
// the in-memory harnesses the earlier tests used: a real Listener, a real
// Dial, a real armorchan handshake completing over that real socket, and a
// genuine message exchanged in both directions afterward.
func TestAcceptAndHandshakeAndDialRoundTrip(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	path := socketPath(t)
	daemonStaticPrivate, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	l, err := Listen(path, NewAllowedUIDs(uint32(os.Getuid())))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	type serverResult struct {
		ch   *armorchan.Channel
		conn net.Conn
		err  error
	}
	serverResultCh := make(chan serverResult, 1)
	go func() {
		ch, conn, err := AcceptAndHandshake(l, daemonStaticPrivate)
		serverResultCh <- serverResult{ch, conn, err}
	}()

	clientCh, clientConn, err := Dial(path, daemonStaticPrivate.PublicKey())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.Close()

	var sr serverResult
	select {
	case sr = <-serverResultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptAndHandshake never returned")
	}
	if sr.err != nil {
		t.Fatalf("AcceptAndHandshake: %v", sr.err)
	}
	defer sr.conn.Close()

	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- clientCh.Send([]byte("Hello from a real socket")) }()

	got, err := sr.ch.Receive()
	if err != nil {
		t.Fatalf("server Receive: %v", err)
	}
	if err := <-sendErrCh; err != nil {
		t.Fatalf("client Send: %v", err)
	}
	if string(got) != "Hello from a real socket" {
		t.Errorf("server Receive = %q, want %q", got, "Hello from a real socket")
	}

	// And the other direction, daemon to client, over the same real
	// socket, confirming this is a genuine full duplex channel, not only a
	// one way pipe that happened to work above.
	sendErrCh = make(chan error, 1)
	go func() { sendErrCh <- sr.ch.Send([]byte("Hello back from the daemon")) }()

	got, err = clientCh.Receive()
	if err != nil {
		t.Fatalf("client Receive: %v", err)
	}
	if err := <-sendErrCh; err != nil {
		t.Fatalf("server Send: %v", err)
	}
	if string(got) != "Hello back from the daemon" {
		t.Errorf("client Receive = %q, want %q", got, "Hello back from the daemon")
	}
}

// TestAcceptRefusesDisallowedPeerUID - This test verifies the actual
// enforcement this whole layer exists for: a connection whose real UID is
// not in the Listener's allowed set is refused before its own handshake,
// or anything else, ever runs, rather than merely logged or warned about.
// Every real UID on this machine, this test's included, is known to the
// kernel, so this test constructs an AllowedUIDs set that does not contain
// this process's UID, standing in for a connection from an account this
// daemon was never configured to trust.
func TestAcceptRefusesDisallowedPeerUID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	path := socketPath(t)
	ourUID := uint32(os.Getuid())
	disallowed := NewAllowedUIDs(ourUID + 12345) // deliberately not our own UID

	l, err := Listen(path, disallowed)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	acceptResultCh := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			conn.Close()
		}
		acceptResultCh <- err
	}()

	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		t.Fatalf("ResolveUnixAddr: %v", err)
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}

	// Accept itself never returns for a refused connection, see its own
	// doc comment, it moves on to wait for the next one; the observable
	// proof a refusal happened is this connection being closed on the
	// daemon's side without ever completing a handshake, or Accept
	// delivering a completely different, later connection instead. Confirm
	// the refused connection's read side observes EOF, is closed rather
	// than left hanging, within a bounded time, then confirm Accept is
	// still waiting rather than having returned this refused connection.
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Error("expected the disallowed connection to be closed by the daemon rather than yielding real data")
	}
	conn.Close()

	select {
	case err := <-acceptResultCh:
		t.Fatalf("Accept returned (err=%v) for a connection this test never expected it to hand back, since the only connection made was from a disallowed UID", err)
	case <-time.After(200 * time.Millisecond):
		// Accept is still blocked waiting for the next connection, exactly
		// the "refused, keep listening" behavior expected.
	}
}

// TestAllowedUIDsRefusesEveryUIDWhenEmpty - This test verifies that an
// AllowedUIDs constructed with no members at all refuses every connection,
// including one from this test process's UID, the correct default-deny
// shape for a checker nothing was explicitly added to.
func TestAllowedUIDsRefusesEveryUIDWhenEmpty(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	empty := NewAllowedUIDs()
	if len(empty) != 0 {
		t.Fatalf("NewAllowedUIDs() with no arguments should be empty, got %d entries", len(empty))
	}

	path := socketPath(t)
	l, err := Listen(path, empty)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	// Accept itself must run for the peer credential check to happen at
	// all; without a goroutine pulling the connection off the Listener's
	// accept queue, the connection below would sit unaccepted, and a read
	// against it would time out regardless of what AllowedUIDs contains,
	// proving nothing about the refusal this test exists to verify.
	go func() {
		conn, err := l.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		t.Fatalf("ResolveUnixAddr: %v", err)
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(buf); err == nil {
		t.Error("expected the connection to be closed outright by an AllowedUIDs with no members")
	}
}

// TestPeerCredentialCheckerErrorIsDistinctFromLookupFailure - This test
// verifies AllowedUIDs.CheckPeer's two distinct failure modes stay
// distinguishable: a real *peerNotAllowedError for a UID the kernel
// successfully reported but that is not allowed, confirmed here directly
// against a live connection this test knows is not in the allowed set.
func TestPeerCredentialCheckerErrorIsDistinctFromLookupFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking is Linux only for now; see peercred_other.go")
	}

	path := socketPath(t)
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		t.Fatalf("ResolveUnixAddr: %v", err)
	}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	defer ln.Close()
	defer os.Remove(path)

	serverConnCh := make(chan *net.UnixConn, 1)
	go func() {
		conn, _ := ln.AcceptUnix()
		serverConnCh <- conn
	}()

	clientConn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-serverConnCh
	defer serverConn.Close()

	checker := NewAllowedUIDs(uint32(os.Getuid()) + 999)
	err = checker.CheckPeer(serverConn)
	if err == nil {
		t.Fatal("expected CheckPeer to refuse a UID deliberately not in the allowed set")
	}
	var notAllowed *peerNotAllowedError
	if !errors.As(err, &notAllowed) {
		t.Errorf("CheckPeer error = %v (%T), want *peerNotAllowedError", err, err)
	}
}
