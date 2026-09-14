// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import "testing"

// TestNewAuthProviderLocalReturnsAWorkingLocalAuthProvider - This test
// verifies that NewProvider("local", users) returns a working
// *LocalProvider, checked by authenticating a known user through the
// returned Provider rather than only inspecting its concrete type, so a
// future change to NewLocalProvider's construction is still caught here if
// it ever stopped wiring users through correctly.
func TestNewAuthProviderLocalReturnsAWorkingLocalAuthProvider(t *testing.T) {
	users := testUsers(t)

	provider, err := NewProvider("local", users)
	if err != nil {
		t.Fatalf("NewProvider(\"local\", ...) returned unexpected error: %v", err)
	}

	ok, aerr := provider.Authenticate("alice", "s3cret")
	if aerr != nil {
		t.Fatalf("Authenticate returned unexpected error: %v", aerr)
	}
	if !ok {
		t.Error("expected the returned provider to authenticate the correct password for a known user")
	}
}

// TestNewAuthProviderUnrecognizedTypeIsAnError - This test verifies that a
// providerType with no matching case is a hard error rather than something
// silently ignored, the same fail loudly convention every other malformed
// setting in this project follows, see this function's doc comment.
func TestNewAuthProviderUnrecognizedTypeIsAnError(t *testing.T) {
	_, err := NewProvider("ldap", Users{})
	if err == nil {
		t.Fatal("expected an error for an unrecognized provider type, got nil")
	}
}
