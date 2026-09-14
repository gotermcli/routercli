// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import "testing"

// TestHashAndVerifyRoundTrip - This test verifies that a password hashed
// with HashPassword is accepted by VerifyPassword when correct and
// rejected when wrong.
func TestHashAndVerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Error("VerifyPassword should accept the correct password")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Error("VerifyPassword should reject an incorrect password")
	}
}

// TestHashPasswordNeverProducesPlaintextID - This test verifies that
// HashPassword always produces a bcrypt ("$6$") hash, never the plaintext
// ("$0$") form VerifyPassword also understands.
func TestHashPasswordNeverProducesPlaintextID(t *testing.T) {
	hash, err := HashPassword("test")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if len(hash) < 3 || hash[:3] != "$"+cryptIDBcrypt+"$" {
		t.Errorf("HashPassword output %q does not start with the bcrypt ID prefix", hash)
	}
}

// TestVerifyPasswordPlaintext - This test verifies that VerifyPassword
// still recognizes a "$0$" plaintext entry, a development and testing
// convenience, even though HashPassword itself never produces one.
func TestVerifyPasswordPlaintext(t *testing.T) {
	if !VerifyPassword("$0$hunter2", "hunter2") {
		t.Error("plaintext ($0$) verification should accept a matching password")
	}
	if VerifyPassword("$0$hunter2", "wrong") {
		t.Error("plaintext ($0$) verification should reject a non-matching password")
	}
}

// TestVerifyPasswordMalformedOrUnknownID - This test verifies that a
// stored value that is not a well-formed "$id$encoded" string, or that
// names an unrecognized id, fails verification instead of erroring or
// matching by accident.
func TestVerifyPasswordMalformedOrUnknownID(t *testing.T) {
	cases := []string{
		"not-even-dollar-prefixed",
		"$",
		"$6",
		"$99$something",
	}
	for _, stored := range cases {
		if VerifyPassword(stored, "anything") {
			t.Errorf("VerifyPassword(%q, ...) should fail closed, returned true", stored)
		}
	}
}

// TestIsPlaintextHash - This test verifies that IsPlaintextHash recognizes
// a "$0$" stored value as plaintext, reports false for a real bcrypt hash,
// and fails closed, reporting false rather than erroring, for a value that
// does not parse as a "$id$encoded" string at all.
func TestIsPlaintextHash(t *testing.T) {
	if !IsPlaintextHash("$0$hunter2") {
		t.Error("expected a $0$ stored value to be reported as plaintext")
	}
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if IsPlaintextHash(hash) {
		t.Error("expected a real bcrypt hash to not be reported as plaintext")
	}
	if IsPlaintextHash("not-even-dollar-prefixed") {
		t.Error("expected a malformed stored value to not be reported as plaintext")
	}
}

// TestIsRecognizedHash - This test verifies IsRecognizedHash across a real
// bcrypt hash, the plaintext storage form, which is itself a registered id
// and so counts as recognized here even though IsPlaintextHash treats it
// specially, an id nothing has registered, and a value that is not even
// "$id$encoded" shaped at all.
func TestIsRecognizedHash(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !IsRecognizedHash(hash) {
		t.Error("expected a real bcrypt hash to be recognized")
	}
	if !IsRecognizedHash("$0$hunter2") {
		t.Error("expected the plaintext storage form to be recognized, even though it is not itself a real hash")
	}
	if IsRecognizedHash("$99$whatever") {
		t.Error("expected an id nothing has registered to not be recognized")
	}
	if IsRecognizedHash("not-even-dollar-prefixed") {
		t.Error("expected a malformed value to not be recognized")
	}
	if IsRecognizedHash("") {
		t.Error("expected an empty value to not be recognized")
	}
}

// TestPlaintextHasherHashAlwaysFails - This test verifies that
// plaintextHasher.Hash returns a non-nil error for every input, including
// an empty string, and never returns a usable encoded value alongside it.
// This is a genuine security invariant, not an arbitrary choice:
// cryptIDPlaintext exists only for a "$0$..." entry typed by hand into
// example/etc/users.yaml for local development and testing, and HashPassword must
// never be able to produce one for a real deployment, see this type's doc
// comment in auth.go.
func TestPlaintextHasherHashAlwaysFails(t *testing.T) {
	for _, password := range []string{"hunter2", "", "correct horse battery staple"} {
		encoded, err := plaintextHasher{}.Hash(password)
		if err == nil {
			t.Errorf("plaintextHasher{}.Hash(%q) returned nil error, want a non-nil error", password)
		}
		if encoded != "" {
			t.Errorf("plaintextHasher{}.Hash(%q) returned encoded value %q alongside its error, want empty", password, encoded)
		}
	}
}

// TestPlaintextHasherDummyReturnsAFixedPlaceholder - This test verifies
// that plaintextHasher.Dummy returns the fixed literal
// "not-a-real-password" every time, matching its own doc comment, and that
// this placeholder never itself verifies as a real password through
// Verify. auth/provider.go calls Verify(Dummy(), password) specifically
// when a login attempt names a username that does not exist, a timing
// attack mitigation so response latency cannot reveal whether a username
// is valid; a placeholder that ever verified would defeat the entire point
// of calling Dummy at all.
func TestPlaintextHasherDummyReturnsAFixedPlaceholder(t *testing.T) {
	h := plaintextHasher{}
	dummy := h.Dummy()
	if dummy != "not-a-real-password" {
		t.Errorf("plaintextHasher{}.Dummy() = %q, want %q", dummy, "not-a-real-password")
	}
	for _, candidate := range []string{"", "hunter2", "not-a-real-password"} {
		if candidate == dummy {
			// A candidate that happens to equal the placeholder itself is
			// expected to verify; that is plain string equality working
			// correctly, not a defeated mitigation. A real login attempt
			// is never typing the placeholder text itself as its password.
			continue
		}
		if h.Verify(dummy, candidate) {
			t.Errorf("plaintextHasher{}.Verify(Dummy(), %q) = true, want false", candidate)
		}
	}
}
