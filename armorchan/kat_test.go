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

// TestKnownAnswerKeySchedule - This is a known answer test, not an
// independently authored, second implementation checked against this one;
// both fix the same inputs and run them through this package's transcript
// and deriveKeys functions. Its real value is regression protection: every
// expected value below was captured from a verified-correct run of this
// exact implementation and frozen here, so an accidental future change to
// the derivation order, an HKDF info string, which key ends up bound to
// which direction, or any other detail of this package's key schedule,
// changes what this test produces without necessarily changing whether it
// still compiles or whether the handshake still completes end to end, and
// this test is what catches that. Confirming the key schedule's design
// against TLS 1.3's published test vectors, RFC 9846, is exactly the kind
// of independent check this package's doc comment recommends an outside
// reviewer perform before real deployment; this test is not a substitute
// for that review.
func TestKnownAnswerKeySchedule(t *testing.T) {
	clientEphPrivBytes := bytes.Repeat([]byte{0x01}, 32)
	daemonStaticPrivBytes := bytes.Repeat([]byte{0x02}, 32)

	clientEphPriv, err := curve().NewPrivateKey(clientEphPrivBytes)
	if err != nil {
		t.Fatalf("constructing client ephemeral private key: %v", err)
	}
	daemonStaticPriv, err := curve().NewPrivateKey(daemonStaticPrivBytes)
	if err != nil {
		t.Fatalf("constructing daemon static private key: %v", err)
	}

	clientEphPub := clientEphPriv.PublicKey()
	daemonStaticPub := daemonStaticPriv.PublicKey()

	wantClientEphPub := mustDecodeHex(t, "a4e09292b651c278b9772c569f5fa9bb13d906b46ab68c9df9dc2b4409f8a209")
	wantDaemonStaticPub := mustDecodeHex(t, "ce8d3ad1ccb633ec7b70c17814a5c76ecd029685050d344745ba05870e587d59")
	if !bytes.Equal(clientEphPub.Bytes(), wantClientEphPub) {
		t.Errorf("clientEphPub = %x, want %x", clientEphPub.Bytes(), wantClientEphPub)
	}
	if !bytes.Equal(daemonStaticPub.Bytes(), wantDaemonStaticPub) {
		t.Errorf("daemonStaticPub = %x, want %x", daemonStaticPub.Bytes(), wantDaemonStaticPub)
	}

	sharedSecret, err := daemonStaticPriv.ECDH(clientEphPub)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}
	wantSharedSecret := mustDecodeHex(t, "2ed76ab549b1e73c031eb49c9448f0798aea81b698279a0c3dc3e49fbfc4b953")
	if !bytes.Equal(sharedSecret, wantSharedSecret) {
		t.Errorf("sharedSecret = %x, want %x", sharedSecret, wantSharedSecret)
	}

	// Both sides of a real handshake compute this same shared secret from
	// opposite ends of the same ECDH computation; confirming that symmetry
	// holds here is as important as confirming the value itself matches
	// the frozen known answer.
	sharedSecretFromClientSide, err := clientEphPriv.ECDH(daemonStaticPub)
	if err != nil {
		t.Fatalf("ECDH (client side): %v", err)
	}
	if !bytes.Equal(sharedSecret, sharedSecretFromClientSide) {
		t.Errorf("shared secret computed from the daemon side (%x) does not match the client side (%x)", sharedSecret, sharedSecretFromClientSide)
	}

	transcriptHash := transcript(clientEphPub, daemonStaticPub)
	wantTranscriptHash := mustDecodeHex(t, "5339f2339b79eddd1f77a615d29af13e87f87a1d4697e72cd5b0d79de459727c")
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
		{"daemonToClientKey", keys.daemonToClientKey[:], "3b124d0c8517374907014861a1e644deecba396cf0f879a7d73a1cf257e028d2"},
		{"daemonToClientNonceBase", keys.daemonToClientNonceBase[:], "f62b8b6e45560ffd0cd3fe79"},
		{"clientToDaemonKey", keys.clientToDaemonKey[:], "dd6814008f71f9bbeab578e35f28c8dbc1258affbbb854a7e432ee4741785431"},
		{"clientToDaemonNonceBase", keys.clientToDaemonNonceBase[:], "7168d27795ba0a0930f82e23"},
		{"confirmationKey", keys.confirmationKey[:], "777750da478614b166021a884415170ccc6b13ad9e59babf897790ef0a7d0fb0"},
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
