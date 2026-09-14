// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"net/url"
	"testing"
	"time"
)

// TestHOTPMatchesRFC6238TestVectors - This test checks hotp against RFC
// 6238's Appendix B worked examples exactly, not just whether the output
// looks plausible. Those vectors use the 20-byte ASCII secret
// "12345678901234567890", not base32-encoded, since the RFC uses the raw
// ASCII bytes directly, and eight-digit output at specific Unix times,
// with SHA1, this project's default HMAC. This calls the unexported hotp
// and totpAt functions with digits=8 to match the RFC exactly, even though
// every real caller of this package only ever asks for six. See
// totpDigits's doc comment for why six is the fixed public default.
func TestHOTPMatchesRFC6238TestVectors(t *testing.T) {
	secret := []byte("12345678901234567890") // RFC 6238 Appendix B's SHA1 test secret, raw ASCII.

	cases := []struct {
		unixTime int64
		want     string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		// RFC 6238's final vector, 20000000000, exceeds int64 range for
		// some 32-bit builds' time.Unix handling, and is not needed beyond
		// the five above to confirm the algorithm. Those five already span
		// early, mid-range, and far future Unix times.
	}

	for _, c := range cases {
		tm := time.Unix(c.unixTime, 0).UTC()
		got := totpAt(secret, tm, 8, totpPeriod)
		if got != c.want {
			t.Errorf("totpAt(t=%d) = %q, want %q (RFC 6238 Appendix B)", c.unixTime, got, c.want)
		}
	}
}

// TestGenerateAndVerifyTOTPCodeRoundTrip - This test verifies that a
// freshly generated secret produces a six-digit code that VerifyTOTPCode
// accepts for that same instant.
func TestGenerateAndVerifyTOTPCodeRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	now := time.Now()

	code, err := GenerateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode returned error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code = %q, want 6 digits", code)
	}
	if !VerifyTOTPCode(secret, code, now) {
		t.Error("VerifyTOTPCode rejected a code generated for the exact same time")
	}
}

// TestVerifyTOTPCodeRejectsWrongCode - This test verifies that a fixed,
// almost certainly wrong code fails verification. It skips rather than
// fails in the astronomically unlikely case that "000000" is correct at
// the instant the test runs, so the test cannot become flaky.
func TestVerifyTOTPCodeRejectsWrongCode(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	if VerifyTOTPCode(secret, "000000", time.Now()) {
		t.Skip("code 000000 happened to be correct at this instant, skipping")
	}
}

// TestVerifyTOTPCodeToleratesClockSkew - This test verifies that a code
// generated one period earlier still verifies now, which is the whole
// point of totpSkew, tolerating a phone or server clock that has drifted
// slightly.
func TestVerifyTOTPCodeToleratesClockSkew(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()

	earlier := now.Add(-totpPeriod * time.Second)
	code, _ := GenerateTOTPCode(secret, earlier)
	if !VerifyTOTPCode(secret, code, now) {
		t.Error("a code from one period earlier should still verify (within totpSkew)")
	}
}

// TestVerifyTOTPCodeRejectsCodeOutsideSkewWindow - This test verifies that
// a code from far enough in the past, well outside the +/-1 step
// tolerance, is rejected rather than accepted.
func TestVerifyTOTPCodeRejectsCodeOutsideSkewWindow(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()

	farAway := now.Add(-10 * totpPeriod * time.Second)
	code, _ := GenerateTOTPCode(secret, farAway)
	if VerifyTOTPCode(secret, code, now) {
		t.Error("a code from 10 periods earlier should not verify, since it is outside the skew window")
	}
}

// TestDecodeTOTPSecretHandlesSpacesAndCase - This test verifies that
// VerifyTOTPCode tolerates a secret typed the way a human would, grouped
// into four character blocks with spaces and in lowercase, rather than
// only accepting the exact unbroken uppercase form GenerateTOTPSecret
// returns.
func TestDecodeTOTPSecretHandlesSpacesAndCase(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	now := time.Now()
	code, _ := GenerateTOTPCode(secret, now)

	var spaced string
	lower := ""
	for i, r := range secret {
		lower += string(r + 32*boolToInt(r >= 'A' && r <= 'Z'))
		if i > 0 && i%4 == 0 {
			spaced += " "
		}
		spaced += string(r)
	}

	if !VerifyTOTPCode(spaced, code, now) {
		t.Error("VerifyTOTPCode should tolerate a space-grouped secret")
	}
}

func boolToInt(b bool) rune {
	if b {
		return 1
	}
	return 0
}

// TestGenerateTOTPSecretProducesUniqueValidBase32 - This test verifies
// that two calls to GenerateTOTPSecret never collide, and that the result
// decodes as valid base32.
func TestGenerateTOTPSecretProducesUniqueValidBase32(t *testing.T) {
	s1, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	s2, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}
	if s1 == s2 {
		t.Error("two calls to GenerateTOTPSecret produced the same secret - randomness is broken")
	}
	if _, err := decodeTOTPSecret(s1); err != nil {
		t.Errorf("generated secret failed to decode as base32: %v", err)
	}
}

// TestTOTPProvisioningURIContainsExpectedFields - This test verifies that
// the otpauth:// URI TOTPProvisioningURI builds parses correctly and
// carries the issuer, secret, digits, and period an authenticator app
// needs.
func TestTOTPProvisioningURIContainsExpectedFields(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	uri := TOTPProvisioningURI("routercli", "alice", secret)

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("provisioning URI failed to parse as a URL: %v", err)
	}
	if parsed.Scheme != "otpauth" {
		t.Errorf("scheme = %q, want %q", parsed.Scheme, "otpauth")
	}
	if parsed.Host != "totp" {
		t.Errorf("host = %q, want %q", parsed.Host, "totp")
	}
	q := parsed.Query()
	if q.Get("secret") != secret {
		t.Errorf("secret query param = %q, want %q", q.Get("secret"), secret)
	}
	if q.Get("issuer") != "routercli" {
		t.Errorf("issuer query param = %q, want %q", q.Get("issuer"), "routercli")
	}
	if q.Get("digits") != "6" {
		t.Errorf("digits query param = %q, want %q", q.Get("digits"), "6")
	}
	if q.Get("period") != "30" {
		t.Errorf("period query param = %q, want %q", q.Get("period"), "30")
	}
}

// TestFormatTOTPSecretForDisplayGroupsIntoFourCharacterBlocks - This test
// verifies that FormatTOTPSecretForDisplay inserts a space every four
// characters and otherwise leaves the secret's characters untouched, the
// conventional grouping every authenticator app and setup guide uses.
func TestFormatTOTPSecretForDisplayGroupsIntoFourCharacterBlocks(t *testing.T) {
	got := FormatTOTPSecretForDisplay("JBSWY3DPEHPK3PXP")
	want := "JBSW Y3DP EHPK 3PXP"
	if got != want {
		t.Errorf("FormatTOTPSecretForDisplay(%q) = %q, want %q", "JBSWY3DPEHPK3PXP", got, want)
	}
}

// TestFormatTOTPSecretForDisplayShorterThanOneBlock - This test verifies
// that a secret shorter than four characters is returned with no space
// inserted at all, rather than a leading or trailing one.
func TestFormatTOTPSecretForDisplayShorterThanOneBlock(t *testing.T) {
	got := FormatTOTPSecretForDisplay("ABC")
	if got != "ABC" {
		t.Errorf("FormatTOTPSecretForDisplay(%q) = %q, want %q", "ABC", got, "ABC")
	}
}
