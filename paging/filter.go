// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"regexp"
	"strings"
)

// ApplyFilters - This function runs every stage against lines, in order,
// each stage narrowing what the previous one produced. This is the same
// left to right pipeline a shell's "|" chain means, and the same thing a
// chained filter means on a real Cisco or HP device.
//
// lines is never mutated. Each stage produces a new slice.
//
// mode decides whether every Pattern is matched as a plain substring or
// compiled as a regular expression. One mode applies to the whole call; a
// session cannot mix modes within one chain.
//
// The only error returned is a regular expression that fails to compile,
// which is possible only in regex mode. A substring match has nothing to
// compile and cannot fail.
func ApplyFilters(lines []string, stages []FilterStage, mode FilterMode) ([]string, error) {
	for _, stage := range stages {
		matcher, err := newMatcher(stage.Pattern, mode)
		if err != nil {
			return nil, err
		}
		lines = applyStage(lines, stage.Kind, matcher)
	}
	return lines, nil
}

// matchFunc - This type is a compiled pattern, built once per stage by
// newMatcher and then reused for every line that stage looks at, rather
// than recompiling a regular expression, or re-deriving a substring check,
// once per line.
type matchFunc func(line string) bool

// newMatcher - This function builds the matchFunc a single FilterStage's
// Pattern runs, once, before applyStage walks every line. See FilterMode's
// doc comment for what each mode means.
func newMatcher(pattern string, mode FilterMode) (matchFunc, error) {
	if mode == FilterModeRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	return func(line string) bool { return strings.Contains(line, pattern) }, nil
}

// applyStage - This function runs one stage over lines.
//
// Include and Exclude are each an independent pass. Begin finds the first
// matching line and keeps it and everything after, discarding what came
// before, or returns nothing when no line matches, which is what Cisco and
// HP do for a "begin" pattern that never appears.
func applyStage(lines []string, kind FilterKind, matches matchFunc) []string {
	switch kind {
	case FilterExclude:
		var out []string
		for _, line := range lines {
			if !matches(line) {
				out = append(out, line)
			}
		}
		return out

	case FilterBegin:
		for i, line := range lines {
			if matches(line) {
				return lines[i:]
			}
		}
		return nil

	default: // FilterInclude
		var out []string
		for _, line := range lines {
			if matches(line) {
				out = append(out, line)
			}
		}
		return out
	}
}
