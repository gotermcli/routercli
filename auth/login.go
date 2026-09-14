// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gotermcli/routercli/i18n"

	"golang.org/x/term"
)

// dummyBcryptHash - This constant is a fixed, valid bcrypt hash,
// bcryptHasher's Dummy method in auth.go returns its encoded half, used
// only to burn the same amount of CPU time a real password comparison
// would, when LocalProvider.Authenticate, see provider.go, hits a
// nonexistent username and bcrypt is still the active default hasher. Its
// plaintext, "not-a-real-password", is never compared against anything a
// real user could type. Only its bcrypt cost factor matters here.
const dummyBcryptHash = "$2a$10$C6UzMDM.H6dfI/f/IKcEeO4Sqzr4v4E4T0.E1TQhZY6NxN.0kQ8wa"

// ErrLoginFailed - This variable is returned by PromptLogin once the
// attempt limit is exhausted, so a caller such as main.go can distinguish
// a user typing the wrong password repeatedly from an actual I/O error,
// and choose an appropriate exit path and message for each.
var ErrLoginFailed = errors.New("authentication failed")

// ----------------------------------------------------------------------
// Public Functions - Login
// ----------------------------------------------------------------------

// VerifyLogin - This function performs the credential check, kept
// separate from terminal I/O so it can be unit tested without a real tty.
// It returns a new Session on success and does no prompting or retry
// accounting; PromptLogin drives the interactive flow around it.
//
// The check itself is delegated to provider, so this function does not
// need to know whether the backend is the local users.yaml, an LDAP
// directory, or anything else.
//
// A nonexistent username and a wrong password MUST produce the same
// result. Distinguishing "no such user" from "wrong password" tells an
// attacker which usernames are valid. Every Provider implementation is
// written to preserve that property.
func VerifyLogin(provider Provider, username, password string) (*Session, bool) {
	ok, err := provider.Authenticate(username, password)
	if err != nil || !ok {
		return nil, false
	}
	return &Session{Username: username, Authenticated: true}, true
}

// PromptLogin - This function runs the interactive login flow. It reads a
// username, reads a masked password, verifies both against the provider,
// and returns a Session on success.
//
// users is needed alongside provider because a second factor secret is
// always looked up in users.yaml, whichever provider checked the
// password. A backend such as LDAP has no notion of a TOTP secret. A
// username the provider authenticates with no users.yaml entry is treated
// as a real identity with no second factor.
//
// totpEnabled mirrors EnableTOTPAuthentication. When false, no second
// factor is ever requested, even for a user who has a secret enrolled.
// That switch turns step-up authentication off across the deployment.
//
// A correct password with a wrong or missing code fails exactly like a
// wrong password, so an attacker cannot tell the two apart.
//
// auditFail is called after every failed attempt, not only the last.
//
// With rateLimiter nil, maxAttempts is a flat cap. With one supplied, the
// limiter does the real work and maxAttempts is a safety ceiling. Allow is
// checked right after each username is read, so a lockout is per username.
//
// A locked out session returns ErrLoginFailed immediately rather than
// blocking. Sleeping out a lockout inside an interactive prompt is worse
// than ending the attempt and telling the caller how long to wait.
func PromptLogin(r io.Reader, w io.Writer, fd int, provider Provider, users Users, totpEnabled bool, maxAttempts int, rateLimiter *KeyedRateLimiter, t *i18n.Translator, auditFail func(username string)) (*Session, error) {
	reader := bufio.NewReader(r)

	// With a real rate limiter, the lockout itself is what limits how many
	// attempts may happen. This loop bound is purely a defensive safety
	// net against an infinite loop if the rate limiter is ever
	// misconfigured to never lock out, and is not expected to be reached
	// in normal operation.
	loopBound := maxAttempts
	if rateLimiter != nil {
		loopBound = 1000
	}

	for attempt := 1; attempt <= loopBound; attempt++ {
		fmt.Fprint(w, promptText(t, "auth.username_prompt", "Username: "))
		username, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("error reading username: %w", err)
		}
		username = strings.TrimSpace(username)

		if rateLimiter != nil {
			if ok, retryAfter := rateLimiter.Allow(username); !ok {
				fmt.Fprintln(w, "%", promptText(t, "auth.too_many_attempts", "Too many failed attempts - try again in %s", RoundForDisplay(retryAfter)))
				if auditFail != nil {
					auditFail(username)
				}
				return nil, ErrLoginFailed
			}
		}

		fmt.Fprint(w, promptText(t, "auth.password_prompt", "Password: "))
		passwordBytes, err := term.ReadPassword(fd)
		fmt.Fprintln(w) // ReadPassword does not echo the newline the user typed.
		if err != nil {
			return nil, fmt.Errorf("error reading password: %w", err)
		}

		session, ok := VerifyLogin(provider, username, string(passwordBytes))
		if ok {
			u := users[username]
			if u == nil {
				// provider authenticated username, but this project's
				// users.yaml has no matching entry for it. This is
				// expected once a non-local Provider is in play, see this
				// function's doc comment, and means there is no second
				// factor secret to check.
				u = &User{Username: username}
			}
			if !totpEnabled || !SecondFactorRequired(u) {
				if rateLimiter != nil {
					rateLimiter.RecordSuccess(username)
				}
				return session, nil
			}
			if VerifySecondFactor(w, reader, fd, u, t) {
				if rateLimiter != nil {
					rateLimiter.RecordSuccess(username)
				}
				return session, nil
			}
			// A wrong or missing second factor code falls through to the
			// same failure handling as a wrong password, below.
		}

		if rateLimiter != nil {
			rateLimiter.RecordFailure(username)
		}
		if auditFail != nil {
			auditFail(username)
		}
		if attempt < loopBound {
			fmt.Fprintln(w, "%", promptText(t, "auth.login_incorrect", "Login incorrect"))
		}
	}

	return nil, ErrLoginFailed
}

// RoundForDisplay - This function rounds a retry-after duration up to the
// nearest second before showing it to a user. The underlying duration
// often carries sub-second precision, for example "4m59.7s", that is
// meaningless noise in a "try again in %s" message. It is exported so
// every place that needs this can reach it.
func RoundForDisplay(d time.Duration) time.Duration {
	return d.Round(time.Second)
}

// PromptSecret - This function reads a single password, masked, with no
// username and no association with any *User, and returns it as plaintext
// for the caller to verify.
func PromptSecret(w io.Writer, fd int, t *i18n.Translator) (string, error) {
	fmt.Fprint(w, promptText(t, "auth.password_prompt", "Password: "))
	passwordBytes, err := term.ReadPassword(fd)
	fmt.Fprintln(w)
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

// PromptNewPassword - This function reads a candidate new password, masked
// the same way PromptSecret reads an existing one, for
// example/cmd/core/cmd_password.go's password change command. It is a distinct
// function from PromptSecret, rather than PromptSecret reused as is, so
// its own prompt text ("New password: ") reads unambiguously different
// from a prompt for an already-known password.
func PromptNewPassword(w io.Writer, fd int, t *i18n.Translator) (string, error) {
	fmt.Fprint(w, promptText(t, "auth.new_password_prompt", "New password: "))
	passwordBytes, err := term.ReadPassword(fd)
	fmt.Fprintln(w)
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

// PromptPasswordConfirmation - This function reads a second, masked copy
// of a candidate new password, for example/cmd/core/cmd_password.go's password
// change command to confirm against what PromptNewPassword already read,
// the same "type it twice" confirmation step any password change form uses
// to catch a typo before it becomes the only copy of a password nobody,
// including its own owner, can reproduce.
func PromptPasswordConfirmation(w io.Writer, fd int, t *i18n.Translator) (string, error) {
	fmt.Fprint(w, promptText(t, "auth.confirm_password_prompt", "Confirm new password: "))
	passwordBytes, err := term.ReadPassword(fd)
	fmt.Fprintln(w)
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

// ----------------------------------------------------------------------
// Private Functions - Login
// ----------------------------------------------------------------------

// promptText - This function resolves a translation key through t, falling
// back to fallback when t is nil or the key comes back as i18n's bracketed
// placeholder.
//
// The second check matters because a real Translator with no catalogs
// loaded returns the bracketed form too, and a login prompt reading
// "[[auth.username_prompt]]" is worse than the English fallback.
func promptText(t *i18n.Translator, key, fallback string, args ...any) string {
	text := t.T(key, args...)
	if text == "[["+key+"]]" {
		if len(args) > 0 {
			return fmt.Sprintf(fallback, args...)
		}
		return fallback
	}
	return text
}
