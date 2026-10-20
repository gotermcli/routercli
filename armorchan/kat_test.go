// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestKnownAnswerKeySchedule - This is regression protection for the key
// schedule, not an independent verification of it.
//
// Every expected value below was captured from a verified correct run of
// this same implementation and frozen. Both the test and the code under
// test therefore share any design error between them.
//
// What it does catch is drift. A change to the derivation order, an HKDF
// info string, or which key ends up bound to which direction alters what
// these functions produce without necessarily breaking the build or
// stopping the handshake completing end to end. This test is what notices.
//
// Checking the design itself against TLS 1.3's published vectors, RFC 9846,
// needs a reviewer other than the author. This test is not a substitute for
// that.
func TestKnownAnswerKeySchedule(t *testing.T) {
	clientEphPrivBytes := bytes.Repeat([]byte{0x01}, 32)
	daemonStaticPrivBytes := bytes.Repeat([]byte{0x02}, 32)
	daemonEphPrivBytes := bytes.Repeat([]byte{0x03}, 32)

	clientEphPriv, err := curve().NewPrivateKey(clientEphPrivBytes)
	if err != nil {
		t.Fatalf("constructing client ephemeral private key: %v", err)
	}
	daemonStaticPriv, err := curve().NewPrivateKey(daemonStaticPrivBytes)
	if err != nil {
		t.Fatalf("constructing daemon static private key: %v", err)
	}
	daemonEphPriv, err := curve().NewPrivateKey(daemonEphPrivBytes)
	if err != nil {
		t.Fatalf("constructing daemon ephemeral private key: %v", err)
	}

	clientEphPub := clientEphPriv.PublicKey()
	daemonStaticPub := daemonStaticPriv.PublicKey()
	daemonEphPub := daemonEphPriv.PublicKey()

	wantClientEphPub := mustDecodeHex(t, "a4e09292b651c278b9772c569f5fa9bb13d906b46ab68c9df9dc2b4409f8a209")
	wantDaemonStaticPub := mustDecodeHex(t, "ce8d3ad1ccb633ec7b70c17814a5c76ecd029685050d344745ba05870e587d59")
	if !bytes.Equal(clientEphPub.Bytes(), wantClientEphPub) {
		t.Errorf("clientEphPub = %x, want %x", clientEphPub.Bytes(), wantClientEphPub)
	}
	if !bytes.Equal(daemonStaticPub.Bytes(), wantDaemonStaticPub) {
		t.Errorf("daemonStaticPub = %x, want %x", daemonStaticPub.Bytes(), wantDaemonStaticPub)
	}

	// Two exchanges concatenated, ephemeral first, exactly as both
	// handshake halves build it.
	ephShared, err := daemonEphPriv.ECDH(clientEphPub)
	if err != nil {
		t.Fatalf("ephemeral ECDH: %v", err)
	}
	staticShared, err := daemonStaticPriv.ECDH(clientEphPub)
	if err != nil {
		t.Fatalf("static ECDH: %v", err)
	}
	sharedSecret := append(append([]byte{}, ephShared...), staticShared...)
	wantSharedSecret := mustDecodeHex(t, "b0190541a2abb8bc163b6eef0fae7ba0179004b444326f73809563164d47c521"+
		"2ed76ab549b1e73c031eb49c9448f0798aea81b698279a0c3dc3e49fbfc4b953")
	if !bytes.Equal(sharedSecret, wantSharedSecret) {
		t.Errorf("sharedSecret = %x, want %x", sharedSecret, wantSharedSecret)
	}

	// Both sides of a real handshake compute this same shared secret from
	// opposite ends of the same ECDH computation; confirming that symmetry
	// holds here is as important as confirming the value itself matches
	// the frozen known answer.
	clientEph, err := clientEphPriv.ECDH(daemonEphPub)
	if err != nil {
		t.Fatalf("ephemeral ECDH (client side): %v", err)
	}
	clientStatic, err := clientEphPriv.ECDH(daemonStaticPub)
	if err != nil {
		t.Fatalf("static ECDH (client side): %v", err)
	}
	sharedSecretFromClientSide := append(append([]byte{}, clientEph...), clientStatic...)
	if !bytes.Equal(sharedSecret, sharedSecretFromClientSide) {
		t.Errorf("shared secret computed from the daemon side (%x) does not match the client side (%x)", sharedSecret, sharedSecretFromClientSide)
	}

	transcriptHash := transcript(clientEphPub, daemonEphPub, daemonStaticPub)
	wantTranscriptHash := mustDecodeHex(t, "77cc4b669be4ba579c94223e054e81f9716d8c72a7a0ffb32867d45ba9d21cc1")
	if !bytes.Equal(transcriptHash[:], wantTranscriptHash) {
		t.Errorf("transcriptHash = %x, want %x", transcriptHash[:], wantTranscriptHash)
	}

	keys, err := deriveKeys(sharedSecret, transcriptHash)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"daemonToClientKey", keys.daemonToClientKey[:], "9da75e7238ecfba74e8527540d3fb30afb755521428e8e2094076258d72cb5df"},
		{"daemonToClientNonceBase", keys.daemonToClientNonceBase[:], "d9d502f3e2365c111b5c704f"},
		{"clientToDaemonKey", keys.clientToDaemonKey[:], "221714f603538a548e23557b2805d8d504b9768fbc5a71a53e98f8c86b60b06f"},
		{"clientToDaemonNonceBase", keys.clientToDaemonNonceBase[:], "c07f8c97373b028695fab6b5"},
		{"confirmationKey", keys.confirmationKey[:], "ad32a210197b45f32e5b460e1e7d63d69c97349d3ebf1c436ee5244c0308e5c1"},
	}
	for _, c := range cases {
		want := mustDecodeHex(t, c.want)
		if !bytes.Equal(c.got, want) {
			t.Errorf("%s = %x, want %x", c.name, c.got, want)
		}
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decoding known-answer hex constant %q: %v", s, err)
	}
	return b
}
