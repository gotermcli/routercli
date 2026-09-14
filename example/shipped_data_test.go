// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package main

import (
	"path/filepath"
	"testing"

	"github.com/gotermcli/routercli/auth"
)

// TestShippedUsersFileHasWorkingTestAccounts - A bcrypt hash is opaque, so a
// typo made while hand editing one produces a users.yaml that loads without
// error, has the right shape, and silently locks the account out. Nothing but
// verifying a password against the hash catches that.
//
// This test loads this example's own example/etc/users.yaml and confirms the seeded
// admin account works with the password "testpass123".
//
// It lives here rather than in package auth because it exercises this
// example's data, not the library. The library's own tests use inline
// fixtures and prove LoadUsers handles arbitrary YAML correctly; this one
// proves the file actually shipped is usable.
func TestShippedUsersFileHasWorkingTestAccounts(t *testing.T) {
	users, err := auth.LoadUsers(filepath.Join("etc", "users.yaml"))
	if err != nil {
		t.Fatalf("failed to load this example's users.yaml: %v", err)
	}

	u, ok := users["admin"]
	if !ok {
		t.Fatal("expected admin to be defined in this example's users.yaml")
	}
	if !auth.VerifyPassword(u.PasswordHash, "testpass123") {
		t.Error("admin's password should verify against \"testpass123\"")
	}
}
