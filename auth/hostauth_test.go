// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"os/user"
	"testing"
	"time"
)

// TestSessionFromHostIdentityReturnsTheRealOSAccount - This test verifies
// that SessionFromHostIdentity returns the operating system account this
// process runs as, on both Username and HostUsername, marks the session
// Authenticated, and stamps HostConnectedAt between the start of the test and
// the call returning.
//
// No substitute identity is possible here. os/user.Current is not behind the
// Provider seam, because this function trusts the operating system directly,
// which is the whole point of it. So the test checks against the one real
// account the environment provides.
func TestSessionFromHostIdentityReturnsTheRealOSAccount(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine the current OS user in this environment: %v", err)
	}

	before := time.Now()
	session, serr := SessionFromHostIdentity()
	after := time.Now()

	if serr != nil {
		t.Fatalf("SessionFromHostIdentity returned unexpected error: %v", serr)
	}
	if session.Username != u.Username {
		t.Errorf("session.Username = %q, want %q", session.Username, u.Username)
	}
	if session.HostUsername != u.Username {
		t.Errorf("session.HostUsername = %q, want %q", session.HostUsername, u.Username)
	}
	if !session.Authenticated {
		t.Error("expected a host-identity session to be marked Authenticated")
	}
	if session.HostConnectedAt.Before(before) || session.HostConnectedAt.After(after) {
		t.Errorf("session.HostConnectedAt = %v, want a time between %v and %v", session.HostConnectedAt, before, after)
	}
}
