// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameSize bounds every frame this package reads, handshake messages
// and encrypted records alike.
//
// Without it, a four byte length prefix claiming close to four gigabytes
// would make readFrame allocate that much before discovering the rest of
// the frame does not exist. That is a cheap way for a compromised or buggy
// peer to exhaust memory.
//
// SO_PEERCRED already restricts both ends to allowed accounts before a
// Channel exists, but a single check is never treated as sufficient on its
// own.
//
// One mebibyte fits the largest message this protocol needs, a full
// running-config payload included, with room to grow.
const MaxFrameSize = 1 << 20

// ErrFrameTooLarge is returned by readFrame when a peer's length prefix
// claims a frame larger than MaxFrameSize, before any attempt is made to
// read or allocate that much data.
var ErrFrameTooLarge = errors.New("armorchan: frame exceeds maximum allowed size")

// writeFrame - This function writes b as one length prefixed frame, a four
// byte big endian length followed by the body, in a single Write call.
//
// Every message this package sends uses this framing, so readFrame is the
// one place a peer needs to know how to find the next boundary.
//
// Writing the prefix and body in one call, rather than two, means one
// write to the connection is always exactly one frame. Every test here
// that observes, captures, or corrupts a specific frame depends on that.
func writeFrame(w io.Writer, b []byte) error {
	if len(b) > MaxFrameSize {
		// Reaching this is a bug in this package, not a hostile peer:
		// every caller here already keeps what it sends well under
		// MaxFrameSize. It is checked anyway rather than trusted.
		return fmt.Errorf("armorchan: refusing to send a %d byte frame, larger than MaxFrameSize", len(b))
	}
	framed := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(framed[:4], uint32(len(b)))
	copy(framed[4:], b)
	if _, err := w.Write(framed); err != nil {
		return fmt.Errorf("armorchan: writing frame: %w", err)
	}
	return nil
}

// readFrame - This function reads one frame written by writeFrame and
// returns its body.
//
// A length prefix claiming more than MaxFrameSize is refused with
// ErrFrameTooLarge before anything is allocated from that untrusted value.
// readFrame never allocates more than MaxFrameSize on one call, whatever a
// malformed or adversarial prefix claims.
func readFrame(r io.Reader) ([]byte, error) {
	var lengthPrefix [4]byte
	if _, err := io.ReadFull(r, lengthPrefix[:]); err != nil {
		return nil, fmt.Errorf("armorchan: reading frame length: %w", err)
	}
	length := binary.BigEndian.Uint32(lengthPrefix[:])
	if length > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("armorchan: reading frame body: %w", err)
	}
	return body, nil
}
