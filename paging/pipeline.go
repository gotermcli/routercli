// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"errors"
	"fmt"
	"strings"
)

// SplitPipeline - This function splits an already tokenized command line
// on every bare "|" token, the same convention a shell pipeline uses.
//
// cmdTokens is everything before the first "|", which the caller resolves
// against a command tree. Each group after it becomes one entry in
// segments, in order, holding one raw filter stage.
//
// A line with no "|" token returns tokens unchanged as cmdTokens and a nil
// segments, so a caller checks len(segments) to know whether filtering was
// requested.
//
// The split is purely syntactic. It knows where one segment ends and the
// next begins, not whether a segment is well formed. ParseStages turns a
// segment into a real FilterStage.
func SplitPipeline(tokens []string) (cmdTokens []string, segments [][]string) {
	var groups [][]string
	var current []string
	for _, t := range tokens {
		if t == "|" {
			groups = append(groups, current)
			current = nil
			continue
		}
		current = append(current, t)
	}
	groups = append(groups, current)

	if len(groups) == 1 {
		return groups[0], nil
	}
	return groups[0], groups[1:]
}

// ParseStages - This turns the raw filter groups SplitPipeline produced
// into a []FilterStage ready for ApplyFilters.
//
// maxDepth is the configured MaxFilterChainDepth, checked here rather than
// in ApplyFilters. Too many filters is an operator mistake worth reporting
// clearly, not something to truncate or run anyway. Zero disables
// filtering, so any segment at all is refused.
//
// Each segment MUST begin with exactly "include", "exclude", or "begin",
// case sensitive, like every other literal command word. Everything after
// that is joined with single spaces to form Pattern, so a pattern
// containing spaces needs no quoting: both `include "eth 0"` and
// `include eth 0` give the pattern `eth 0`, the same as a real device
// reading the rest of the line verbatim.
//
// An empty segment, a doubled "|", or a keyword with no pattern is an
// error rather than something silently ignored.
//
// Pattern is not checked for being a valid regular expression here,
// because the mode is chosen where a stage runs.
func ParseStages(segments [][]string, maxDepth int) ([]FilterStage, error) {
	if len(segments) == 0 {
		return nil, nil
	}

	if maxDepth <= 0 {
		return nil, errors.New("output filtering is disabled")
	}

	if len(segments) > maxDepth {
		return nil, fmt.Errorf("too many filters (%d), the maximum is %d", len(segments), maxDepth)
	}

	stages := make([]FilterStage, 0, len(segments))
	for _, segment := range segments {
		if len(segment) == 0 {
			return nil, errors.New("empty filter, expected 'include', 'exclude', or 'begin' followed by a pattern")
		}
		if len(segment) < 2 {
			return nil, fmt.Errorf("filter %q has no pattern to match against", segment[0])
		}

		var kind FilterKind
		switch segment[0] {
		case "include":
			kind = FilterInclude
		case "exclude":
			kind = FilterExclude
		case "begin":
			kind = FilterBegin
		default:
			return nil, fmt.Errorf("unknown filter %q, expected 'include', 'exclude', or 'begin'", segment[0])
		}

		stages = append(stages, FilterStage{Kind: kind, Pattern: strings.Join(segment[1:], " ")})
	}
	return stages, nil
}
