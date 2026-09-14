// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

// ErrChannelFaulted is returned by Send or Receive once a Channel has
// already failed once, from either direction. A Channel that has ever
// produced this error MUST NOT be used again. Every caller treats a
// faulted Channel as fatal to the underlying connection, closing it rather
// than continuing to use it or attempting to reset it.
var ErrChannelFaulted = errors.New("armorchan: channel already faulted, close the connection")

// ErrNonceSpaceExhausted is returned by Send once one direction has sent
// the maximum number of records a single set of derived keys may protect.
//
// Reaching it needs roughly 2^64 records on one connection, never
// realistic for a CLI session. It is checked anyway rather than letting a
// nonce counter wrap, which would reuse a nonce under the same key, the
// exact failure this package's nonce discipline exists to prevent.
var ErrNonceSpaceExhausted = errors.New("armorchan: nonce space exhausted for this direction, reconnect required")

// Channel - This type is a live encrypted, authenticated, ordered channel
// between the daemon and one CLI client, established by ServerHandshake or
// ClientHandshake.
//
// It wraps a raw connection and takes over framing from that point on.
// Nothing else should read from or write to that connection once a Channel
// exists for it.
//
// The two directions use independent keys and nonce counters, so one
// goroutine may Send while another Receives. Calling Send from more than
// one goroutine at once, or Receive from more than one, is not safe. Each
// direction has exactly one caller, so no serialization is added beyond
// each direction's own mutex.
type Channel struct {
	conn io.ReadWriter

	sendMu     sync.Mutex
	sendAEAD   cipher.AEAD
	sendBase   [nonceLength]byte
	sendCount  uint64
	sendFailed bool

	recvMu     sync.Mutex
	recvAEAD   cipher.AEAD
	recvBase   [nonceLength]byte
	recvCount  uint64
	recvFailed bool
}

// Send - This method encrypts plaintext under the send direction key and
// writes it as one frame to the connection.
//
// Once Send returns a non-nil error the send direction has faulted. Every
// later call returns ErrChannelFaulted without touching the connection,
// and the caller MUST close it.
func (c *Channel) Send(plaintext []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if c.sendFailed {
		return ErrChannelFaulted
	}

	if c.sendCount == math.MaxUint64 {
		c.sendFailed = true
		return ErrNonceSpaceExhausted
	}

	nonce := deriveNonce(c.sendBase, c.sendCount)
	aad := sequenceAAD(c.sendCount)
	ciphertext := c.sendAEAD.Seal(nil, nonce[:], plaintext, aad[:])

	if err := writeFrame(c.conn, ciphertext); err != nil {
		c.sendFailed = true
		return fmt.Errorf("armorchan: send: %w", err)
	}
	c.sendCount++
	return nil
}

// Receive - This method reads one frame from the connection and decrypts
// it under the receive direction key.
//
// The nonce and additional authenticated data come from the receive
// counter's current expected value, never from anything read off the wire.
// A record encrypted under any other counter value, whether tampered,
// corrupted, or replayed, fails its authentication tag and is rejected.
//
// The counter advances only after decryption succeeds.
//
// Once Receive returns a non-nil error the receive direction has faulted.
// Every later call returns ErrChannelFaulted without touching the
// connection, and the caller MUST close it.
func (c *Channel) Receive() ([]byte, error) {
	c.recvMu.Lock()
	defer c.recvMu.Unlock()

	if c.recvFailed {
		return nil, ErrChannelFaulted
	}

	if c.recvCount == math.MaxUint64 {
		c.recvFailed = true
		return nil, ErrNonceSpaceExhausted
	}

	ciphertext, err := readFrame(c.conn)
	if err != nil {
		c.recvFailed = true
		return nil, fmt.Errorf("armorchan: receive: %w", err)
	}

	nonce := deriveNonce(c.recvBase, c.recvCount)
	aad := sequenceAAD(c.recvCount)
	plaintext, err := c.recvAEAD.Open(nil, nonce[:], ciphertext, aad[:])
	if err != nil {
		c.recvFailed = true
		return nil, fmt.Errorf("armorchan: receive: record failed authentication, tampered, corrupted, or replayed: %w", err)
	}
	c.recvCount++
	return plaintext, nil
}

// nonceLength is the nonce size crypto/cipher's standard GCM construction
// expects, 96 bits, the same size TLS 1.3's record nonce uses, RFC 9846
// Section 5.3.
const nonceLength = 12

// deriveNonce - This function reproduces TLS 1.3's per record nonce
// construction, RFC 9846 Section 5.3: a fixed per direction base,
// established at handshake time and never sent on the wire, with its low
// 64 bits XORed against a big endian encoding of the sequence number.
//
// Neither side ever transmits a nonce or a sequence number. Both derive
// the same value only because both are counting records in the same
// direction in the same order.
//
// That is exactly what makes a replayed record fail authentication: the
// receiver's counter has already moved past it.
func deriveNonce(base [nonceLength]byte, counter uint64) [nonceLength]byte {
	var nonce [nonceLength]byte
	copy(nonce[:], base[:])
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	for i := 0; i < 8; i++ {
		nonce[4+i] ^= counterBytes[i]
	}
	return nonce
}

// sequenceAAD - This function returns the sequence number as eight big
// endian bytes, for use as the additional authenticated data on this
// record.
//
// Binding the same counter into the authenticated data as well as the
// nonce is belt and suspenders. Replay protection then does not rest on
// nonce derivation alone, so a later change to how a nonce is derived
// cannot silently reopen a replay window without this check also changing.
func sequenceAAD(counter uint64) [8]byte {
	var aad [8]byte
	binary.BigEndian.PutUint64(aad[:], counter)
	return aad
}
