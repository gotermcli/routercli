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

	qrcode "github.com/skip2/go-qrcode"
)

// init - This registers "totp enable", "totp enable qr", and
// "totp disable", reachable only from inside the user Command Level.
//
// No confirmation code or password is ever a same line argument. A same
// line argument is recorded verbatim by both the audit log and readline
// history, which would leave the secret on disk. The literal "qr" is the
// exception, since it names a display mode rather than a secret.
//
// "totp enable" shows the secret for manual entry. "totp enable qr" also
// shows a scannable QR code, for a terminal where that renders well. Both
// then read a confirmation code and share the same enrollment path.
//
// Each handler splits interactive work from decision work. The registered
// handler prints and reads from the terminal, then hands plain strings to
// finishTOTPEnable or finishTOTPDisable. That split is what makes the
// verify and save logic testable with a known code and a fixed time.
//
// A rejected code is retried up to ctx.TOTPMaxAttempts times. Once the
// enrollment succeeds or the attempts run out, the screen and scrollback
// are cleared so the secret does not linger for the next person at that
// terminal.
func init() {
	command.Register("totp.enable", func(ctx *command.AppContext, args []string) error {
		return runTOTPEnable(ctx, false, int(os.Stdin.Fd()), os.Stdout)
	})

	command.Register("totp.enable.qr", func(ctx *command.AppContext, args []string) error {
		return runTOTPEnable(ctx, true, int(os.Stdin.Fd()), os.Stdout)
	})

	command.Register("totp.disable", func(ctx *command.AppContext, args []string) error {
		return runTOTPDisable(ctx, int(os.Stdin.Fd()), os.Stdout)
	})
}

// runTOTPDisable - This function is the interactive body of "totp
// disable", registered against fd and stdout rather than the real
// process's os.Stdin and os.Stdout directly, the same io.Writer and file
// descriptor injection pattern runTOTPEnable below and main.go's
// establishSession already use, and for the same reason: it lets a test
// hand this function a pty's slave file directly. The init registration
// above is the only real caller outside a test, always passing the real
// process's stdin and stdout, so production behavior is unchanged.
func runTOTPDisable(ctx *command.AppContext, fd int, stdout io.Writer) error {
	if err := requireLoggedIn(ctx); err != nil {
		return err
	}
	user, ok := currentUser(ctx)
	if !ok {
		return fmt.Errorf("%s", ctx.Translator.T("user.no_current_user"))
	}
	if user.TOTPSecret == "" {
		fmt.Println(ctx.Translator.T("totp.not_enabled"))
		return nil
	}

	maxAttempts := totpMaxAttempts(ctx)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		password, err := auth.PromptSecret(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		code, err := auth.PromptTOTPCode(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		disabled, err := finishTOTPDisable(ctx, user, password, code, time.Now())
		if err != nil {
			return err
		}
		if disabled {
			return nil
		}
		if remaining := maxAttempts - attempt; remaining > 0 {
			fmt.Println(ctx.Translator.T("totp.disable_retry", remaining))
		}
	}

	fmt.Println(ctx.Translator.T("totp.disable_attempts_exhausted"))
	return nil
}

// runTOTPEnable - This function is the shared interactive body of
// "totp enable" and "totp enable qr", differing only in showQR, which
// decides how the freshly generated secret is displayed.
//
// Everything else is identical either way: the login and current user
// checks, the already-enabled refusal, the retry loop, and clearing the
// screen once the secret is no longer needed.
//
// fd and stdout are injected so a test can drive this against a real pty.
func runTOTPEnable(ctx *command.AppContext, showQR bool, fd int, stdout io.Writer) error {
	if err := requireLoggedIn(ctx); err != nil {
		return err
	}
	user, ok := currentUser(ctx)
	if !ok {
		return fmt.Errorf("%s", ctx.Translator.T("user.no_current_user"))
	}
	if user.TOTPSecret != "" {
		fmt.Println(ctx.Translator.T("totp.already_enabled"))
		return nil
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return err
	}
	if showQR {
		if err := printTOTPEnrollmentQR(ctx, secret); err != nil {
			return err
		}
	} else {
		printTOTPSecret(ctx, secret)
	}

	maxAttempts := totpMaxAttempts(ctx)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		code, err := auth.PromptTOTPCode(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		enabled, err := finishTOTPEnable(ctx, user, secret, code, time.Now())
		if err != nil {
			return err
		}
		if enabled {
			clearScreen(stdout)
			return nil
		}
		if remaining := maxAttempts - attempt; remaining > 0 {
			fmt.Println(ctx.Translator.T("totp.enable_retry", remaining))
		}
	}

	clearScreen(stdout)
	fmt.Println(ctx.Translator.T("totp.enable_attempts_exhausted"))
	return nil
}

// totpMaxAttempts - This function returns ctx.TOTPMaxAttempts, or 1 if it
// is zero or negative, the minimum a retry loop needs in order to offer
// even the first attempt. A real ctx wired up through main.go always
// carries a positive value here, config.SystemConfig.validate enforces
// that at startup the same way it already does for LoginMaxAttempts, so
// this fallback only matters for a hand built *command.AppContext in a
// test that does not set the field.
func totpMaxAttempts(ctx *command.AppContext) int {
	if ctx.TOTPMaxAttempts < 1 {
		return 1
	}
	return ctx.TOTPMaxAttempts
}

// currentUser - This function looks up the auth.User record matching
// ctx.Session.Username in ctx.Users, so the totp enable and totp disable
// handlers modify the same record auth.PromptLogin already authenticated
// against. It returns ok false if ctx.Users or ctx.Session is nil, or the
// session's username has no matching entry. None of that should be
// possible once requireLoggedIn has already passed, but this is checked
// rather than assumed, the same defensive style cmd_password_manager.go
// already uses for its own ctx.Levels.ByName lookup.
func currentUser(ctx *command.AppContext) (*auth.User, bool) {
	if ctx.Session == nil || ctx.Users == nil {
		return nil, false
	}
	u, ok := ctx.Users[ctx.Session.Username]
	return u, ok
}

// printTOTPSecret - This function shows a freshly generated TOTP secret as
// plain, grouped text for manual entry, the presentation plain "totp
// enable" uses for whoever would rather type a secret into their
// authenticator app than scan a QR code, or is enrolling from a terminal a
// QR code would not render well on.
func printTOTPSecret(ctx *command.AppContext, secret string) {
	fmt.Println(ctx.Translator.T("totp.enable_secret_intro"))
	fmt.Println()
	fmt.Println("  " + auth.FormatTOTPSecretForDisplay(secret))
	fmt.Println()
}

// printTOTPEnrollmentQR - This function shows a freshly generated TOTP
// secret both as a scannable QR code and as plain text for manual entry,
// the "totp enable qr" presentation.
func printTOTPEnrollmentQR(ctx *command.AppContext, secret string) error {
	uri := auth.TOTPProvisioningURI(ctx.TOTPIssuer, ctx.Session.Username, secret)
	qr, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return err
	}

	fmt.Println(ctx.Translator.T("totp.enable_intro"))
	fmt.Println()
	fmt.Println(qr.ToSmallString(false))
	fmt.Println(ctx.Translator.T("totp.enable_manual"))
	fmt.Println()
	fmt.Println("  " + auth.FormatTOTPSecretForDisplay(secret))
	fmt.Println()
	return nil
}

// ansiClearScreen is the escape sequence for wiping a terminal and
// returning the cursor home.
//
// \x1b[2J clears the visible screen, \x1b[3J discards the scrollback, and
// \x1b[H moves the cursor home.
//
// Without \x1b[3J a viewer could scroll up and read an already cleared
// secret straight back off the screen, which defeats the point of clearing
// it. That sequence is an xterm extension, honored by every mainstream
// emulator in wide use.
const ansiClearScreen = "\x1b[H\x1b[2J\x1b[3J"

// clearScreen - This function wipes w, an already printed secret and its
// QR code included, off the visible terminal and its scrollback once that
// material is no longer needed. runTOTPEnable calls this both right after
// a confirmation code is accepted and saved, and once every retry attempt
// has been used up without one, so a secret that was shown but never
// confirmed does not linger on screen either.
func clearScreen(w io.Writer) {
	fmt.Fprint(w, ansiClearScreen)
}

// finishTOTPEnable - The verify and save half of "totp enable", split from
// the prompting half so it can be tested without a terminal.
//
// now is a parameter rather than a time.Now() call, so a test can pass a
// fixed instant alongside a code generated for it, instead of racing a
// real clock across a 30 second TOTP window.
//
// A code that does not verify is reported and returns false with a nil
// error, the same way a wrong password at a login prompt is reported. One
// wrong attempt is not a program failure. The caller decides whether any
// attempts remain.
//
// On success the secret is set in memory only. "write memory" is what
// saves it.
func finishTOTPEnable(ctx *command.AppContext, user *auth.User, secret, code string, now time.Time) (bool, error) {
	if !auth.VerifyTOTPCode(secret, code, now) {
		fmt.Println(ctx.Translator.T("totp.enable_failed"))
		return false, nil
	}

	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		users[ctx.Session.Username].TOTPSecret = secret
		return nil, nil
	}); err != nil {
		return false, err
	}
	// This only ever changes user.TOTPSecret, in memory, never
	// ctx.UsersFile on disk. See this project's "nothing survives a
	// restart without an explicit save" core rule: a reload or
	// restart before "write memory" leaves TOTP disabled again for this
	// account.
	ctx.Logger.Debugln("DEBUG: TOTP enabled for user", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("totp.enable_confirm"))
	return true, nil
}

// finishTOTPDisable - The verify and save half of "totp disable", the same
// split finishTOTPEnable uses.
//
// Both the account password and a current code must verify before the
// secret is cleared. Removing a second factor is exactly what someone else
// at an unlocked session should not be able to do unchallenged, so this
// re-authenticates rather than trusting an already open session.
//
// On success the secret is cleared in memory only, through MutateUsers.
// "write memory" is what saves that.
//
// The returned bool tells the caller whether the account is now disabled,
// so it knows whether to keep retrying.
func finishTOTPDisable(ctx *command.AppContext, user *auth.User, password, code string, now time.Time) (bool, error) {
	if !auth.VerifyPassword(user.PasswordHash, password) || !auth.VerifyTOTPCode(user.TOTPSecret, code, now) {
		fmt.Println(ctx.Translator.T("totp.disable_denied"))
		return false, nil
	}

	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		users[ctx.Session.Username].TOTPSecret = ""
		return nil, nil
	}); err != nil {
		return false, err
	}
	// This only ever changes user.TOTPSecret, in memory, never
	// ctx.UsersFile on disk. See finishTOTPEnable's comment right above
	// for the same reasoning, in reverse: a reload or restart before
	// "write memory" leaves TOTP enabled again for this account.
	ctx.Logger.Debugln("DEBUG: TOTP disabled for user", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("totp.disable_confirm"))
	return true, nil
}
