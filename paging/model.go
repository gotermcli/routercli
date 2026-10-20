// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// FilterMode - This type chooses how a FilterStage's Pattern is matched
// against one line of output. See FilterModeSubstring and FilterModeRegex.
type FilterMode int

const (
	// FilterModeSubstring matches a line that literally contains Pattern,
	// with no special characters. This is the default, because it is
	// predictable for an operator who wants a plain word search and never
	// has to think about escaping a character that happens to be a regular
	// expression metacharacter, such as a period in an IP address.
	FilterModeSubstring FilterMode = iota

	// FilterModeRegex matches a line where Pattern, compiled as an RE2
	// regular expression, matches anywhere in it. This is what Cisco and HP
	// do, so a deployment wanting vendor parity switches to it, either
	// through configuration at startup or "terminal filter-mode regex" at
	// runtime.
	FilterModeRegex
)

// FilterKind - This type names which of the three pipe filter keywords,
// "include", "exclude", or "begin", one FilterStage applies. See
// ApplyFilters for what each one does to a list of lines.
type FilterKind int

const (
	// FilterInclude keeps only a line that matches the pattern, discarding
	// every other line.
	FilterInclude FilterKind = iota

	// FilterExclude keeps only a line that does not match the pattern,
	// discarding every line that does.
	FilterExclude

	// FilterBegin discards every line before the first one that matches
	// the pattern, then keeps that line and everything after it. When no
	// line matches at all, the result is empty, matching real Cisco and HP
	// behavior for a "begin" pattern that never appears in the output.
	FilterBegin
)

// FilterStage - This type is one stage of a pipe filter pipeline, one "|
// include eth0" or "| begin interface" segment of a typed command line.
// ApplyFilters runs a list of these against a command's captured output,
// in order, each stage narrowing what the previous one already produced.
type FilterStage struct {
	Kind    FilterKind
	Pattern string
}
