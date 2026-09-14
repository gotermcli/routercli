// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package tokenize

import (
	"reflect"
	"testing"
)

// TestTokenizeBasic - This test verifies that Tokenize splits on
// whitespace, collapses repeated whitespace, and returns nil for an empty
// or whitespace-only line.
func TestTokenizeBasic(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"show version", []string{"show", "version"}},
		{"  show   version  ", []string{"show", "version"}},
		{"", nil},
		{"   ", nil},
	}
	for _, c := range cases {
		got, err := Tokenize(c.in)
		if err != nil {
			t.Fatalf("Tokenize(%q) returned error: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestTokenizeQuoted - This test verifies that Tokenize handles double-and
// single-quoted values, embedded escaped quotes, an empty quoted value,
// and a quote ending mid-token, the same way a shell would.
func TestTokenizeQuoted(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`set description "uplink to core"`, []string{"set", "description", "uplink to core"}},
		{`set description 'uplink to core'`, []string{"set", "description", "uplink to core"}},
		{`set description "has \"embedded\" quotes"`, []string{"set", "description", `has "embedded" quotes`}},
		{`set description ""`, []string{"set", "description", ""}},
		{`set x "a"b`, []string{"set", "x", "ab"}}, // quote can end mid-token, same as a shell
	}
	for _, c := range cases {
		got, err := Tokenize(c.in)
		if err != nil {
			t.Fatalf("Tokenize(%q) returned error: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokenize(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// TestTokenizeUnterminatedQuote - This test verifies that a line with an
// opening quote and no closing quote returns an error rather than silently
// consuming the rest of the line.
func TestTokenizeUnterminatedQuote(t *testing.T) {
	_, err := Tokenize(`set description "uplink to core`)
	if err == nil {
		t.Fatal("expected an error for an unterminated quote, got nil")
	}
}

// TestQuoteRoundTrip - This test covers the property the "show config"
// use case depends on: for any value, QuoteIfNeeded fed back through
// Tokenize must produce exactly that value, with no loss. That is the copy
// out, paste back in requirement.
func TestQuoteRoundTrip(t *testing.T) {
	values := []string{
		"eth0",
		"uplink to core",
		`has "embedded" quotes`,
		"has\ttab",
		"",
		"has'single'quotes",
		// A value ending in a literal backslash is the case that breaks if
		// backslashes are not escaped before quotes. The trailing `\"` then
		// reads as an escaped quote followed by an unterminated string.
		`path C:\`,
		`back\\slash`,
		`a\"b`, // backslash immediately followed by a literal quote
		`\`,
	}
	for _, v := range values {
		line := "set description " + QuoteIfNeeded(v)
		tokens, err := Tokenize(line)
		if err != nil {
			t.Fatalf("round trip Tokenize(%q) failed: %v", line, err)
		}
		if len(tokens) != 3 {
			t.Fatalf("round trip Tokenize(%q) = %#v, want 3 tokens", line, tokens)
		}
		if tokens[2] != v {
			t.Errorf("round trip failed: put in %q, got back %q (via line %q)", v, tokens[2], line)
		}
	}
}
