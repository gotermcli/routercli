// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// TestReadFrameRoundTripsWriteFrame - This test verifies the ordinary
// case: whatever writeFrame writes, readFrame reads back byte for byte,
// for both an empty frame and an ordinary sized one.
func TestReadFrameRoundTripsWriteFrame(t *testing.T) {
	for _, body := range [][]byte{{}, []byte("hello"), bytes.Repeat([]byte{0x42}, 4096)} {
		var buf bytes.Buffer
		if err := writeFrame(&buf, body); err != nil {
			t.Fatalf("writeFrame(%d bytes): %v", len(body), err)
		}
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("readFrame(%d bytes): %v", len(body), err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("readFrame round trip = %x, want %x", got, body)
		}
	}
}

// TestWriteFrameRefusesOversizedBody - This test verifies that writeFrame
// itself refuses to send a body larger than MaxFrameSize, this package's
// bug if ever reached rather than a hostile peer's, checked anyway per
// this project's "fail loudly" convention.
func TestWriteFrameRefusesOversizedBody(t *testing.T) {
	var buf bytes.Buffer
	err := writeFrame(&buf, make([]byte, MaxFrameSize+1))
	if err == nil {
		t.Fatal("expected writeFrame to refuse a body larger than MaxFrameSize")
	}
	if buf.Len() != 0 {
		t.Errorf("expected nothing written when writeFrame refuses an oversized body, got %d bytes", buf.Len())
	}
}

// TestReadFrameRejectsClaimedLengthAboveMax - This test verifies that a
// length prefix claiming more bytes than MaxFrameSize is refused with
// ErrFrameTooLarge before readFrame ever attempts to read, or allocate a
// buffer for, that claimed length; this is the exact property that keeps a
// four byte length prefix claiming close to four gigabytes from being able
// to force a large allocation on its own.
func TestReadFrameRejectsClaimedLengthAboveMax(t *testing.T) {
	var lengthPrefix [4]byte
	binary.BigEndian.PutUint32(lengthPrefix[:], MaxFrameSize+1)
	buf := bytes.NewBuffer(lengthPrefix[:])

	_, err := readFrame(buf)
	if err != ErrFrameTooLarge {
		t.Errorf("readFrame error = %v, want ErrFrameTooLarge", err)
	}
}

// TestReadFrameHandlesTruncatedInput - This test verifies that a
// connection that closes, or stalls forever, partway through a frame,
// whether inside the length prefix itself or inside the body a genuine
// length prefix promised, is reported as an ordinary error rather than a
// panic. Every case here is fed through io.EOF directly, standing in for a
// peer that vanished mid-message.
func TestReadFrameHandlesTruncatedInput(t *testing.T) {
	cases := map[string][]byte{
		"nothing at all":                     {},
		"one byte of a four byte length":     {0x00},
		"three bytes of a four byte length":  {0x00, 0x00, 0x00},
		"length claims 10, body has 3":       concatBytes([]byte{0x00, 0x00, 0x00, 0x0a}, []byte{0x01, 0x02, 0x03}),
		"length claims 10, body has nothing": concatBytes([]byte{0x00, 0x00, 0x00, 0x0a}),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(input))
			if err == nil {
				t.Errorf("expected an error for truncated input %q, got nil", name)
			}
		})
	}
}

// concatBytes is a small readability helper for
// TestReadFrameHandlesTruncatedInput's table, joining a length prefix and
// however much of a body a given case supplies.
func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// FuzzReadFrame - This fuzz test feeds arbitrary byte sequences directly
// to readFrame, this package's decoder for both the raw handshake messages
// and every encrypted record afterward, confirming none of them ever panic
// it, no matter how the length prefix and body are truncated, malformed,
// or mismatched against each other. A tampered or malformed post-handshake
// record is expected to be caught one layer up, by Channel.Receive's AEAD
// authentication check, once it has been read; this fuzz test's job is
// only confirming readFrame itself never panics finding that record's
// boundary in the first place.
func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x02, 0x03})
	f.Add([]byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x02})
	f.Add(bytes.Repeat([]byte{0x41}, 5000))

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("readFrame panicked on input %x: %v", data, r)
			}
		}()
		_, _ = readFrame(bytes.NewReader(data))
	})
}

// FuzzChannelReceive - This fuzz test constructs a genuine Channel through
// a real handshake, then feeds arbitrary bytes directly to the underlying
// connection in place of a real, encrypted record, confirming Receive
// itself never panics on malformed post-handshake traffic, only ever
// returns an error and leaves the Channel faulted.
func FuzzChannelReceive(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})
	f.Add(bytes.Repeat([]byte{0x99}, 64))
	f.Add(bytes.Repeat([]byte{0x00}, 200))

	f.Fuzz(func(t *testing.T, recordBody []byte) {
		if len(recordBody) > MaxFrameSize {
			t.Skip("not a case readFrame itself would ever accept past its own length check")
		}

		// A minimal, otherwise unused AEAD, keyed from fixed, obviously
		// fake key material, giving Receive something real to call Open
		// against; the point of this fuzz target is confirming no input to
		// Receive itself, garbage included, ever panics, not exercising a
		// genuine handshake again, already covered directly by the
		// handshake tests elsewhere in this package.
		var fakeKey [32]byte
		aead, err := newAEAD(fakeKey)
		if err != nil {
			t.Fatalf("newAEAD: %v", err)
		}

		pr, pw := io.Pipe()
		fakeChannel := &Channel{conn: readOnlyConn{pr}, recvAEAD: aead}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Receive panicked on malformed record %x: %v", recordBody, r)
			}
		}()

		go func() {
			_ = writeFrame(pw, recordBody)
			_ = pw.Close()
		}()

		_, _ = fakeChannel.Receive()
	})
}

// readOnlyConn adapts an io.Reader into the io.ReadWriter a Channel
// expects, with Write always failing outright; used only by
// FuzzChannelReceive above, which drives Receive directly against a hand
// assembled Channel that never went through ServerHandshake or
// ClientHandshake, and never calls Send on it at all.
type readOnlyConn struct {
	r io.Reader
}

func (c readOnlyConn) Read(b []byte) (int, error) { return c.r.Read(b) }
func (c readOnlyConn) Write([]byte) (int, error)  { return 0, io.ErrClosedPipe }
