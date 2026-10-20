// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"io"
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

// TestClientHandshakeRejectsImpostorWithoutTheMatchingPrivateKey - This test
// verifies the security property the handshake exists to provide: advertising
// the genuine daemon's static public key is not enough to pass as that
// daemon.
//
// The impostor here knows the expected public key byte for byte, from the
// same file a real client reads, and echoes it back correctly. It does not
// hold the matching private key, so it cannot produce a confirmation record
// any real client could decrypt.
//
// ClientHandshake MUST refuse with ErrHandshakeFailed.
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

// recordingConn - This type wraps a connection and keeps a copy of every byte
// written through it, which is what a passive attacker on the wire has.
type recordingConn struct {
	inner   io.ReadWriter
	written [][]byte
}

func (r *recordingConn) Read(p []byte) (int, error) { return r.inner.Read(p) }

func (r *recordingConn) Write(p []byte) (int, error) {
	r.written = append(r.written, append([]byte{}, p...))
	return r.inner.Write(p)
}

// TestStaticKeyAloneCannotDecryptARecordedSession - This test verifies forward
// secrecy: holding the daemon's long-lived static private key, plus a full
// recording of a session, is not enough to recover that session's traffic.
//
// This is the "capture now, steal the key later" case. The static key lives on
// disk beside the socket, so it is the realistic thing for an attacker to
// obtain after the fact, from a backup or a later compromise.
//
// The session is driven through the real ClientHandshake and Send, and the
// record is taken off the wire rather than constructed by the test. That
// matters: a test that seals its own record with keys it derived itself proves
// nothing about the implementation, since it would pass whatever the handshake
// did.
func TestStaticKeyAloneCannotDecryptARecordedSession(t *testing.T) {
	daemonStaticPrivate, err := curve().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating the daemon static key: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverWire := &recordingConn{inner: serverConn}
	go func() {
		ch, err := ServerHandshake(serverWire, daemonStaticPrivate)
		if err != nil {
			return
		}
		for {
			if _, err := ch.Receive(); err != nil {
				return
			}
		}
	}()

	// Everything the client writes, captured exactly as it goes out.
	wire := &recordingConn{inner: clientConn}

	clientCh, err := ClientHandshake(wire, daemonStaticPrivate.PublicKey())
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	secret := []byte("the operator's password")
	if err := clientCh.Send(secret); err != nil {
		t.Fatalf("sending the record: %v", err)
	}

	// Frame one is the client ephemeral public key. Frame two is the
	// encrypted record. Both carry a four byte length prefix.
	if len(wire.written) < 2 {
		t.Fatalf("expected at least two writes on the wire, got %d", len(wire.written))
	}
	clientEphemeralPublic, err := curve().NewPublicKey(wire.written[0][4:])
	if err != nil {
		t.Fatalf("parsing the recorded client ephemeral public key: %v", err)
	}
	record := wire.written[1][4:]

	// The server's response frame is on the wire too, and its first key
	// length bytes are the daemon ephemeral public key. The attacker has it,
	// so the transcript is fully reconstructable and cannot be what defeats
	// the attack.
	if len(serverWire.written) < 1 {
		t.Fatalf("expected the server response to have been recorded")
	}
	keyLength := len(daemonStaticPrivate.PublicKey().Bytes())
	daemonEphemeralPublic, err := curve().NewPublicKey(serverWire.written[0][4 : 4+keyLength])
	if err != nil {
		t.Fatalf("parsing the recorded daemon ephemeral public key: %v", err)
	}

	// The attacker holds the static private key and every public value that
	// crossed the wire, including the correct transcript. The only thing
	// missing is the ephemeral exchange, which needs a private key that no
	// longer exists anywhere.
	attackerShared, err := daemonStaticPrivate.ECDH(clientEphemeralPublic)
	if err != nil {
		t.Fatalf("attacker ECDH: %v", err)
	}
	attackerTranscript := transcript(clientEphemeralPublic, daemonEphemeralPublic,
		daemonStaticPrivate.PublicKey())
	attackerKeys, err := deriveKeys(attackerShared, attackerTranscript)
	if err != nil {
		t.Fatalf("attacker deriveKeys: %v", err)
	}
	attackerAEAD, err := newAEAD(attackerKeys.clientToDaemonKey)
	if err != nil {
		t.Fatalf("attacker newAEAD: %v", err)
	}
	aad := sequenceAAD(0)
	nonce := deriveNonce(attackerKeys.clientToDaemonNonceBase, 0)

	if plain, err := attackerAEAD.Open(nil, nonce[:], record, aad[:]); err == nil {
		t.Fatalf("FORWARD SECRECY BROKEN: the static key alone recovered %q", plain)
	}
}
