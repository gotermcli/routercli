// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"crypto/ecdh"
	"fmt"
	"os"

	"github.com/gotermcli/routercli/armorchan"
)

// staticPrivateKeyFilePermissions is the file mode a real daemon's
// persisted static private key is written with, restrictive, matching this
// package's socketPermissions: whoever can read this file can impersonate
// this daemon to every connecting client, exactly the same stakes as the
// socket itself.
const staticPrivateKeyFilePermissions = 0o600

// staticPublicKeyFilePermissions is the file mode the matching public key
// file is written with, world readable, matching this project's handshake
// design: "its public half written to a world readable file alongside the
// socket path so a CLI client can read it before ever connecting."
const staticPublicKeyFilePermissions = 0o644

// StaticKeyPath returns the file path this daemon's persisted static
// private key is read from and written to, derived from socketPath,
// config.SystemConfig.DaemonSocketPath, so a daemon and every CLI client
// configured against the same socket path always agree on where to find it
// with no separate configuration field of its own needed. The matching
// public key file sits right beside it, the same path with ".pub"
// appended, see LoadOrCreateStaticKeyPair and ReadStaticPublicKey.
func StaticKeyPath(socketPath string) string {
	return socketPath + ".key"
}

// LoadOrCreateStaticKeyPair returns this daemon's persisted static X25519
// identity, read from privateKeyPath, most StaticKeyPath's return value,
// if that file already exists, or generated fresh with
// armorchan.GenerateStaticKeyPair and written to privateKeyPath, and its
// public half to privateKeyPath+".pub", if it does not; see this project's
// handshake design: "The daemon holds one persisted static X25519 key
// pair, generated once at first startup and kept on disk." Reading and
// writing this key to disk is this package's concern, not armorchan's.
func LoadOrCreateStaticKeyPair(privateKeyPath string) (*ecdh.PrivateKey, error) {
	raw, err := os.ReadFile(privateKeyPath)
	if err == nil {
		key, kerr := ecdh.X25519().NewPrivateKey(raw)
		if kerr != nil {
			return nil, fmt.Errorf("daemon: %s does not hold a valid X25519 private key: %w", privateKeyPath, kerr)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("daemon: reading %s: %w", privateKeyPath, err)
	}

	key, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(privateKeyPath, key.Bytes(), staticPrivateKeyFilePermissions); err != nil {
		return nil, fmt.Errorf("daemon: writing %s: %w", privateKeyPath, err)
	}
	publicKeyPath := privateKeyPath + ".pub"
	if err := os.WriteFile(publicKeyPath, key.PublicKey().Bytes(), staticPublicKeyFilePermissions); err != nil {
		return nil, fmt.Errorf("daemon: writing %s: %w", publicKeyPath, err)
	}
	return key, nil
}

// ReadStaticPublicKey reads a RouterCLI daemon's static public key from
// publicKeyPath, previously written by LoadOrCreateStaticKeyPair to
// privateKeyPath+".pub", for a CLI client's use as
// expectedDaemonStaticPublic in Dial.
func ReadStaticPublicKey(publicKeyPath string) (*ecdh.PublicKey, error) {
	raw, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: reading %s: %w", publicKeyPath, err)
	}
	key, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("daemon: %s does not hold a valid X25519 public key: %w", publicKeyPath, err)
	}
	return key, nil
}
