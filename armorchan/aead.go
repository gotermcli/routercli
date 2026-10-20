// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package armorchan

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// newAEAD - This function builds the one authenticated cipher this package
// uses, AES-256-GCM, from a 32 byte key.
//
// Every key deriveKeys produces is exactly 32 bytes, so this is the only
// place a key size is assumed rather than checked. Even here aes.NewCipher
// would fail first on a wrong size.
func newAEAD(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("armorchan: constructing AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("armorchan: constructing GCM: %w", err)
	}
	return aead, nil
}
