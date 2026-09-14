// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"reflect"
	"testing"
)

// ----------------------------------------------------------------------
//
// ApplyFilters - substring mode
//
// ----------------------------------------------------------------------

// TestApplyFiltersIncludeSubstringKeepsOnlyMatchingLines - This test
// verifies the ordinary "| include" case in the default substring mode:
// only a line that literally contains Pattern survives, in its original
// order.
func TestApplyFiltersIncludeSubstringKeepsOnlyMatchingLines(t *testing.T) {
	lines := []string{"interface eth0", "interface eth1", "hostname router1"}
	stages := []FilterStage{{Kind: FilterInclude, Pattern: "eth"}}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"interface eth0", "interface eth1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestApplyFiltersExcludeSubstringDropsMatchingLines - This test verifies
// "| exclude" keeps everything except the lines that match, the mirror
// image of TestApplyFiltersIncludeSubstringKeepsOnlyMatchingLines.
func TestApplyFiltersExcludeSubstringDropsMatchingLines(t *testing.T) {
	lines := []string{"interface eth0", "interface eth1", "hostname router1"}
	stages := []FilterStage{{Kind: FilterExclude, Pattern: "eth"}}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"hostname router1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestApplyFiltersBeginSubstringKeepsFirstMatchOnward - This test verifies
// "| begin" discards every line before the first match, then keeps that
// line and everything after it, matching real Cisco and HP behavior.
func TestApplyFiltersBeginSubstringKeepsFirstMatchOnward(t *testing.T) {
	lines := []string{"! header", "hostname router1", "interface eth0", "interface eth1"}
	stages := []FilterStage{{Kind: FilterBegin, Pattern: "interface"}}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"interface eth0", "interface eth1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestApplyFiltersBeginSubstringNoMatchReturnsEmpty - This test verifies
// that "| begin" with a pattern that never appears in the output returns
// an empty result, not the whole, unfiltered input, matching real Cisco
// and HP behavior for a "begin" pattern that never appears.
func TestApplyFiltersBeginSubstringNoMatchReturnsEmpty(t *testing.T) {
	lines := []string{"a", "b", "c"}
	stages := []FilterStage{{Kind: FilterBegin, Pattern: "nowhere"}}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want an empty result", got)
	}
}

// TestApplyFiltersSubstringPatternWithRegexMetacharactersMatchesLiterally
// - This test verifies substring mode's whole reason for existing as
// the default: a pattern containing a real regular expression
// metacharacter, a period in an IP address for instance, is matched
// completely literally, with no special meaning given to it at all.
func TestApplyFiltersSubstringPatternWithRegexMetacharactersMatchesLiterally(t *testing.T) {
	lines := []string{"ip address 10.0.0.1", "ip address 10X0X0X1"}
	stages := []FilterStage{{Kind: FilterInclude, Pattern: "10.0.0.1"}}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"ip address 10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v, the period must not act as a regex wildcard in substring mode", got, want)
	}
}

// ----------------------------------------------------------------------
//
// ApplyFilters - regex mode
//
// ----------------------------------------------------------------------

// TestApplyFiltersIncludeRegexMatchesPattern - This test verifies that
// FilterModeRegex compiles Pattern as a real Go RE2 regular expression and
// keeps only a line it matches anywhere in.
func TestApplyFiltersIncludeRegexMatchesPattern(t *testing.T) {
	lines := []string{"interface eth0", "interface eth1", "interface gi0/1"}
	stages := []FilterStage{{Kind: FilterInclude, Pattern: `eth[0-9]$`}}

	got, err := ApplyFilters(lines, stages, FilterModeRegex)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"interface eth0", "interface eth1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestApplyFiltersRegexInvalidPatternReturnsError - This test verifies the
// one error ApplyFilters itself documents: an unparseable regular
// expression, an unclosed bracket expression here, is reported back rather
// than panicking or silently matching nothing.
func TestApplyFiltersRegexInvalidPatternReturnsError(t *testing.T) {
	stages := []FilterStage{{Kind: FilterInclude, Pattern: "eth["}}

	_, err := ApplyFilters([]string{"interface eth0"}, stages, FilterModeRegex)
	if err == nil {
		t.Fatal("expected an error for an invalid regular expression, got nil")
	}
}

// TestApplyFiltersSameMetacharacterPatternDiffersBetweenModes - This test
// verifies substring and regex mode behave differently for the exact same
// pattern, the whole point of offering both: "eth." matches only a literal
// "eth." in substring mode, but matches "eth" followed by any character at
// all in regex mode.
func TestApplyFiltersSameMetacharacterPatternDiffersBetweenModes(t *testing.T) {
	lines := []string{"eth0", "eth1", "ethX"}
	stages := []FilterStage{{Kind: FilterInclude, Pattern: "eth."}}

	substringGot, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters (substring) returned unexpected error: %v", err)
	}
	if len(substringGot) != 0 {
		t.Errorf("substring mode got %v, want no matches, \"eth.\" is not a literal substring of any line", substringGot)
	}

	regexGot, err := ApplyFilters(lines, stages, FilterModeRegex)
	if err != nil {
		t.Fatalf("ApplyFilters (regex) returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(regexGot, lines) {
		t.Errorf("regex mode got %v, want %v, \".\" should match any single character", regexGot, lines)
	}
}

// ----------------------------------------------------------------------
//
// ApplyFilters - chaining
//
// ----------------------------------------------------------------------

// TestApplyFiltersChainsStagesInOrder - This test verifies that several
// stages narrow the result left to right, each one working against what
// the previous stage already produced, not the original, unfiltered lines.
func TestApplyFiltersChainsStagesInOrder(t *testing.T) {
	lines := []string{
		"interface eth0",
		" shutdown",
		"interface eth1",
		" description uplink",
	}
	stages := []FilterStage{
		{Kind: FilterBegin, Pattern: "eth1"},
		{Kind: FilterExclude, Pattern: "shutdown"},
	}

	got, err := ApplyFilters(lines, stages, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	want := []string{"interface eth1", " description uplink"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestApplyFiltersEmptyStagesReturnsLinesUnchanged - This test verifies
// that an empty, or nil, stages slice is a plain no-op, returning lines
// exactly as given, the case main.go's dispatch hits for a command with no
// "|" typed at all.
func TestApplyFiltersEmptyStagesReturnsLinesUnchanged(t *testing.T) {
	lines := []string{"a", "b", "c"}
	got, err := ApplyFilters(lines, nil, FilterModeSubstring)
	if err != nil {
		t.Fatalf("ApplyFilters returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, lines) {
		t.Errorf("got %v, want %v unchanged", got, lines)
	}
}
