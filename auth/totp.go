// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ----------------------------------------------------------------------
// Constants
// ----------------------------------------------------------------------

const (
	// totpSecretBytes - This constant is 20 bytes, 160 bits, RFC 4226's
	// recommended HMAC-SHA1 key size, and what Google Authenticator and
	// every other mainstream authenticator app expects.
	totpSecretBytes = 20

	// totpDigits - This constant is 6, what Google Authenticator, and
	// effectively every TOTP app in practice, displays, even though RFC
	// 6238's worked examples use 8. The RFC's core algorithm supports
	// either. This package fixes 6, since that is what Google
	// Authenticator support means to a user setting this up. The 8-digit
	// RFC test vectors are still verified exactly, against the same
	// underlying hotp function, in totp_test.go. Fixing the public digit
	// count does not mean the core math is only tested at that count.
	totpDigits = 6

	// totpPeriod - This constant is 30 seconds, the RFC 6238 default and
	// universal real-world convention. No mainstream authenticator app
	// uses anything else, so there is no real reason to make this
	// configurable and every reason not to. A mismatched period between
	// this code and a user's phone app would just look like TOTP is
	// broken, with no obvious cause.
	totpPeriod = 30

	// totpSkew - This constant allows codes from one step before or after
	// the current one to still verify, tolerating ordinary clock drift
	// between the server and the phone generating codes, the same
	// tolerance every mainstream TOTP verifier uses. A code is valid for
	// up to (2*totpSkew+1)*totpPeriod seconds total, not indefinitely.
	totpSkew = 1
)

// ----------------------------------------------------------------------
// Public Functions - TOTP
// ----------------------------------------------------------------------

// GenerateTOTPSecret - This function generates a new random TOTP secret,
// base32-encoded with no padding, the way every authenticator app expects
// it typed or scanned. It is called once per user during enrollment, see
// the totp enable and totp enable qr commands in package core (cmd/core).
// The result is what gets shown as both the QR code and the plain text
// manual entry string, and what SaveUsers persists into that user's
// users.yaml entry as totp_secret.
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("error generating TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// GenerateTOTPCode - This function computes the current six-digit code for
// a base32-encoded secret at time t. It is exposed mainly for tests that
// need a live code to confirm a freshly generated secret with, the same
// sanity check totp enable's interactive confirmation step performs
// against whatever the user types. Most callers verifying a login attempt
// want VerifyTOTPCode instead, which also tolerates clock drift.
func GenerateTOTPCode(base32Secret string, t time.Time) (string, error) {
	secret, err := decodeTOTPSecret(base32Secret)
	if err != nil {
		return "", err
	}
	return totpAt(secret, t, totpDigits, totpPeriod), nil
}

// VerifyTOTPCode - This function checks a user-entered code against a
// base32-encoded secret, tolerant of up to totpSkew time steps of clock
// drift in either direction. It uses a constant-time comparison for each
// candidate. A TOTP code is short-lived, but there is no reason to leak
// timing information about how close a guess was regardless.
func VerifyTOTPCode(base32Secret, code string, t time.Time) bool {
	secret, err := decodeTOTPSecret(base32Secret)
	if err != nil {
		return false
	}
	counter := uint64(t.Unix()) / totpPeriod

	for skew := -totpSkew; skew <= totpSkew; skew++ {
		if skew < 0 && counter < uint64(-skew) {
			continue // Avoid underflowing counter near the Unix epoch.
		}
		candidate := hotp(secret, counter+uint64(skew), totpDigits)
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPProvisioningURI - This function builds the "otpauth://" URI every
// mainstream authenticator app understands when scanned as a QR code.
//
// issuer is shown as the organization or service name in the app.
// username identifies the account.
//
// The encoding has to be right, URL escaping the label and using real
// query parameters rather than string concatenation, because a malformed
// URI simply fails to scan in most apps, with no useful error.
func TOTPProvisioningURI(issuer, username, base32Secret string) string {
	label := url.PathEscape(issuer + ":" + username)
	v := url.Values{}
	v.Set("secret", base32Secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprintf("%d", totpDigits))
	v.Set("period", fmt.Sprintf("%d", totpPeriod))
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// FormatTOTPSecretForDisplay - This function groups a base32 secret into
// four character blocks, the conventional grouping, for someone typing it
// by hand.
//
// It is display only. Verification strips spaces before decoding, so this
// never affects what is validated or stored.
func FormatTOTPSecretForDisplay(secret string) string {
	var b strings.Builder
	for i, r := range secret {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ----------------------------------------------------------------------
// Private Functions - TOTP
// ----------------------------------------------------------------------

// decodeTOTPSecret - This function normalizes and decodes a base32 secret
// the way a human might type or paste it. It is uppercased, with any
// spaces stripped before decoding, since some apps and QR generators group
// it in four character blocks for readability.
func decodeTOTPSecret(base32Secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(base32Secret, " ", ""))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid TOTP secret: %w", err)
	}
	return secret, nil
}

// hotp - This function implements RFC 4226's HOTP algorithm: HMAC-SHA1 of
// an eight byte big endian counter, dynamic truncation per section 5.3,
// then mod 10^digits. TOTP is HOTP with the counter derived from time.
//
// digits is a parameter rather than the totpDigits constant so the tests
// can verify this against RFC 6238's eight digit worked examples, while
// every real caller asks for six.
func hotp(secret []byte, counter uint64, digits int) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, truncated%mod)
}

// totpAt - This function derives an HOTP counter from wall-clock time, RFC
// 6238's only real addition over HOTP, and delegates to hotp.
func totpAt(secret []byte, t time.Time, digits, period int) string {
	counter := uint64(t.Unix()) / uint64(period)
	return hotp(secret, counter, digits)
}
