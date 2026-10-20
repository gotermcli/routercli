// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"strings"
	"testing"
)

// containsViolation - This function reports whether want appears anywhere
// in got, the shared helper every table test below uses to check
// ValidatePassword's returned slice without depending on the order
// violations are reported in.
func containsViolation(got []PasswordViolation, want PasswordViolation) bool {
	for _, v := range got {
		if v == want {
			return true
		}
	}
	return false
}

// TestValidatePasswordAcceptsAPasswordSatisfyingEveryRule - This test
// verifies that a password meeting the minimum length and every
// composition rule, all three turned on at once, returns no violations at
// all.
func TestValidatePasswordAcceptsAPasswordSatisfyingEveryRule(t *testing.T) {
	policy := PasswordPolicy{MinLength: 10, RequireUppercase: true, RequireNumbers: true, RequireSpecialChars: true}
	violations := ValidatePassword("Str0ng!Passw0rd", policy)
	if violations != nil {
		t.Errorf("ValidatePassword() = %v, want nil", violations)
	}
}

// TestValidatePasswordAcceptsAPlainPasswordWithNoComplexityRequired - This
// test verifies that a password satisfying only the minimum length, with
// every PasswordPolicy composition flag left off, is accepted, the shipped
// default policy shape, length as the only control, no mandatory
// composition rules.
func TestValidatePasswordAcceptsAPlainPasswordWithNoComplexityRequired(t *testing.T) {
	policy := PasswordPolicy{MinLength: 10}
	violations := ValidatePassword("just some words", policy)
	if violations != nil {
		t.Errorf("ValidatePassword() = %v, want nil", violations)
	}
}

// TestValidatePasswordTooShort - This test verifies that a candidate
// shorter than PasswordPolicy.MinLength reports PasswordViolationTooShort.
func TestValidatePasswordTooShort(t *testing.T) {
	policy := PasswordPolicy{MinLength: 10}
	violations := ValidatePassword("short", policy)
	if !containsViolation(violations, PasswordViolationTooShort) {
		t.Errorf("ValidatePassword(%q) = %v, want it to contain %v", "short", violations, PasswordViolationTooShort)
	}
}

// TestValidatePasswordExactlyAtMinLengthPasses - This test verifies the
// boundary case, a candidate exactly PasswordPolicy.MinLength characters
// long, does not report PasswordViolationTooShort.
func TestValidatePasswordExactlyAtMinLengthPasses(t *testing.T) {
	policy := PasswordPolicy{MinLength: 10}
	candidate := "1234567890" // exactly 10 characters
	violations := ValidatePassword(candidate, policy)
	if containsViolation(violations, PasswordViolationTooShort) {
		t.Errorf("ValidatePassword(%q) = %v, want no %v at exactly MinLength", candidate, violations, PasswordViolationTooShort)
	}
}

// TestValidatePasswordOneShortOfMinLengthFails - This test verifies the
// other side of the same boundary, one character short of
// PasswordPolicy.MinLength, does report PasswordViolationTooShort.
func TestValidatePasswordOneShortOfMinLengthFails(t *testing.T) {
	policy := PasswordPolicy{MinLength: 10}
	candidate := "123456789" // exactly 9 characters
	violations := ValidatePassword(candidate, policy)
	if !containsViolation(violations, PasswordViolationTooShort) {
		t.Errorf("ValidatePassword(%q) = %v, want it to contain %v one character short of MinLength", candidate, violations, PasswordViolationTooShort)
	}
}

// TestValidatePasswordLengthCountsRunesNotBytes - This test verifies that
// MinLength is measured in runes, not raw bytes, so a password built from
// multi-byte UTF-8 characters is judged by how many characters it visibly
// has, not penalized for their encoded size. "élève" is 5 runes but more
// than 5 bytes once encoded as UTF-8.
func TestValidatePasswordLengthCountsRunesNotBytes(t *testing.T) {
	policy := PasswordPolicy{MinLength: 5}
	candidate := "élève"
	if len(candidate) <= 5 {
		t.Fatalf("test fixture %q is not actually longer in bytes than in runes, byte length %d", candidate, len(candidate))
	}
	violations := ValidatePassword(candidate, policy)
	if containsViolation(violations, PasswordViolationTooShort) {
		t.Errorf("ValidatePassword(%q) = %v, want no %v when the rune count already meets MinLength", candidate, violations, PasswordViolationTooShort)
	}
}

// TestValidatePasswordTooLong - This test verifies that a candidate longer
// than MaxPasswordLength reports PasswordViolationTooLong, regardless of
// how permissive the PasswordPolicy itself is.
func TestValidatePasswordTooLong(t *testing.T) {
	policy := PasswordPolicy{MinLength: 1}
	candidate := strings.Repeat("a", MaxPasswordLength+1)
	violations := ValidatePassword(candidate, policy)
	if !containsViolation(violations, PasswordViolationTooLong) {
		t.Errorf("ValidatePassword() with a %d byte candidate = %v, want it to contain %v", len(candidate), violations, PasswordViolationTooLong)
	}
}

// TestValidatePasswordExactlyAtMaxLengthPasses - This test verifies the
// boundary case, a candidate exactly MaxPasswordLength bytes long, does
// not report PasswordViolationTooLong.
func TestValidatePasswordExactlyAtMaxLengthPasses(t *testing.T) {
	policy := PasswordPolicy{MinLength: 1}
	candidate := strings.Repeat("a", MaxPasswordLength)
	violations := ValidatePassword(candidate, policy)
	if containsViolation(violations, PasswordViolationTooLong) {
		t.Errorf("ValidatePassword() with a %d byte candidate = %v, want no %v at exactly MaxPasswordLength", len(candidate), violations, PasswordViolationTooLong)
	}
}

// TestValidatePasswordMaxLengthIsCheckedInBytesNotRunes - This test
// verifies that MaxPasswordLength is checked against raw byte length, not
// rune count, since that is what bcrypt's 72 byte input limit counts. A
// candidate with fewer than MaxPasswordLength runes can still exceed
// MaxPasswordLength bytes once multi-byte characters are involved, and
// must still be rejected.
func TestValidatePasswordMaxLengthIsCheckedInBytesNotRunes(t *testing.T) {
	policy := PasswordPolicy{MinLength: 1}
	// Each "é" encodes as 2 bytes in UTF-8. 40 of them is 40 runes but 80
	// bytes, comfortably past MaxPasswordLength in bytes while nowhere
	// near it in runes.
	candidate := strings.Repeat("é", 40)
	if len(candidate) <= MaxPasswordLength {
		t.Fatalf("test fixture %q is not actually longer in bytes than MaxPasswordLength, byte length %d", candidate, len(candidate))
	}
	violations := ValidatePassword(candidate, policy)
	if !containsViolation(violations, PasswordViolationTooLong) {
		t.Errorf("ValidatePassword() with a %d byte, %d rune candidate = %v, want it to contain %v", len(candidate), len([]rune(candidate)), violations, PasswordViolationTooLong)
	}
}

// TestValidatePasswordRequireUppercase - This test table drives
// RequireUppercase across a candidate with an uppercase letter and one
// entirely without, confirming the violation fires only when the rule is
// both enabled and unmet.
func TestValidatePasswordRequireUppercase(t *testing.T) {
	cases := []struct {
		name          string
		candidate     string
		wantViolation bool
	}{
		{name: "has uppercase", candidate: "Password1", wantViolation: false},
		{name: "no uppercase", candidate: "password1", wantViolation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := PasswordPolicy{MinLength: 1, RequireUppercase: true}
			violations := ValidatePassword(tc.candidate, policy)
			got := containsViolation(violations, PasswordViolationNeedsUppercase)
			if got != tc.wantViolation {
				t.Errorf("ValidatePassword(%q) violation present = %v, want %v", tc.candidate, got, tc.wantViolation)
			}
		})
	}
}

// TestValidatePasswordUppercaseNotCheckedWhenPolicyDoesNotRequireIt - This
// test verifies that a candidate with no uppercase letter at all reports
// no PasswordViolationNeedsUppercase when RequireUppercase is left off,
// confirming the rule is opt in, not always enforced.
func TestValidatePasswordUppercaseNotCheckedWhenPolicyDoesNotRequireIt(t *testing.T) {
	policy := PasswordPolicy{MinLength: 1}
	violations := ValidatePassword("all lowercase", policy)
	if containsViolation(violations, PasswordViolationNeedsUppercase) {
		t.Errorf("ValidatePassword() = %v, want no %v when RequireUppercase is false", violations, PasswordViolationNeedsUppercase)
	}
}

// TestValidatePasswordRequireNumbers - This test table drives
// RequireNumbers the same way TestValidatePasswordRequireUppercase drives
// RequireUppercase.
func TestValidatePasswordRequireNumbers(t *testing.T) {
	cases := []struct {
		name          string
		candidate     string
		wantViolation bool
	}{
		{name: "has a digit", candidate: "Password1", wantViolation: false},
		{name: "no digit", candidate: "Password", wantViolation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := PasswordPolicy{MinLength: 1, RequireNumbers: true}
			violations := ValidatePassword(tc.candidate, policy)
			got := containsViolation(violations, PasswordViolationNeedsNumber)
			if got != tc.wantViolation {
				t.Errorf("ValidatePassword(%q) violation present = %v, want %v", tc.candidate, got, tc.wantViolation)
			}
		})
	}
}

// TestValidatePasswordRequireSpecialChars - This test table drives
// RequireSpecialChars across punctuation, a symbol, and a candidate with
// neither, confirming isSpecialChar's Unicode punctuation and symbol
// classification rather than a fixed ASCII list.
func TestValidatePasswordRequireSpecialChars(t *testing.T) {
	cases := []struct {
		name          string
		candidate     string
		wantViolation bool
	}{
		{name: "has ASCII punctuation", candidate: "Password1!", wantViolation: false},
		{name: "has a Unicode symbol", candidate: "Password1€", wantViolation: false},
		{name: "no special character", candidate: "Password1", wantViolation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := PasswordPolicy{MinLength: 1, RequireSpecialChars: true}
			violations := ValidatePassword(tc.candidate, policy)
			got := containsViolation(violations, PasswordViolationNeedsSpecialChar)
			if got != tc.wantViolation {
				t.Errorf("ValidatePassword(%q) violation present = %v, want %v", tc.candidate, got, tc.wantViolation)
			}
		})
	}
}

// TestValidatePasswordReportsEveryViolationTogether - This test verifies
// that a candidate failing several rules at once, too short and missing
// every required composition class, gets every matching violation back in
// one call, rather than only the first one encountered, so a session sees
// everything wrong with a rejected password at once.
func TestValidatePasswordReportsEveryViolationTogether(t *testing.T) {
	policy := PasswordPolicy{MinLength: 20, RequireUppercase: true, RequireNumbers: true, RequireSpecialChars: true}
	violations := ValidatePassword("short", policy)

	want := []PasswordViolation{
		PasswordViolationTooShort,
		PasswordViolationNeedsUppercase,
		PasswordViolationNeedsNumber,
		PasswordViolationNeedsSpecialChar,
	}
	for _, w := range want {
		if !containsViolation(violations, w) {
			t.Errorf("ValidatePassword() = %v, want it to contain %v", violations, w)
		}
	}
	if len(violations) != len(want) {
		t.Errorf("ValidatePassword() returned %d violations %v, want exactly %d", len(violations), violations, len(want))
	}
}

// TestValidatePasswordTooLongIsCheckedAlongsideEveryOtherRule - This test
// verifies that PasswordViolationTooLong is reported together with any
// other failing rule, not treated as an exclusive, short-circuiting case,
// matching ValidatePassword's documented behavior of checking and
// reporting every rule together.
func TestValidatePasswordTooLongIsCheckedAlongsideEveryOtherRule(t *testing.T) {
	policy := PasswordPolicy{MinLength: 1, RequireUppercase: true}
	candidate := strings.Repeat("a", MaxPasswordLength+1) // too long and all lowercase
	violations := ValidatePassword(candidate, policy)

	if !containsViolation(violations, PasswordViolationTooLong) {
		t.Errorf("ValidatePassword() = %v, want it to contain %v", violations, PasswordViolationTooLong)
	}
	if !containsViolation(violations, PasswordViolationNeedsUppercase) {
		t.Errorf("ValidatePassword() = %v, want it to contain %v", violations, PasswordViolationNeedsUppercase)
	}
}
