// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"strings"
	"testing"

	"github.com/gotermcli/routercli/auth"
)

// TestPrintTOTPSecretShowsTheFormattedSecret - This test verifies that
// printTOTPSecret prints secret through auth.FormatTOTPSecretForDisplay,
// its grouped, human readable form, rather than the bare, ungrouped base32
// string.
func TestPrintTOTPSecretShowsTheFormattedSecret(t *testing.T) {
	ctx := newTOTPTestContext(t, "alice", &auth.User{Username: "alice"})
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}

	out := captureStdout(t, func() { printTOTPSecret(ctx, secret) })

	if !strings.Contains(out, auth.FormatTOTPSecretForDisplay(secret)) {
		t.Errorf("expected output to contain the formatted secret, got %q", out)
	}
}

// TestPrintTOTPEnrollmentQRShowsTheQRCodeAndSecret - This test verifies
// that printTOTPEnrollmentQR prints both a rendered QR code block, a
// nonempty block of text distinct from the plain secret line, and the same
// manually enterable secret printTOTPSecret's test checks for, the "totp
// enable qr" presentation, and returns no error for a valid secret and
// issuer.
func TestPrintTOTPEnrollmentQRShowsTheQRCodeAndSecret(t *testing.T) {
	ctx := newTOTPTestContext(t, "alice", &auth.User{Username: "alice"})
	ctx.TOTPIssuer = "RouterCLI"
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret returned error: %v", err)
	}

	var qerr error
	out := captureStdout(t, func() { qerr = printTOTPEnrollmentQR(ctx, secret) })

	if qerr != nil {
		t.Fatalf("printTOTPEnrollmentQR returned unexpected error: %v", qerr)
	}
	if !strings.Contains(out, auth.FormatTOTPSecretForDisplay(secret)) {
		t.Errorf("expected output to contain the formatted secret, got %q", out)
	}
	if len(out) < 200 {
		t.Errorf("expected a rendered QR code block to make the output substantially longer than just the secret line, got only %d bytes: %q", len(out), out)
	}
}
