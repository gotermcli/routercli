// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	return path
}

// TestLoadUsersValid - This test verifies that a well-formed users.yaml
// loads into a Users map keyed by username, with each User's Username
// field populated from that same map key.
func TestLoadUsersValid(t *testing.T) {
	yaml := `
users:
  admin:
    password: "$0$adminpass"
  bob:
    password: "$0$bobpass"
`
	path := writeTempFile(t, "users.yaml", yaml)
	users, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers returned unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	if users["admin"].Username != "admin" {
		t.Errorf("Username not populated from map key: got %q", users["admin"].Username)
	}
}

// TestLoadUsersMissingPasswordIsError - This test verifies that a user
// entry with no password field is rejected rather than silently loaded
// with an empty hash.
func TestLoadUsersMissingPasswordIsError(t *testing.T) {
	yaml := `
users:
  ghost: {}
`
	path := writeTempFile(t, "users.yaml", yaml)
	_, err := LoadUsers(path)
	if err == nil {
		t.Fatal("expected an error for a user with no password set, got nil")
	}
}

// TestLoadUsersMissingFile - This test verifies that a path with no file
// on disk returns an error instead of an empty Users map.
func TestLoadUsersMissingFile(t *testing.T) {
	_, err := LoadUsers("/nonexistent/users.yaml")
	if err == nil {
		t.Fatal("expected an error for a missing users file, got nil")
	}
}

// TestLoadUsersUnknownFieldIsError - This test verifies that users.yaml
// uses the same strict, KnownFields(true) parsing that
// config.LoadSystemConfig uses for its own configuration file. A
// misspelled field name here, shown below, would otherwise be silently
// dropped rather than erroring, which is a worse mistake in this specific
// file than almost anywhere else in the project, since it would look like
// a secret was configured when it was not.
func TestLoadUsersUnknownFieldIsError(t *testing.T) {
	yaml := `
users:
  admin:
    password: "$0$adminpass"
    enable_passwrod: "$0$typoed-field-name"
`
	path := writeTempFile(t, "users.yaml", yaml)
	_, err := LoadUsers(path)
	if err == nil {
		t.Fatal("expected an error for an unknown field (misspelled enable_passwrod), got nil")
	}
}

// TestLoadUsersEmptyFileIsNotError - This test verifies that a completely
// empty users.yaml is a valid, zero user configuration rather than an
// error.
func TestLoadUsersEmptyFileIsNotError(t *testing.T) {
	path := writeTempFile(t, "users.yaml", "")
	users, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers returned unexpected error for an empty file: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected an empty file to produce zero users, got %d", len(users))
	}
}

// TestLoadUsersMultipleDocumentsIsError - This test verifies that a
// users.yaml containing more than one "---" separated YAML document is
// rejected, rather than silently loading only the first document.
func TestLoadUsersMultipleDocumentsIsError(t *testing.T) {
	yaml := `
users:
  admin:
    password: "$0$adminpass"
---
users:
  bob:
    password: "$0$bobpass"
`
	path := writeTempFile(t, "users.yaml", yaml)
	_, err := LoadUsers(path)
	if err == nil {
		t.Fatal("expected an error for a users file containing multiple YAML documents, got nil")
	}
}

// TestSaveUsersRoundTrip - This test verifies that SaveUsers writes a file
// LoadUsers can read back, preserving every field on every user, including
// a TOTPSecret, and that Username, not serialized, see User's yaml:"-"
// tag, comes back populated from the map key exactly the way LoadUsers
// always sets it.
func TestSaveUsersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")
	users := Users{
		"alice": {Username: "alice", PasswordHash: "$0$alicepass", TOTPSecret: "JBSWY3DPEHPK3PXP"},
		"bob":   {Username: "bob", PasswordHash: "$0$bobpass"},
	}

	if err := SaveUsers(path, users); err != nil {
		t.Fatalf("SaveUsers returned unexpected error: %v", err)
	}

	loaded, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers returned unexpected error reading the saved file: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("got %d users after round trip, want 2", len(loaded))
	}
	if loaded["alice"].Username != "alice" || loaded["alice"].PasswordHash != "$0$alicepass" || loaded["alice"].TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("alice round-tripped incorrectly: %+v", loaded["alice"])
	}
	if loaded["bob"].Username != "bob" || loaded["bob"].PasswordHash != "$0$bobpass" || loaded["bob"].TOTPSecret != "" {
		t.Errorf("bob round-tripped incorrectly: %+v", loaded["bob"])
	}
}

// TestSaveUsersOmitsEmptyTOTPSecret - This test verifies that a user with
// no TOTPSecret set produces a saved file with no totp_secret key at all,
// rather than an explicit empty string, keeping a plain password-only
// user's entry looking exactly the way an administrator would have hand
// written it. See User's "totp_secret,omitempty" yaml tag.
func TestSaveUsersOmitsEmptyTOTPSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")
	users := Users{"bob": {Username: "bob", PasswordHash: "$0$bobpass"}}

	if err := SaveUsers(path, users); err != nil {
		t.Fatalf("SaveUsers returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved users file: %v", err)
	}
	if strings.Contains(string(data), "totp_secret") {
		t.Errorf("expected no totp_secret key for a user with no TOTPSecret set, got:\n%s", data)
	}
}

// TestSaveUsersOverwritesExistingFile - This test verifies that calling
// SaveUsers a second time against the same path replaces the file's
// contents entirely, rather than merging with or appending to what was
// there before, since totp disable relies on this to make a cleared
// TOTPSecret disappear from disk, not just from the newly added entries.
func TestSaveUsersOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")

	first := Users{
		"alice": {Username: "alice", PasswordHash: "$0$alicepass", TOTPSecret: "JBSWY3DPEHPK3PXP"},
		"bob":   {Username: "bob", PasswordHash: "$0$bobpass"},
	}
	if err := SaveUsers(path, first); err != nil {
		t.Fatalf("first SaveUsers call returned unexpected error: %v", err)
	}

	second := Users{
		"alice": {Username: "alice", PasswordHash: "$0$alicepass"}, // TOTPSecret cleared
	}
	if err := SaveUsers(path, second); err != nil {
		t.Fatalf("second SaveUsers call returned unexpected error: %v", err)
	}

	loaded, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("LoadUsers returned unexpected error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("got %d users after overwrite, want 1 (bob should be gone)", len(loaded))
	}
	if loaded["alice"].TOTPSecret != "" {
		t.Errorf("expected alice's TOTPSecret to be cleared after the second save, got %q", loaded["alice"].TOTPSecret)
	}
}

// TestSaveUsersErrorsWhenDirectoryDoesNotExist - This test verifies that
// SaveUsers returns an error, rather than panicking or silently doing
// nothing, when the target directory does not exist, the one realistic way
// the temporary-file-plus-rename write in SaveUsers can fail.
func TestSaveUsersErrorsWhenDirectoryDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent-subdir", "users.yaml")
	err := SaveUsers(path, Users{"bob": {Username: "bob", PasswordHash: "$0$bobpass"}})
	if err == nil {
		t.Fatal("expected an error saving to a directory that does not exist, got nil")
	}
}
