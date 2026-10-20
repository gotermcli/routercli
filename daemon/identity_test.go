// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateStaticKeyPairGeneratesAndPersists - This test verifies
// that a fresh privateKeyPath, nothing on disk yet, produces a freshly
// generated key pair, and writes both halves to disk: the private key at
// privateKeyPath, restrictive permissions, and the public key right beside
// it at privateKeyPath+".pub", world readable permissions.
func TestLoadOrCreateStaticKeyPairGeneratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routercli.sock.key")

	key, err := LoadOrCreateStaticKeyPair(path)
	if err != nil {
		t.Fatalf("LoadOrCreateStaticKeyPair: %v", err)
	}

	privInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected the private key file to exist, os.Stat returned: %v", err)
	}
	if got := privInfo.Mode().Perm(); got != staticPrivateKeyFilePermissions {
		t.Errorf("private key file mode = %o, want %o", got, staticPrivateKeyFilePermissions)
	}

	pubInfo, err := os.Stat(path + ".pub")
	if err != nil {
		t.Fatalf("expected the public key file to exist, os.Stat returned: %v", err)
	}
	if got := pubInfo.Mode().Perm(); got != staticPublicKeyFilePermissions {
		t.Errorf("public key file mode = %o, want %o", got, staticPublicKeyFilePermissions)
	}

	pub, err := ReadStaticPublicKey(path + ".pub")
	if err != nil {
		t.Fatalf("ReadStaticPublicKey: %v", err)
	}
	if !bytes.Equal(pub.Bytes(), key.PublicKey().Bytes()) {
		t.Error("ReadStaticPublicKey did not return the same public key LoadOrCreateStaticKeyPair generated")
	}
}

// TestLoadOrCreateStaticKeyPairReusesAnExistingKey - This test verifies
// that a second call against the same privateKeyPath returns the exact
// same key pair the first call generated, rather than silently generating
// a new one every time this daemon starts, which would break every CLI
// client's already distributed public key file the moment the daemon
// restarted.
func TestLoadOrCreateStaticKeyPairReusesAnExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routercli.sock.key")

	first, err := LoadOrCreateStaticKeyPair(path)
	if err != nil {
		t.Fatalf("first LoadOrCreateStaticKeyPair: %v", err)
	}
	second, err := LoadOrCreateStaticKeyPair(path)
	if err != nil {
		t.Fatalf("second LoadOrCreateStaticKeyPair: %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("expected the second call to reuse the first call's persisted key, got a different one")
	}
}

// TestStaticKeyPathAppendsDotKey - This test verifies StaticKeyPath's,
// trivial but load-bearing convention: every caller, a real daemon and a
// real CLI client alike, must derive the exact same path from the same
// socket path with no separate configuration field.
func TestStaticKeyPathAppendsDotKey(t *testing.T) {
	if got, want := StaticKeyPath("/var/run/routercli/routercli.sock"), "/var/run/routercli/routercli.sock.key"; got != want {
		t.Errorf("StaticKeyPath = %q, want %q", got, want)
	}
}

// TestReadStaticPublicKeyMissingFileReturnsError - This test verifies that
// reading a public key file that was never created fails with a clear
// error rather than a nil pointer or an empty key.
func TestReadStaticPublicKeyMissingFileReturnsError(t *testing.T) {
	_, err := ReadStaticPublicKey(filepath.Join(t.TempDir(), "does-not-exist.pub"))
	if err == nil {
		t.Fatal("expected an error reading a nonexistent public key file, got nil")
	}
}
