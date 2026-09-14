// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrHandshakeFailed is returned by ClientHandshake when the peer could
// not be authenticated as the holder of the expected static private key.
//
// That covers two cases: it advertised a different static public key than
// the caller holds, or it failed to produce a confirmation record the
// derived keys could decrypt.
//
// Both mean the same thing in practice. Something other than the genuine
// daemon answered on the socket, and no Channel is returned.
var ErrHandshakeFailed = errors.New("armorchan: handshake failed, peer is not the expected daemon")

// curve is the one elliptic curve this package ever uses, Curve25519 by
// way of crypto/ecdh's X25519 implementation, chosen here once so
// ServerHandshake, ClientHandshake, and GenerateStaticKeyPair never each
// pick it separately.
func curve() ecdh.Curve {
	return ecdh.X25519()
}

// GenerateStaticKeyPair - This function returns a fresh X25519 private
// key for a daemon's persisted static identity. The public half is what
// clients are given out of band and what they pass to ClientHandshake as
// expectedDaemonStaticPublic.
func GenerateStaticKeyPair() (*ecdh.PrivateKey, error) {
	key, err := curve().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("armorchan: generating static key pair: %w", err)
	}
	return key, nil
}

// protocolLabel is folded into the transcript hash and into every HKDF
// info string this package derives from, so a later incompatible version
// can change this one constant and never risk a byte sequence from one
// version being read as valid under the other.
const protocolLabel = "RouterCLI armorchan v1"

// derivedKeys holds every value this handshake's HKDF step produces, one
// call to deriveKeys computing all five as an atomic group so
// ServerHandshake and ClientHandshake can never accidentally derive some
// of these from one transcript and the rest from another.
type derivedKeys struct {
	daemonToClientKey       [32]byte
	daemonToClientNonceBase [nonceLength]byte
	clientToDaemonKey       [32]byte
	clientToDaemonNonceBase [nonceLength]byte
	confirmationKey         [32]byte
}

// deriveKeys - This function implements the key schedule: HKDF extract and
// expand, RFC 5869, over the ECDH shared secret.
//
// The salt is a transcript hash covering both public keys this handshake
// exchanged and the protocol label. Each purpose gets its own expansion
// with a distinct info string.
//
// That is the same "one secret in, several independent purpose bound keys
// out" shape TLS 1.3 uses, RFC 9846 Section 7.1.
func deriveKeys(sharedSecret []byte, transcriptHash [sha256.Size]byte) (derivedKeys, error) {
	var out derivedKeys

	expand := func(info string, length int) ([]byte, error) {
		key, err := hkdf.Key(sha256.New, sharedSecret, transcriptHash[:], info, length)
		if err != nil {
			return nil, fmt.Errorf("armorchan: deriving %q: %w", info, err)
		}
		return key, nil
	}

	d2cKey, err := expand(protocolLabel+" daemon-to-client key", 32)
	if err != nil {
		return derivedKeys{}, err
	}
	copy(out.daemonToClientKey[:], d2cKey)

	d2cNonce, err := expand(protocolLabel+" daemon-to-client nonce", nonceLength)
	if err != nil {
		return derivedKeys{}, err
	}
	copy(out.daemonToClientNonceBase[:], d2cNonce)

	c2dKey, err := expand(protocolLabel+" client-to-daemon key", 32)
	if err != nil {
		return derivedKeys{}, err
	}
	copy(out.clientToDaemonKey[:], c2dKey)

	c2dNonce, err := expand(protocolLabel+" client-to-daemon nonce", nonceLength)
	if err != nil {
		return derivedKeys{}, err
	}
	copy(out.clientToDaemonNonceBase[:], c2dNonce)

	confirmKey, err := expand(protocolLabel+" server confirmation key", 32)
	if err != nil {
		return derivedKeys{}, err
	}
	copy(out.confirmationKey[:], confirmKey)

	return out, nil
}

// transcript - This function computes the transcript hash: the client
// ephemeral public key, then the daemon static public key, then the
// protocol label, through SHA-256.
//
// Both sides compute it identically from the same two keys in the same
// order. That is what lets deriveKeys produce the same output on both ends
// without either transmitting a derived key.
func transcript(clientEphemeralPublic, daemonStaticPublic *ecdh.PublicKey) [sha256.Size]byte {
	h := sha256.New()
	h.Write(clientEphemeralPublic.Bytes())
	h.Write(daemonStaticPublic.Bytes())
	h.Write([]byte(protocolLabel))
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// confirmationPlaintext is the fixed message the daemon encrypts under its
// derived confirmation key, and the client decrypts to confirm it is
// talking to the holder of the expected static private key.
//
// The content carries no meaning beyond being fixed and known to both
// sides. The confirmation key is used exactly once, for this one record,
// under an all-zero nonce, which is safe only because that key is never
// reused afterward.
var confirmationPlaintext = []byte(protocolLabel + " server confirmation")

// zeroNonce is the fixed nonce used for the one, single-use confirmation
// record. Reusing an all-zero nonce is safe here specifically, and nowhere
// else in this package.
var zeroNonce [nonceLength]byte

// ServerHandshake - This function runs the handshake from the daemon side
// of conn, using daemonStaticPrivate as the persisted static identity. It
// returns a ready Channel once the client's first message has been read
// and the confirmation record has been sent.
//
// This does not authenticate the connecting client. On the Unix domain
// socket this package runs above, an SO_PEERCRED check against the raw
// connection already did that before ServerHandshake was called.
func ServerHandshake(conn io.ReadWriter, daemonStaticPrivate *ecdh.PrivateKey) (*Channel, error) {
	clientEphemeralPublicBytes, err := readFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: reading client ephemeral public key: %w", err)
	}
	clientEphemeralPublic, err := curve().NewPublicKey(clientEphemeralPublicBytes)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: client ephemeral public key is not a valid X25519 key: %w", err)
	}

	sharedSecret, err := daemonStaticPrivate.ECDH(clientEphemeralPublic)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: computing shared secret: %w", err)
	}

	daemonStaticPublic := daemonStaticPrivate.PublicKey()
	transcriptHash := transcript(clientEphemeralPublic, daemonStaticPublic)

	keys, err := deriveKeys(sharedSecret, transcriptHash)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: %w", err)
	}

	confirmAEAD, err := newAEAD(keys.confirmationKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: %w", err)
	}
	confirmationRecord := confirmAEAD.Seal(nil, zeroNonce[:], confirmationPlaintext, transcriptHash[:])

	response := append(append([]byte{}, daemonStaticPublic.Bytes()...), confirmationRecord...)
	if err := writeFrame(conn, response); err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: sending confirmation: %w", err)
	}

	sendAEAD, err := newAEAD(keys.daemonToClientKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: %w", err)
	}
	recvAEAD, err := newAEAD(keys.clientToDaemonKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: server handshake: %w", err)
	}

	return &Channel{
		conn:     conn,
		sendAEAD: sendAEAD,
		sendBase: keys.daemonToClientNonceBase,
		recvAEAD: recvAEAD,
		recvBase: keys.clientToDaemonNonceBase,
	}, nil
}

// ClientHandshake - This function runs the handshake from the client side
// of conn, generating a fresh ephemeral key pair for this one connection.
// It returns a ready Channel once the daemon's confirmation record has
// been received and verified against expectedDaemonStaticPublic.
//
// It returns ErrHandshakeFailed, and no Channel, whenever the peer fails
// to prove it holds the private key matching expectedDaemonStaticPublic.
func ClientHandshake(conn io.ReadWriter, expectedDaemonStaticPublic *ecdh.PublicKey) (*Channel, error) {
	clientEphemeralPrivate, err := curve().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: generating ephemeral key pair: %w", err)
	}
	clientEphemeralPublic := clientEphemeralPrivate.PublicKey()

	if err := writeFrame(conn, clientEphemeralPublic.Bytes()); err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: sending ephemeral public key: %w", err)
	}

	response, err := readFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: reading server response: %w", err)
	}
	staticKeyLength := len(expectedDaemonStaticPublic.Bytes())
	if len(response) < staticKeyLength {
		return nil, errors.New("armorchan: client handshake: server response shorter than one static public key")
	}
	advertisedStaticPublicBytes := response[:staticKeyLength]
	confirmationRecord := response[staticKeyLength:]

	if !bytes.Equal(advertisedStaticPublicBytes, expectedDaemonStaticPublic.Bytes()) {
		return nil, fmt.Errorf("%w: server advertised a different static public key than expected", ErrHandshakeFailed)
	}

	sharedSecret, err := clientEphemeralPrivate.ECDH(expectedDaemonStaticPublic)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: computing shared secret: %w", err)
	}

	transcriptHash := transcript(clientEphemeralPublic, expectedDaemonStaticPublic)

	keys, err := deriveKeys(sharedSecret, transcriptHash)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: %w", err)
	}

	confirmAEAD, err := newAEAD(keys.confirmationKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: %w", err)
	}
	if _, err := confirmAEAD.Open(nil, zeroNonce[:], confirmationRecord, transcriptHash[:]); err != nil {
		return nil, fmt.Errorf("%w: could not decrypt server confirmation record: %w", ErrHandshakeFailed, err)
	}

	sendAEAD, err := newAEAD(keys.clientToDaemonKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: %w", err)
	}
	recvAEAD, err := newAEAD(keys.daemonToClientKey)
	if err != nil {
		return nil, fmt.Errorf("armorchan: client handshake: %w", err)
	}

	return &Channel{
		conn:     conn,
		sendAEAD: sendAEAD,
		sendBase: keys.clientToDaemonNonceBase,
		recvAEAD: recvAEAD,
		recvBase: keys.daemonToClientNonceBase,
	}, nil
}
