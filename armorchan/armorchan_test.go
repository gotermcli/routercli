// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"
)

// handshakeOverPipe runs a genuine ServerHandshake and a genuine
// ClientHandshake concurrently, connected by net.Pipe, an in-memory, fully
// synchronous net.Conn needing no real socket or daemon process, exactly
// the "a pair of test goroutines talking to each other" this package's
// this package is meant to be tested against. It fails the test outright
// if either side of the handshake returns an error, and otherwise returns
// both resulting Channels plus the daemon's static key pair, for tests
// that need to construct an impostor server or otherwise inspect what a
// genuine handshake agreed on.
func handshakeOverPipe(t *testing.T) (serverSide, clientSide *Channel, daemonStaticPrivate *ecdh.PrivateKey) {
	t.Helper()

	daemonStaticPrivate, err := GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	serverConn, clientConn := net.Pipe()

	type result struct {
		channel *Channel
		err     error
	}
	serverResult := make(chan result, 1)
	clientResult := make(chan result, 1)

	go func() {
		ch, err := ServerHandshake(serverConn, daemonStaticPrivate)
		serverResult <- result{ch, err}
	}()
	go func() {
		ch, err := ClientHandshake(clientConn, daemonStaticPrivate.PublicKey())
		clientResult <- result{ch, err}
	}()

	var sr, cr result
	select {
	case sr = <-serverResult:
	case <-time.After(5 * time.Second):
		t.Fatal("ServerHandshake never returned")
	}
	select {
	case cr = <-clientResult:
	case <-time.After(5 * time.Second):
		t.Fatal("ClientHandshake never returned")
	}

	if sr.err != nil {
		t.Fatalf("ServerHandshake: %v", sr.err)
	}
	if cr.err != nil {
		t.Fatalf("ClientHandshake: %v", cr.err)
	}

	return sr.channel, cr.channel, daemonStaticPrivate
}

// TestHandshakeProducesWorkingChannelBothDirections - This test verifies
// the ordinary success path: a genuine ServerHandshake and a genuine
// ClientHandshake, run over the two ends of one net.Pipe, each produce a
// Channel that can send a message the other side reads back correctly, in
// both directions, more than once.
func TestHandshakeProducesWorkingChannelBothDirections(t *testing.T) {
	serverSide, clientSide, _ := handshakeOverPipe(t)

	messages := []struct {
		from, to *Channel
		text     string
	}{
		{clientSide, serverSide, "Hello"},
		{serverSide, clientSide, "ListUsers"},
		{clientSide, serverSide, "a second message from the client"},
		{serverSide, clientSide, "a second message from the daemon"},
	}

	for _, m := range messages {
		errCh := make(chan error, 1)
		go func() { errCh <- m.from.Send([]byte(m.text)) }()

		got, err := m.to.Receive()
		if err != nil {
			t.Fatalf("Receive(%q): %v", m.text, err)
		}
		if err := <-errCh; err != nil {
			t.Fatalf("Send(%q): %v", m.text, err)
		}
		if string(got) != m.text {
			t.Errorf("Receive returned %q, want %q", got, m.text)
		}
	}
}

// TestClientHandshakeRejectsWrongExpectedStaticKey - This test verifies
// that a client holding the wrong expected daemon static public key, a
// different daemon entirely, or a stale key from before a real daemon
// rotated its own identity, refuses the connection with ErrHandshakeFailed
// rather than completing a handshake with whatever answered.
func TestClientHandshakeRejectsWrongExpectedStaticKey(t *testing.T) {
	daemonStaticPrivate, err := GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair (daemon): %v", err)
	}
	wrongStaticPrivate, err := GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair (wrong): %v", err)
	}

	serverConn, clientConn := net.Pipe()

	serverErrCh := make(chan error, 1)
	go func() {
		_, err := ServerHandshake(serverConn, daemonStaticPrivate)
		serverErrCh <- err
	}()

	_, clientErr := ClientHandshake(clientConn, wrongStaticPrivate.PublicKey())
	if !errors.Is(clientErr, ErrHandshakeFailed) {
		t.Errorf("ClientHandshake error = %v, want ErrHandshakeFailed", clientErr)
	}

	select {
	case <-serverErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ServerHandshake never returned after client rejected it")
	}
}

// TestClientHandshakeRejectsImpostorWithoutTheMatchingPrivateKey - This
// test verifies the actual security property this handshake is built to
// provide: merely advertising the genuine daemon's static public key is
// not enough to pass as that daemon. An impostor here knows the real
// expected public key, byte for byte, the same file a real client would
// read, and echoes it back correctly, but does not hold the matching
// private key, so it cannot produce a confirmation record any real client
// could decrypt. ClientHandshake MUST still refuse this connection with
// ErrHandshakeFailed.
func TestClientHandshakeRejectsImpostorWithoutTheMatchingPrivateKey(t *testing.T) {
	daemonStaticPrivate, err := GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair (daemon): %v", err)
	}
	genuineStaticPublic := daemonStaticPrivate.PublicKey()

	serverConn, clientConn := net.Pipe()

	go func() {
		// The impostor: reads the client's ephemeral public key, exactly
		// as a real server would, then answers with the genuine static
		// public key bytes, correctly, followed by garbage where a real
		// confirmation record would be. It never touches
		// daemonStaticPrivate at all.
		if _, err := readFrame(serverConn); err != nil {
			return
		}
		garbageConfirmation := make([]byte, 48)
		if _, err := rand.Read(garbageConfirmation); err != nil {
			return
		}
		response := append(append([]byte{}, genuineStaticPublic.Bytes()...), garbageConfirmation...)
		_ = writeFrame(serverConn, response)
	}()

	_, clientErr := ClientHandshake(clientConn, genuineStaticPublic)
	if !errors.Is(clientErr, ErrHandshakeFailed) {
		t.Errorf("ClientHandshake error = %v, want ErrHandshakeFailed", clientErr)
	}
}
