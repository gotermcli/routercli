// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import "testing"

// TestNewSessionReturnsUnauthenticatedWithNoCommandLevel - This test
// verifies that NewSession returns a Session with every field left at its
// zero value, in particular Authenticated false and CommandLevel empty,
// since NewSession's doc comment states that setting CommandLevel to the
// base level's Name is main.go's job, not this constructor's.
func TestNewSessionReturnsUnauthenticatedWithNoCommandLevel(t *testing.T) {
	s := NewSession()
	if s == nil {
		t.Fatal("expected NewSession to return a non-nil Session")
	}
	if s.Authenticated {
		t.Error("expected a freshly constructed Session to be unauthenticated")
	}
	if s.Username != "" {
		t.Errorf("Username = %q, want empty", s.Username)
	}
	if s.CommandLevel != "" {
		t.Errorf("CommandLevel = %q, want empty", s.CommandLevel)
	}
}

// TestAtLevelTrueForMatchingName - This test verifies that AtLevel reports
// true when CommandLevel exactly matches the name asked about.
func TestAtLevelTrueForMatchingName(t *testing.T) {
	s := &Session{CommandLevel: "exec"}
	if !s.AtLevel("exec") {
		t.Error("expected AtLevel(\"exec\") to be true when CommandLevel is \"exec\"")
	}
}

// TestAtLevelFalseForDifferentName - This test verifies that AtLevel
// reports false for any name other than the session's actual CommandLevel,
// including a name that is a prefix or superstring of it, since this is an
// exact comparison, not a prefix match.
func TestAtLevelFalseForDifferentName(t *testing.T) {
	s := &Session{CommandLevel: "exec"}
	for _, name := range []string{"base", "exe", "execute", ""} {
		if s.AtLevel(name) {
			t.Errorf("expected AtLevel(%q) to be false when CommandLevel is \"exec\"", name)
		}
	}
}
