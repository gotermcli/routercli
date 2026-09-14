// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// init - This registers "password change", reachable only from inside the
// user Command Level.
//
// Nothing is ever a same line argument. A same line argument is recorded
// verbatim by both the audit log and readline history, which would leave a
// plaintext password on disk.
//
// The handler splits interactive work from decision work, the same as
// cmd_totp.go. runPasswordChange drives the prompts, then hands plain
// strings to verifyReauth and finishPasswordChange, so the verify and save
// logic can be tested with known inputs.
//
// A change has two phases, re-authentication then the new password. Both
// draw on one shared budget, ctx.PasswordChangeMaxAttempts, rather than
// each getting its own. Failing re-authentication several times leaves
// correspondingly fewer chances to get the new password right.
func init() {
	command.Register("password.change", runPasswordChange)
}

// runPasswordChange - This function is the registered "password change"
// handler. It carries the fixed command.HandlerFunc signature
// command.Register requires, so it passes the real process's stdin file
// descriptor and stdout along to runPasswordChangeWithIO, which does the
// actual work. See this file's init doc comment for the shape of the whole
// flow and why the attempt budget is shared across both of its phases.
func runPasswordChange(ctx *command.AppContext, args []string) error {
	return runPasswordChangeWithIO(ctx, int(os.Stdin.Fd()), os.Stdout)
}

// RunPasswordChange - This function is the exported entry point for
// driving "password change" from outside this package.
//
// main.go calls it once, right after a login, when the account has
// MustChangePassword set. "account create" is the one place that flag is
// ever set.
//
// It is otherwise identical to a session typing the command. It exists
// only because main.go never resolves and dispatches a command by name, so
// it needs a real function to call.
func RunPasswordChange(ctx *command.AppContext, fd int, stdout io.Writer) error {
	return runPasswordChangeWithIO(ctx, fd, stdout)
}

// runPasswordChangeWithIO - This function drives the interactive body of
// "password change" against fd and stdout rather than the process's real
// stdin and stdout.
//
// That injection lets a test hand this a pty's slave file rather than
// needing a real terminal attached to the test binary. The only non-test
// caller always passes the real ones.
func runPasswordChangeWithIO(ctx *command.AppContext, fd int, stdout io.Writer) error {
	if err := requireLoggedIn(ctx); err != nil {
		return err
	}
	user, ok := currentUser(ctx)
	if !ok {
		return fmt.Errorf("%s", ctx.Translator.T("user.no_current_user"))
	}

	maxAttempts := passwordChangeMaxAttempts(ctx)
	reauthenticated := false

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if !reauthenticated {
			password, err := auth.PromptSecret(stdout, fd, ctx.Translator)
			if err != nil {
				return err
			}
			var code string
			if auth.SecondFactorRequired(user) {
				code, err = auth.PromptTOTPCode(stdout, fd, ctx.Translator)
				if err != nil {
					return err
				}
			}
			if !verifyReauth(ctx.AuthProvider, user, password, code, time.Now()) {
				fmt.Println(ctx.Translator.T("password.change.denied"))
				printPasswordChangeRetry(ctx, maxAttempts, attempt)
				continue
			}
			reauthenticated = true
		}

		newPassword, err := auth.PromptNewPassword(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		confirmPassword, err := auth.PromptPasswordConfirmation(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}

		changed, err := finishPasswordChange(ctx, user, newPassword, confirmPassword)
		if err != nil {
			return err
		}
		if changed {
			return nil
		}
		printPasswordChangeRetry(ctx, maxAttempts, attempt)
	}

	fmt.Println(ctx.Translator.T("password.change.attempts_exhausted"))
	return nil
}

// passwordChangeMaxAttempts - This function returns
// ctx.PasswordChangeMaxAttempts, or 1 if it is zero or negative, the
// minimum a retry loop needs in order to offer even the first attempt, the
// same fallback totpMaxAttempts in cmd_totp.go already uses for the same
// reason. A real ctx wired up through main.go always carries a positive
// value here, config.SystemConfig.validate enforces that at startup, so
// this fallback only matters for a hand built *command.AppContext in a
// test that does not set the field.
func passwordChangeMaxAttempts(ctx *command.AppContext) int {
	if ctx.PasswordChangeMaxAttempts < 1 {
		return 1
	}
	return ctx.PasswordChangeMaxAttempts
}

// printPasswordChangeRetry - This function prints how many attempts remain
// after a failed phase of runPasswordChange, current password, second
// factor code, or new password alike, or nothing at all once attempt was
// the last one, since runPasswordChange's closing "attempts_exhausted"
// message covers that case instead.
func printPasswordChangeRetry(ctx *command.AppContext, maxAttempts, attempt int) {
	if remaining := maxAttempts - attempt; remaining > 0 {
		fmt.Println(ctx.Translator.T("password.change.retry", remaining))
	}
}

// verifyReauth - This function checks password through provider, and when
// a second factor is required, code as well, against the user's stored
// credentials.
//
// The password check goes through provider rather than against
// user.PasswordHash directly, so a password change re-authenticates
// against whichever backend owns the account, the same one that checked
// the original login.
//
// The second factor step does not depend on which provider is in play. A
// TOTP secret always lives in users.yaml and is always checked the same
// way, wherever the password was verified.
//
// now is a parameter rather than a time.Now call, so a test can pass a
// fixed instant alongside a code generated for it.
//
// A right password with a wrong or missing code is reported identically to
// a wrong password, so an attacker watching the response cannot tell the
// two apart.
func verifyReauth(provider auth.Provider, user *auth.User, password, code string, now time.Time) bool {
	ok, err := provider.Authenticate(user.Username, password)
	if err != nil || !ok {
		return false
	}
	if auth.SecondFactorRequired(user) {
		return auth.VerifySecondFactorCode(user, code, now)
	}
	return true
}

// finishPasswordChange - The verify and save half of "password change",
// split from the prompting half so it can be tested against known inputs
// without a terminal.
//
// newPassword and confirmPassword arrive as already read strings. Checks
// run in order and stop at the first failure, so a session sees one
// problem at a time:
//
//  1. The two entries must match.
//  2. The new password must satisfy ctx.PasswordPolicy. Every violation
//     is reported together, since ValidatePassword checks all rules.
//  3. It must differ from the current password. A password that is not
//     changing gains nothing, and usually means the session retyped its
//     own current one by mistake.
//
// On success the hash is replaced in memory only. Nothing here writes to
// disk; "write memory" is what saves it. A restart before that returns to
// whatever the users file last held.
//
// The write goes through MutateUsers so it lands in the daemon when one
// is configured.
func finishPasswordChange(ctx *command.AppContext, user *auth.User, newPassword, confirmPassword string) (bool, error) {
	if newPassword != confirmPassword {
		fmt.Println(ctx.Translator.T("password.change.mismatch"))
		return false, nil
	}

	if violations := auth.ValidatePassword(newPassword, ctx.PasswordPolicy); len(violations) > 0 {
		printPasswordViolations(ctx, violations)
		return false, nil
	}

	if auth.VerifyPassword(user.PasswordHash, newPassword) {
		fmt.Println(ctx.Translator.T("password.change.same_as_current"))
		return false, nil
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return false, err
	}

	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		target := users[ctx.Session.Username]
		target.PasswordHash = hash
		// A successful change, forced or voluntary, always clears a
		// pending forced-change flag. This account has just proven a real
		// password of its own choosing, so there is nothing left to force.
		target.MustChangePassword = false
		return nil, nil
	}); err != nil {
		return false, err
	}
	// This only ever changes user.PasswordHash and
	// user.MustChangePassword, in memory, never ctx.UsersFile on disk. See
	// this project's "nothing survives a restart without an explicit save"
	// core rule: a password changed here, this account's included,
	// reverts to whatever ctx.UsersFile last held the next time this
	// deployment reloads or restarts, unless a session explicitly runs
	// "write memory" or a future equivalent first.
	ctx.Logger.Debugln("DEBUG: password changed for user", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("password.change.confirm"))
	return true, nil
}

// printPasswordViolations - This function prints one translated message
// per violation. The mapping from violation to message key belongs here
// rather than in package auth, which has no i18n awareness at all.
//
// TooShort and TooLong carry the configured minimum and the fixed maximum
// as a formatting argument, so a session is told the real number to
// satisfy rather than given a generic complaint.
//
// An unrecognized violation prints its raw value rather than being
// dropped, so a new violation type added without a matching case here
// fails loudly instead of going unreported.
func printPasswordViolations(ctx *command.AppContext, violations []auth.PasswordViolation) {
	for _, v := range violations {
		switch v {
		case auth.PasswordViolationTooShort:
			fmt.Println(ctx.Translator.T("password.change.violation_too_short", ctx.PasswordPolicy.MinLength))
		case auth.PasswordViolationTooLong:
			fmt.Println(ctx.Translator.T("password.change.violation_too_long", auth.MaxPasswordLength))
		case auth.PasswordViolationNeedsUppercase:
			fmt.Println(ctx.Translator.T("password.change.violation_needs_uppercase"))
		case auth.PasswordViolationNeedsNumber:
			fmt.Println(ctx.Translator.T("password.change.violation_needs_number"))
		case auth.PasswordViolationNeedsSpecialChar:
			fmt.Println(ctx.Translator.T("password.change.violation_needs_special_char"))
		default:
			fmt.Println(string(v))
		}
	}
}
