// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package sanitize

import (
	"strings"
	"testing"
)

// TestFieldCannotForgeARecord - This test verifies the property Field exists
// for: text containing the bytes a delimited record is structured by comes
// back unable to end that record or start a field inside it.
//
// The input is a real audit forgery attempt, a command carrying a newline and
// the tabs needed to look like a complete second entry.
func TestFieldCannotForgeARecord(t *testing.T) {
	forged := "show version\n2020-01-01T00:00:00Z\troot\tuid=0\tOK\terase startup-config"
	got := Field(forged)

	if strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("Field left a structural character in place: %q", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("the newline should be shown escaped, got %q", got)
	}
	if !strings.Contains(got, "erase startup-config") {
		t.Errorf("the original text should still be readable, got %q", got)
	}
}

// TestFieldEscapesABackslashSoTheResultIsUnambiguous - This test verifies that
// a backslash already in the input is escaped.
//
// Without it, a value containing a literal backslash n would be indis-
// tinguishable from an escaped newline, which means a reader cannot tell what
// was actually submitted.
func TestFieldEscapesABackslashSoTheResultIsUnambiguous(t *testing.T) {
	if got, want := Field(`a\nb`), `a\\nb`; got != want {
		t.Errorf("Field(%q) = %q, want %q", `a\nb`, got, want)
	}
	if got, want := Field("a\nb"), `a\nb`; got != want {
		t.Errorf("Field(%q) = %q, want %q", "a\nb", got, want)
	}
}

// TestTerminalMakesAnEscapeSequenceInert - This test verifies that text a
// terminal would act on is shown rather than obeyed.
//
// The sequence below clears the screen and repaints, which is how one session
// could rewrite what another sees in the middle of "show running-config".
func TestTerminalMakesAnEscapeSequenceInert(t *testing.T) {
	got := Terminal("\x1b[2J\x1b[31mPWNED\x07")

	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("Terminal left the escape character in place: %q", got)
	}
	if strings.ContainsRune(got, 0x07) {
		t.Errorf("Terminal left the bell character in place: %q", got)
	}
	if !strings.Contains(got, "PWNED") {
		t.Errorf("the readable text should survive, got %q", got)
	}
}

// TestTerminalKeepsNewlinesSoMultiLineOutputStaysReadable - This test verifies
// that Terminal does not escape newlines.
//
// Callers print multi line listings through it, and a listing whose line
// breaks were escaped would be one long unreadable line. A record that must
// stay on one line wants Field instead.
func TestTerminalKeepsNewlinesSoMultiLineOutputStaysReadable(t *testing.T) {
	if got, want := Terminal("one\ntwo"), "one\ntwo"; got != want {
		t.Errorf("Terminal(%q) = %q, want %q", "one\ntwo", got, want)
	}
}

// TestValidateInputRefusesEveryControlCharacter - This test verifies that no
// control character is accepted as input, and that the error names which one
// and where.
func TestValidateInputRefusesEveryControlCharacter(t *testing.T) {
	for _, in := range []string{"a\nb", "a\tb", "a\rb", "a\x1bb", "a\x00b", "a\x7fb"} {
		err := ValidateInput(in)
		if err == nil {
			t.Errorf("ValidateInput(%q) returned nil, want an error", in)
			continue
		}
		var ce *ErrControlCharacter
		if !asControl(err, &ce) {
			t.Errorf("ValidateInput(%q) = %v, want an ErrControlCharacter", in, err)
			continue
		}
		if ce.Index != 1 {
			t.Errorf("ValidateInput(%q) reported index %d, want 1", in, ce.Index)
		}
	}
}

// TestValidateInputRefusesInvalidUTF8 - This test verifies that bytes which
// are not valid UTF-8 are refused.
//
// What they mean depends on who decodes them, and two readers disagreeing
// about the same bytes is the ambiguity a CLI that stores, replays, and audits
// its input cannot afford.
func TestValidateInputRefusesInvalidUTF8(t *testing.T) {
	if err := ValidateInput("ok\xff\xfe"); err == nil {
		t.Error("invalid UTF-8 should be refused")
	}
}

// TestValidateInputAcceptsOrdinaryText - This test verifies the check is no
// broader than it needs to be: spaces, punctuation, and non-ASCII letters are
// all ordinary content.
func TestValidateInputAcceptsOrdinaryText(t *testing.T) {
	for _, in := range []string{
		"show running-config", "uplink to core", "café", "日本語",
		`a "quoted" value`, "", "192.168.1.1/24",
	} {
		if err := ValidateInput(in); err != nil {
			t.Errorf("ValidateInput(%q) = %v, want nil", in, err)
		}
	}
}

func asControl(err error, target **ErrControlCharacter) bool {
	ce, ok := err.(*ErrControlCharacter)
	if ok {
		*target = ce
	}
	return ok
}

// TestFieldEscapesEveryControlCharacter - This test verifies the general case
// rather than the few characters named individually above.
//
// An escape sequence begins with 0x1b, a bell is 0x07, and a null is 0x00.
// None of them ends a record on its own, but all of them reach a terminal when
// an audit log is read with cat, so none belongs in a stored record raw.
func TestFieldEscapesEveryControlCharacter(t *testing.T) {
	for r := rune(0); r < 0x20; r++ {
		got := Field(string(r))
		if strings.ContainsRune(got, r) && r != 0 {
			t.Errorf("Field left control character %#x in place: %q", r, got)
		}
		if !strings.HasPrefix(got, `\`) {
			t.Errorf("Field(%#x) = %q, want an escape", r, got)
		}
	}
	if got := Field("\x7f"); got != `\x7f` {
		t.Errorf("Field(DEL) = %q, want %q", got, `\x7f`)
	}
}

// TestTerminalEscapesEveryControlCharacterExceptNewline - This test verifies
// the same for Terminal, and pins the one deliberate exception.
func TestTerminalEscapesEveryControlCharacterExceptNewline(t *testing.T) {
	for r := rune(1); r < 0x20; r++ {
		if r == '\n' {
			continue
		}
		got := Terminal(string(r))
		if strings.ContainsRune(got, r) {
			t.Errorf("Terminal left control character %#x in place: %q", r, got)
		}
	}
	if got := Terminal("\n"); got != "\n" {
		t.Errorf("Terminal should keep a newline, got %q", got)
	}
}

// TestValidationErrorsNameTheProblem - This test verifies that both error
// types produce a message a person can act on.
//
// An operator seeing a refusal needs to know which character and where, not
// merely that something was rejected, since a control character is invisible
// in whatever they pasted.
func TestValidationErrorsNameTheProblem(t *testing.T) {
	err := ValidateInput("ab\x1bc")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2") {
		t.Errorf("the message should name the position, got %q", msg)
	}

	utfErr := ValidateInput("ok\xff")
	if utfErr == nil {
		t.Fatal("expected an error for invalid UTF-8")
	}
	if !strings.Contains(utfErr.Error(), "UTF-8") {
		t.Errorf("the message should name the problem, got %q", utfErr.Error())
	}
}

// TestIsSafeInputMatchesValidateInput - This test verifies the boolean form
// agrees with the error form, so a caller picking either gets the same answer.
func TestIsSafeInputMatchesValidateInput(t *testing.T) {
	for _, in := range []string{"fine", "a\nb", "café", "\x00", ""} {
		if IsSafeInput(in) != (ValidateInput(in) == nil) {
			t.Errorf("IsSafeInput(%q) disagrees with ValidateInput", in)
		}
	}
}
