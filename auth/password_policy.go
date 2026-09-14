// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"unicode"
	"unicode/utf8"
)

// ----------------------------------------------------------------------
// Public Functions - Password Policy
// ----------------------------------------------------------------------

// MaxPasswordLength - This constant is the longest password HashPassword
// can hash, not a policy choice an operator can raise or lower. bcrypt,
// the algorithm HashPassword uses, silently ignores any byte past the 72nd
// in its input, so accepting a longer password here would let someone
// believe two different passwords both work when bcrypt itself only ever
// saw and checked their common 72-byte prefix. ValidatePassword rejects
// anything longer than this before it ever reaches HashPassword, so that
// mismatch can never happen.
const MaxPasswordLength = 72

// ValidatePassword - This function checks candidate against policy and
// MaxPasswordLength, returning every rule it fails, or nil if it passes.
//
// Every rule is checked and reported together rather than stopping at the
// first, so a caller can tell someone everything wrong at once instead of
// walking them through one violation per attempt.
//
// Length is counted in runes, so a password using multi-byte characters is
// measured the way a person counting characters on screen would.
// MaxPasswordLength is the exception and is counted in bytes, since that
// is what bcrypt's limit counts.
//
// It performs no I/O and needs no Translator, so it can be tested directly
// against known inputs.
func ValidatePassword(candidate string, policy PasswordPolicy) []PasswordViolation {
	var violations []PasswordViolation

	if utf8.RuneCountInString(candidate) < policy.MinLength {
		violations = append(violations, PasswordViolationTooShort)
	}
	if len(candidate) > MaxPasswordLength {
		violations = append(violations, PasswordViolationTooLong)
	}
	if policy.RequireUppercase && !containsRuneMatching(candidate, unicode.IsUpper) {
		violations = append(violations, PasswordViolationNeedsUppercase)
	}
	if policy.RequireNumbers && !containsRuneMatching(candidate, unicode.IsDigit) {
		violations = append(violations, PasswordViolationNeedsNumber)
	}
	if policy.RequireSpecialChars && !containsRuneMatching(candidate, isSpecialChar) {
		violations = append(violations, PasswordViolationNeedsSpecialChar)
	}

	return violations
}

// ----------------------------------------------------------------------
// Private Functions - Password Policy
// ----------------------------------------------------------------------

// containsRuneMatching - This function reports whether any rune in s
// satisfies match, the shared loop ValidatePassword's three composition
// checks all use, each with a different match function.
func containsRuneMatching(s string, match func(rune) bool) bool {
	for _, r := range s {
		if match(r) {
			return true
		}
	}
	return false
}

// isSpecialChar - This function reports whether r counts as a "special
// character" for PasswordPolicy.RequireSpecialChars, defined here as
// Unicode punctuation or a Unicode symbol, for example "!", "@", or "$".
// This is broader than a fixed ASCII punctuation list, so a password using
// a non-ASCII symbol still satisfies the rule rather than being rejected
// for using the wrong alphabet.
func isSpecialChar(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}
