// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package sanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ----------------------------------------------------------------------
// Public Functions - Escaping
// ----------------------------------------------------------------------

// Field - A delimited record, an audit line for instance, is structured by the
// bytes between its fields. Text carrying those bytes forges the structure
// rather than sitting inside it: a newline writes a whole second record, and a
// tab writes an extra field.
//
// This returns s with every control character replaced by a readable escape,
// so nothing in it can end a record or start a field. A backslash is escaped
// too, so the result is unambiguous about which backslashes were in the
// original.
//
// The escapes match Go's own string syntax, so a reader recognises them
// without being told the convention.
func Field(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Terminal - Text printed to a terminal is interpreted by it, not merely
// displayed. An escape sequence moves the cursor, erases what is already on
// screen, or changes the window title, so a value one session stored can
// rewrite what another session sees when it runs "show running-config".
//
// This returns s with everything a terminal would act on made inert. Control
// characters become the same readable escapes Field uses, which covers the
// escape character every ANSI sequence begins with, so the sequence is shown
// rather than obeyed.
//
// A newline is kept as itself. Callers print multi line output through this,
// and a listing whose line breaks were escaped would be unreadable. A record
// that must stay on one line wants Field instead.
func Terminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ----------------------------------------------------------------------
// Public Functions - Validation
// ----------------------------------------------------------------------

// ErrControlCharacter is returned by ValidateInput for text carrying a
// character the terminal, the audit log, or a configuration file would treat
// as structure rather than content.
type ErrControlCharacter struct {
	// Rune is the offending character, and Index is its byte offset, so a
	// caller can say which character rather than only that there was one.
	Rune  rune
	Index int
}

func (e *ErrControlCharacter) Error() string {
	return fmt.Sprintf("control character %q at byte %d is not allowed", e.Rune, e.Index)
}

// ErrInvalidUTF8 is returned by ValidateInput for text that is not valid UTF-8.
//
// Invalid UTF-8 is refused because what it means depends on who decodes it.
// Two readers disagreeing about the same bytes is exactly the ambiguity a CLI
// that stores, replays, and audits its input cannot afford.
type ErrInvalidUTF8 struct {
	Index int
}

func (e *ErrInvalidUTF8) Error() string {
	return fmt.Sprintf("invalid UTF-8 at byte %d", e.Index)
}

// ValidateInput - This function reports whether s is safe to accept as a
// command argument, a username, or any other text that will later be stored,
// replayed, or written to a log.
//
// It refuses invalid UTF-8, and any control character other than none: no
// newline, no tab, no escape, no carriage return. Every one of those is
// structure somewhere RouterCLI sends text, and none of them has a legitimate
// place inside a single argument.
//
// Refusing at the door is what keeps the rest simple. Text that passed this
// cannot forge an audit record, cannot inject a line into a configuration
// file, and cannot drive another operator's terminal, whichever sink it
// eventually reaches.
//
// It says nothing about length, spelling, or whether the value makes sense.
// MaxArgLength already bounds length, and what a valid hostname looks like is
// a deployment's decision.
func ValidateInput(s string) error {
	if !utf8.ValidString(s) {
		for i := range s {
			if r, size := utf8.DecodeRuneInString(s[i:]); r == utf8.RuneError && size <= 1 {
				return &ErrInvalidUTF8{Index: i}
			}
		}
		return &ErrInvalidUTF8{Index: 0}
	}
	for i, r := range s {
		if unicode.IsControl(r) {
			return &ErrControlCharacter{Rune: r, Index: i}
		}
	}
	return nil
}

// IsSafeInput - This function is ValidateInput as a boolean, for a caller that
// only needs to decide and has nothing to report.
func IsSafeInput(s string) bool { return ValidateInput(s) == nil }
