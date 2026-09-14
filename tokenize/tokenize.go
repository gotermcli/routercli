// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package tokenize

import (
	"fmt"
	"strings"
)

// Tokenize - A value can contain spaces. "set description "uplink to
// core"" has to parse as three tokens, not six, and whatever
// "show running-config" prints has to paste back in and tokenize to the
// same values it started from. Neither works without quote awareness.
//
// This splits a line into tokens the way a shell does. Unquoted
// whitespace separates them. A token wrapped in matching single or double
// quotes becomes one token with the quotes stripped, and inside a double
// quoted token a backslash escapes the quote character.
//
// An unterminated quote is an error rather than a guess, because guessing
// is how a configuration round trip goes quietly wrong.
func Tokenize(line string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inToken := false

	runes := []rune(line)
	i := 0
	for i < len(runes) {
		r := runes[i]

		switch {
		case r == ' ' || r == '\t':
			if inToken {
				tokens = append(tokens, current.String())
				current.Reset()
				inToken = false
			}
			i++

		case r == '"' || r == '\'':
			quote := r
			inToken = true
			i++
			closed := false
			for i < len(runes) {
				if runes[i] == '\\' && quote == '"' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
					current.WriteRune(runes[i+1])
					i += 2
					continue
				}
				if runes[i] == quote {
					closed = true
					i++
					break
				}
				current.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated %c quote in input", quote)
			}

		default:
			inToken = true
			current.WriteRune(r)
			i++
		}
	}

	if inToken {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// QuoteIfNeeded - This function is the inverse of Tokenize. Given a value,
// it returns a string that Tokenize parses back into exactly that value.
// This is what makes `show running-config` output pasteable back into the
// console.
//
// A value holding no whitespace, quote, or backslash is returned
// unchanged, so a simple command stays readable. Anything else is wrapped
// in double quotes, with internal backslashes and double quotes escaped.
//
// Backslashes MUST be escaped before quotes. Reversing the order double
// escapes the backslashes that Tokenize relies on and corrupts any value
// containing one. A value ending in a literal backslash, `path C:\`, has
// to become `"path C:\\"` to round trip. Escaping the quote first instead
// produces `"path C:\"`, which Tokenize reads as an escaped quote followed
// by an unterminated string.
func QuoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"'\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
