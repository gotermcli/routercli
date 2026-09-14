// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

// relayedConn is one endpoint of a bidirectional, in-memory connection
// assembled from two directional Go channels of frames, relayed by a
// goroutine this test controls, in place of a real socket or a plain
// net.Pipe. writeFrame writes exactly one frame per call to Write. That is
// intended, so relaying whole channel elements, one per frame, never needs
// to reassemble or split anything a real byte stream transport would.
type relayedConn struct {
	out chan<- []byte
	in  <-chan []byte
	buf []byte
}

func (c *relayedConn) Write(p []byte) (int, error) {
	c.out <- append([]byte{}, p...)
	return len(p), nil
}

func (c *relayedConn) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		b, ok := <-c.in
		if !ok {
			return 0, io.EOF
		}
		c.buf = b
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// tamperableLink connects a client side relayedConn and a server side
// relayedConn through a relay goroutine this test controls directly,
// letting a test inspect, corrupt, drop, or resend any specific frame
// travelling in either direction, something neither a real socket nor a
// plain net.Pipe would ever allow a well behaved test to do to its own
// traffic. Every frame relayed in either direction is also kept in
// clientToServerFrames or serverToClientFrames, in order, so a test can
// recover the exact bytes a genuine Send produced, to feed back in again
// later for a replay test.
type tamperableLink struct {
	clientConn, serverConn *relayedConn

	mu                        sync.Mutex
	clientToServerFrames      [][]byte
	serverToClientFrames      [][]byte
	corruptNextClientToServer bool

	rawClientToServer chan []byte
	rawServerToClient chan []byte
}

func newTamperableLink() *tamperableLink {
	l := &tamperableLink{
		rawClientToServer: make(chan []byte, 16),
		rawServerToClient: make(chan []byte, 16),
	}
	deliveredClientToServer := make(chan []byte, 16)
	deliveredServerToClient := make(chan []byte, 16)

	l.clientConn = &relayedConn{out: l.rawClientToServer, in: deliveredServerToClient}
	l.serverConn = &relayedConn{out: l.rawServerToClient, in: deliveredClientToServer}

	go func() {
		for frame := range l.rawClientToServer {
			l.mu.Lock()
			l.clientToServerFrames = append(l.clientToServerFrames, append([]byte{}, frame...))
			corrupt := l.corruptNextClientToServer
			l.corruptNextClientToServer = false
			l.mu.Unlock()

			if corrupt {
				frame = flipOneBit(frame)
			}
			deliveredClientToServer <- frame
		}
	}()
	go func() {
		for frame := range l.rawServerToClient {
			l.mu.Lock()
			l.serverToClientFrames = append(l.serverToClientFrames, append([]byte{}, frame...))
			l.mu.Unlock()
			deliveredServerToClient <- frame
		}
	}()

	return l
}

// corruptNextClientToServerFrame arms this link to flip one bit in the
// very next frame the client side writes, before delivering it to the
// server side, then disarms itself; exactly one frame is ever affected by
// one call.
func (l *tamperableLink) corruptNextClientToServerFrame() {
	l.mu.Lock()
	l.corruptNextClientToServer = true
	l.mu.Unlock()
}

// replayClientToServerFrame delivers frame, previously recorded off a
// genuine client write by clientToServerFrames, to the server side a
// second time, standing in for an attacker who captured a real, validly
// encrypted record and resent it later on the same connection, without
// needing to have broken anything about it.
func (l *tamperableLink) replayClientToServerFrame(frame []byte) {
	l.rawClientToServer <- append([]byte{}, frame...)
}

func flipOneBit(frame []byte) []byte {
	cp := append([]byte{}, frame...)
	if len(cp) > 4 {
		// Byte 4 is the first byte of the frame's body, the ciphertext
		// itself, past the four byte length prefix; flipping a bit there
		// corrupts the AEAD sealed record without changing its own claimed
		// length, exactly what a bit flip in transit, or a deliberate
		// tamper attempt, would look like.
		cp[4] ^= 0x01
	}
	return cp
}

// handshakeOverTamperableLink runs a genuine handshake over a fresh
// tamperableLink and returns both resulting Channels alongside the link
// itself, for tests that need to corrupt or replay a specific frame after
// the handshake has already completed normally.
func handshakeOverTamperableLink(t *testing.T) (serverSide, clientSide *Channel, link *tamperableLink) {
	t.Helper()

	daemonStaticPrivate, err := GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	link = newTamperableLink()

	type result struct {
		channel *Channel
		err     error
	}
	serverResult := make(chan result, 1)
	clientResult := make(chan result, 1)

	go func() {
		ch, err := ServerHandshake(link.serverConn, daemonStaticPrivate)
		serverResult <- result{ch, err}
	}()
	go func() {
		ch, err := ClientHandshake(link.clientConn, daemonStaticPrivate.PublicKey())
		clientResult <- result{ch, err}
	}()

	sr := <-serverResult
	cr := <-clientResult
	if sr.err != nil {
		t.Fatalf("ServerHandshake: %v", sr.err)
	}
	if cr.err != nil {
		t.Fatalf("ClientHandshake: %v", cr.err)
	}
	return sr.channel, cr.channel, link
}

// TestChannelRejectsTamperedRecord - This test verifies that a single
// flipped bit anywhere in a genuine, previously valid ciphertext record is
// caught by AES-GCM's authentication tag check: Receive returns an error
// rather than corrupted plaintext or a silent success, and the Channel
// permanently faults afterward, refusing every later Receive call with
// ErrChannelFaulted rather than continuing to read from a connection that
// already produced one unauthenticated record.
func TestChannelRejectsTamperedRecord(t *testing.T) {
	serverSide, clientSide, link := handshakeOverTamperableLink(t)

	link.corruptNextClientToServerFrame()

	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- clientSide.Send([]byte("hostname edgeswitch1")) }()

	_, receiveErr := serverSide.Receive()
	if receiveErr == nil {
		t.Fatal("expected Receive to reject a tampered record, got a nil error")
	}
	if err := <-sendErrCh; err != nil {
		t.Fatalf("Send: %v", err)
	}

	if _, err := serverSide.Receive(); !errors.Is(err, ErrChannelFaulted) {
		t.Errorf("second Receive after a tampered record = %v, want ErrChannelFaulted", err)
	}
}

// TestChannelRejectsReplayedRecord - This test verifies the other half of
// this package's replay protection: a genuine, successfully delivered
// record, captured exactly as it was sent, and resent later on the same
// connection, is rejected outright rather than accepted a second time. The
// receiver's nonce counter has already moved past the value that record
// was encrypted under, so the replayed record fails authentication against
// the counter value the receiver now expects next, exactly the property
// this project's testing requirements name directly.
func TestChannelRejectsReplayedRecord(t *testing.T) {
	serverSide, clientSide, link := handshakeOverTamperableLink(t)

	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- clientSide.Send([]byte("account create betty")) }()

	got, err := serverSide.Receive()
	if err != nil {
		t.Fatalf("Receive (first, genuine delivery): %v", err)
	}
	if err := <-sendErrCh; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(got) != "account create betty" {
		t.Fatalf("Receive returned %q, want %q", got, "account create betty")
	}

	link.mu.Lock()
	if len(link.clientToServerFrames) == 0 {
		link.mu.Unlock()
		t.Fatal("expected at least one recorded client-to-server frame")
	}
	capturedFrame := append([]byte{}, link.clientToServerFrames[len(link.clientToServerFrames)-1]...)
	link.mu.Unlock()

	link.replayClientToServerFrame(capturedFrame)

	if _, err := serverSide.Receive(); err == nil {
		t.Error("expected Receive to reject a replayed record, got a nil error")
	} else if errors.Is(err, ErrChannelFaulted) {
		t.Error("expected a distinct authentication failure on the replay itself, not an already-faulted channel from a prior test step")
	}
}

// TestChannelNonceCounterAdvancesExactlyOncePerMessage - This test
// verifies, directly against the unexported counters themselves, that each
// direction's nonce counter advances by exactly one per successfully sent
// or received message, never skipping and never double counting, the
// property deriveNonce's construction depends on to guarantee two records
// in the same direction are never protected by the same nonce under the
// same key.
func TestChannelNonceCounterAdvancesExactlyOncePerMessage(t *testing.T) {
	serverSide, clientSide, _ := handshakeOverPipe(t)

	const messageCount = 25
	for i := 0; i < messageCount; i++ {
		msg := []byte(fmt.Sprintf("message %d", i))
		errCh := make(chan error, 1)
		go func() { errCh <- clientSide.Send(msg) }()
		if _, err := serverSide.Receive(); err != nil {
			t.Fatalf("Receive #%d: %v", i, err)
		}
		if err := <-errCh; err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}

	clientSide.sendMu.Lock()
	sendCount := clientSide.sendCount
	clientSide.sendMu.Unlock()
	if sendCount != messageCount {
		t.Errorf("client sendCount = %d, want %d", sendCount, messageCount)
	}

	serverSide.recvMu.Lock()
	recvCount := serverSide.recvCount
	serverSide.recvMu.Unlock()
	if recvCount != messageCount {
		t.Errorf("server recvCount = %d, want %d", recvCount, messageCount)
	}
}

// TestChannelSendFaultsPermanentlyAfterUnderlyingWriteError - This test
// verifies that once Send hits a real error writing to the underlying
// connection, every later call to Send on that same Channel returns
// ErrChannelFaulted immediately, without attempting to write to the
// connection again, rather than retrying or leaving the Channel in an
// ambiguous, possibly reusable state.
func TestChannelSendFaultsPermanentlyAfterUnderlyingWriteError(t *testing.T) {
	serverSide, _, _ := handshakeOverPipe(t)

	failing := &alwaysFailingConn{}
	serverSide.conn = failing

	if err := serverSide.Send([]byte("first")); err == nil {
		t.Fatal("expected the first Send against a failing connection to return an error")
	}
	if got := failing.writeCalls; got != 1 {
		t.Fatalf("expected exactly one Write attempt against the failing connection, got %d", got)
	}

	if err := serverSide.Send([]byte("second")); !errors.Is(err, ErrChannelFaulted) {
		t.Errorf("second Send after a write failure = %v, want ErrChannelFaulted", err)
	}
	if got := failing.writeCalls; got != 1 {
		t.Errorf("expected no further Write attempts once faulted, got %d total calls", got)
	}
}

type alwaysFailingConn struct {
	writeCalls int
	readCalls  int
}

func (c *alwaysFailingConn) Write(p []byte) (int, error) {
	c.writeCalls++
	return 0, errors.New("simulated permanent write failure")
}

func (c *alwaysFailingConn) Read(p []byte) (int, error) {
	c.readCalls++
	return 0, errors.New("simulated permanent read failure")
}
