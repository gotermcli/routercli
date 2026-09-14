// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gotermcli/routercli/i18n"

	"golang.org/x/term"
)

// ----------------------------------------------------------------------
// Public Functions - MFA
// ----------------------------------------------------------------------

// SecondFactorRequired - This function reports whether u has any second
// factor configured, checked at login, see login.go's PromptLogin, right
// after the password verifies. This is the one place that needs to know
// about every second factor method that exists. Adding a new method later,
// such as FIDO2 or U2F, means adding its own "is it configured" check here
// and its own branch in VerifySecondFactor below, without touching
// PromptLogin or any other call site at all.
func SecondFactorRequired(u *User) bool {
	return u.TOTPSecret != ""
}

// VerifySecondFactor - This function prompts for and checks whichever
// second factor u has configured. Only TOTP exists today; this is the seam
// a later method plugs into.
//
// It returns false when SecondFactorRequired(u) was false, which callers
// are expected to check first. It does not re-derive whether a second
// factor is needed, only whether the configured one checks out.
//
// reader is the same bufio.Reader PromptLogin already wraps stdin in.
// Wrapping the same stream in a second reader risks losing bytes the first
// has already buffered ahead. fd is used only for the masked read, since
// term.ReadPassword needs a real descriptor.
func VerifySecondFactor(w io.Writer, reader *bufio.Reader, fd int, u *User, t *i18n.Translator) bool {
	if u.TOTPSecret != "" {
		return promptAndVerifyTOTP(w, reader, fd, u, t)
	}
	return false
}

// VerifySecondFactorCode - This function checks an already read code
// against whichever second factor u has configured. It performs no I/O,
// which is what separates it from VerifySecondFactor.
//
// The password change command runs its own retry loop around a masked
// prompt and only needs the check, not the prompting.
//
// now is a parameter rather than a time.Now call, so a test can pass a
// fixed instant alongside a code generated for it.
//
// It returns false when SecondFactorRequired(u) was false, mirroring
// VerifySecondFactor.
func VerifySecondFactorCode(u *User, code string, now time.Time) bool {
	if u.TOTPSecret != "" {
		return VerifyTOTPCode(u.TOTPSecret, code, now)
	}
	return false
}

// PromptTOTPCode - This function reads one six digit code, masked like a
// password, with no User association and no verification.
//
// It is for a caller that already knows which secret to check against,
// such as totp enable and totp disable, rather than deriving it from a
// matched login the way PromptLogin does.
//
// It has no bufio.Reader fallback for a non-terminal fd, since every
// caller runs inside the interactive read loop with a real terminal.
func PromptTOTPCode(w io.Writer, fd int, t *i18n.Translator) (string, error) {
	fmt.Fprint(w, promptText(t, "auth.totp_prompt", "TOTP code: "))
	codeBytes, err := term.ReadPassword(fd)
	fmt.Fprintln(w) // ReadPassword does not echo the newline the user typed.
	if err != nil {
		return "", err
	}
	return string(codeBytes), nil
}

// ----------------------------------------------------------------------
// Private Functions - MFA
// ----------------------------------------------------------------------

// promptAndVerifyTOTP - This function reads a six-digit code, masked the
// same as a password, since there is no reason to show it over a shoulder
// either, and checks it against u.TOTPSecret with the standard clock-skew
// tolerance. See VerifyTOTPCode.
func promptAndVerifyTOTP(w io.Writer, reader *bufio.Reader, fd int, u *User, t *i18n.Translator) bool {
	fmt.Fprint(w, promptText(t, "auth.totp_prompt", "TOTP code: "))

	// term.ReadPassword needs a real terminal file descriptor. When stdin
	// is not one, such as with piped input or some test harnesses, this
	// falls back to reading a plain line from the same bufio.Reader the
	// rest of the login flow already uses, rather than crashing outside an
	// interactive session. Interactive callers, such as main.go's real
	// login flow, always pass a genuine terminal file descriptor, so this
	// fallback is a safety net, not the normal path.
	var code string
	if codeBytes, err := term.ReadPassword(fd); err == nil {
		fmt.Fprintln(w)
		code = string(codeBytes)
	} else {
		line, _ := reader.ReadString('\n')
		code = strings.TrimSpace(line)
	}

	return VerifyTOTPCode(u.TOTPSecret, code, time.Now())
}
