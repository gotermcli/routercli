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
// SplitPipeline
//
// ----------------------------------------------------------------------

// TestSplitPipelineNoPipeReturnsTokensUnchanged - This test verifies that
// a command line with no "|" token at all returns every token as cmdTokens
// with a nil segments, so a caller can check len(segments) == 0 to know no
// filtering was requested, exactly as SplitPipeline's doc comment
// promises.
func TestSplitPipelineNoPipeReturnsTokensUnchanged(t *testing.T) {
	cmdTokens, segments := SplitPipeline([]string{"show", "running-config"})

	if !reflect.DeepEqual(cmdTokens, []string{"show", "running-config"}) {
		t.Errorf("cmdTokens = %v, want [show running-config]", cmdTokens)
	}
	if segments != nil {
		t.Errorf("segments = %v, want nil", segments)
	}
}

// TestSplitPipelineSingleFilterSplitsIntoTwoGroups - This test verifies
// the ordinary single filter case, "show running-config | include eth0",
// splits into one command group and exactly one segment.
func TestSplitPipelineSingleFilterSplitsIntoTwoGroups(t *testing.T) {
	cmdTokens, segments := SplitPipeline([]string{"show", "running-config", "|", "include", "eth0"})

	if !reflect.DeepEqual(cmdTokens, []string{"show", "running-config"}) {
		t.Errorf("cmdTokens = %v, want [show running-config]", cmdTokens)
	}
	want := [][]string{{"include", "eth0"}}
	if !reflect.DeepEqual(segments, want) {
		t.Errorf("segments = %v, want %v", segments, want)
	}
}

// TestSplitPipelineChainedFiltersSplitIntoOneSegmentEach - This test
// verifies that a chain of several "|" separated filters, not just one,
// produces one segment per filter, in the order they were typed, the shape
// ParseStages expects to walk.
func TestSplitPipelineChainedFiltersSplitIntoOneSegmentEach(t *testing.T) {
	cmdTokens, segments := SplitPipeline([]string{
		"show", "running-config", "|", "include", "eth", "|", "exclude", "shutdown",
	})

	if !reflect.DeepEqual(cmdTokens, []string{"show", "running-config"}) {
		t.Errorf("cmdTokens = %v, want [show running-config]", cmdTokens)
	}
	want := [][]string{{"include", "eth"}, {"exclude", "shutdown"}}
	if !reflect.DeepEqual(segments, want) {
		t.Errorf("segments = %v, want %v", segments, want)
	}
}

// TestSplitPipelineEmptySegmentBetweenTwoBarsIsPreserved - This test
// verifies that a doubled "|", with nothing between the two bars, produces
// an empty segment rather than being silently collapsed away, so
// ParseStages, not SplitPipeline itself, is the one place that reports
// this as the real user error it is. See SplitPipeline's doc comment: it
// is purely syntactic.
func TestSplitPipelineEmptySegmentBetweenTwoBarsIsPreserved(t *testing.T) {
	_, segments := SplitPipeline([]string{"show", "version", "|", "|", "include", "x"})

	want := [][]string{nil, {"include", "x"}}
	if !reflect.DeepEqual(segments, want) {
		t.Errorf("segments = %v, want %v", segments, want)
	}
}

// TestSplitPipelineLeadingBarProducesEmptyCommandGroup - This test
// verifies that a "|" as the very first token still splits cleanly,
// leaving cmdTokens empty rather than panicking or dropping the filter
// that follows it. Whether an empty command group resolves to anything is
// command.Resolve's concern, not SplitPipeline's.
func TestSplitPipelineLeadingBarProducesEmptyCommandGroup(t *testing.T) {
	cmdTokens, segments := SplitPipeline([]string{"|", "include", "x"})

	if cmdTokens != nil {
		t.Errorf("cmdTokens = %v, want nil", cmdTokens)
	}
	want := [][]string{{"include", "x"}}
	if !reflect.DeepEqual(segments, want) {
		t.Errorf("segments = %v, want %v", segments, want)
	}
}

// TestSplitPipelineEmptyInputReturnsNoGroupsAtAll - This test verifies the
// degenerate empty token list, nothing typed at all, returns a nil
// cmdTokens and a nil segments, rather than a single empty group being
// mistaken for one real, if empty, command.
func TestSplitPipelineEmptyInputReturnsNoGroupsAtAll(t *testing.T) {
	cmdTokens, segments := SplitPipeline(nil)

	if cmdTokens != nil {
		t.Errorf("cmdTokens = %v, want nil", cmdTokens)
	}
	if segments != nil {
		t.Errorf("segments = %v, want nil", segments)
	}
}

// ----------------------------------------------------------------------
//
// ParseStages
//
// ----------------------------------------------------------------------

// TestParseStagesNoSegmentsReturnsNilWithNoError - This test verifies that
// len(segments) == 0, the "no pipe at all" case SplitPipeline itself
// already reports, is never an error here, even when maxDepth is zero,
// filtering disabled entirely. A command with no "|" typed at all must
// always run normally regardless of this deployment's filtering
// configuration.
func TestParseStagesNoSegmentsReturnsNilWithNoError(t *testing.T) {
	stages, err := ParseStages(nil, 0)
	if err != nil {
		t.Fatalf("ParseStages(nil, 0) returned unexpected error: %v", err)
	}
	if stages != nil {
		t.Errorf("stages = %v, want nil", stages)
	}
}

// TestParseStagesZeroMaxDepthRejectsAnySegment - This test verifies that
// maxDepth <= 0 refuses even a single, otherwise well formed segment,
// config.SystemConfig.MaxFilterChainDepth's "filtering disabled entirely"
// value.
func TestParseStagesZeroMaxDepthRejectsAnySegment(t *testing.T) {
	_, err := ParseStages([][]string{{"include", "eth0"}}, 0)
	if err == nil {
		t.Fatal("expected an error when maxDepth is zero, got nil")
	}
}

// TestParseStagesTooManySegmentsIsRejected - This test verifies the chain
// depth limit itself: with maxDepth 2, a third chained filter is refused
// with a clear error rather than silently truncated or run anyway, exactly
// the security concern behind that limit.
func TestParseStagesTooManySegmentsIsRejected(t *testing.T) {
	segments := [][]string{{"include", "a"}, {"include", "b"}, {"include", "c"}}
	_, err := ParseStages(segments, 2)
	if err == nil {
		t.Fatal("expected an error for a filter chain deeper than maxDepth, got nil")
	}
}

// TestParseStagesAtExactlyMaxDepthSucceeds - This test verifies the
// boundary itself: exactly maxDepth segments is accepted, not just
// maxDepth - 1, confirming TestParseStagesTooManySegmentsIsRejected above
// is testing the real edge, one past the limit, not something further out.
func TestParseStagesAtExactlyMaxDepthSucceeds(t *testing.T) {
	segments := [][]string{{"include", "a"}, {"exclude", "b"}}
	stages, err := ParseStages(segments, 2)
	if err != nil {
		t.Fatalf("ParseStages at exactly maxDepth returned unexpected error: %v", err)
	}
	if len(stages) != 2 {
		t.Errorf("len(stages) = %d, want 2", len(stages))
	}
}

// TestParseStagesRecognizesAllThreeKeywords - This test verifies that
// "include", "exclude", and "begin" each resolve to their own FilterKind,
// the only three real Cisco and HP pipe filter keywords this project
// supports.
func TestParseStagesRecognizesAllThreeKeywords(t *testing.T) {
	segments := [][]string{{"include", "a"}, {"exclude", "b"}, {"begin", "c"}}
	stages, err := ParseStages(segments, 3)
	if err != nil {
		t.Fatalf("ParseStages returned unexpected error: %v", err)
	}
	want := []FilterStage{
		{Kind: FilterInclude, Pattern: "a"},
		{Kind: FilterExclude, Pattern: "b"},
		{Kind: FilterBegin, Pattern: "c"},
	}
	if !reflect.DeepEqual(stages, want) {
		t.Errorf("stages = %+v, want %+v", stages, want)
	}
}

// TestParseStagesUnknownKeywordIsRejected - This test verifies that a
// first token other than "include", "exclude", or "begin" is a real error,
// not silently treated as one of the three, and that no abbreviation,
// "inc" for "include" for instance, is accepted, since this project's
// convention is that every literal command word is matched in full, case
// sensitively.
func TestParseStagesUnknownKeywordIsRejected(t *testing.T) {
	for _, first := range []string{"inc", "Include", "grep", "INCLUDE"} {
		_, err := ParseStages([][]string{{first, "eth0"}}, 2)
		if err == nil {
			t.Errorf("expected an error for filter keyword %q, got nil", first)
		}
	}
}

// TestParseStagesMissingPatternIsRejected - This test verifies that a
// segment holding only the keyword, no pattern at all, "| include" with
// nothing after it, is a real error rather than an empty string pattern
// that would otherwise match every line.
func TestParseStagesMissingPatternIsRejected(t *testing.T) {
	_, err := ParseStages([][]string{{"include"}}, 2)
	if err == nil {
		t.Fatal("expected an error for a filter with no pattern, got nil")
	}
}

// TestParseStagesEmptySegmentIsRejected - This test verifies that an
// entirely empty segment, produced by a doubled "|" on the command line,
// is a real, reported error, not silently skipped.
func TestParseStagesEmptySegmentIsRejected(t *testing.T) {
	_, err := ParseStages([][]string{nil}, 2)
	if err == nil {
		t.Fatal("expected an error for an empty filter segment, got nil")
	}
}

// TestParseStagesMultiWordPatternIsJoinedWithSpaces - This test verifies
// that a pattern spanning several tokens, "include eth 0" for instance, is
// joined back together with a single space each, so "include \"eth 0\""
// and "include eth 0" both produce the identical pattern, matching how a
// real Cisco or HP device reads the rest of the line verbatim once it sees
// the keyword.
func TestParseStagesMultiWordPatternIsJoinedWithSpaces(t *testing.T) {
	stages, err := ParseStages([][]string{{"include", "eth", "0", "up"}}, 2)
	if err != nil {
		t.Fatalf("ParseStages returned unexpected error: %v", err)
	}
	if len(stages) != 1 || stages[0].Pattern != "eth 0 up" {
		t.Errorf("stages = %+v, want a single stage with Pattern %q", stages, "eth 0 up")
	}
}
