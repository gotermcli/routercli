// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"strings"
	"testing"

	"github.com/gotermcli/routercli/auth"
)

// TestPrintPasswordChangeRetryPrintsWhenAttemptsRemain - This test
// verifies that printPasswordChangeRetry prints something when attempt is
// not yet the last of maxAttempts, ctx built by newTestContext carries no
// Translator, see that helper's doc comment, so the printed text is the
// bracketed i18n placeholder rather than a fully translated sentence,
// still enough to confirm the right message key is looked up at all, and
// only when there is another attempt left.
func TestPrintPasswordChangeRetryPrintsWhenAttemptsRemain(t *testing.T) {
	ctx := newTestContext()

	out := captureStdout(t, func() { printPasswordChangeRetry(ctx, 3, 1) })

	if !strings.Contains(out, "password.change.retry") {
		t.Errorf("expected output naming the retry message key with attempts remaining, got %q", out)
	}
}

// TestPrintPasswordChangeRetrySilentOnLastAttempt - This test verifies
// that printPasswordChangeRetry prints nothing at all once attempt is the
// final one, maxAttempts-attempt no longer positive, since
// runPasswordChange's closing "attempts_exhausted" message covers that
// case instead, see this function's doc comment.
func TestPrintPasswordChangeRetrySilentOnLastAttempt(t *testing.T) {
	ctx := newTestContext()

	out := captureStdout(t, func() { printPasswordChangeRetry(ctx, 3, 3) })

	if out != "" {
		t.Errorf("expected no output on the final attempt, got %q", out)
	}
}

// TestPrintPasswordViolationsPrintsOneLinePerViolation - This test
// verifies that printPasswordViolations prints one line naming each entry
// in violations, in order, rather than collapsing them into a single
// message or stopping at the first.
func TestPrintPasswordViolationsPrintsOneLinePerViolation(t *testing.T) {
	ctx := newTestContext()
	ctx.PasswordPolicy = auth.PasswordPolicy{MinLength: 10}
	violations := []auth.PasswordViolation{
		auth.PasswordViolationTooShort,
		auth.PasswordViolationNeedsUppercase,
		auth.PasswordViolationNeedsNumber,
	}

	out := captureStdout(t, func() { printPasswordViolations(ctx, violations) })

	for _, want := range []string{"violation_too_short", "violation_needs_uppercase", "violation_needs_number"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to mention %q, got %q", want, out)
		}
	}
}

// TestPrintPasswordViolationsUnrecognizedValuePrintsRawValue - This test
// verifies that printPasswordViolations still prints something for a
// PasswordViolation value with no matching case, its own raw string,
// rather than silently dropping it, see this function's doc comment on why
// an unrecognized value must never go unreported. ValidatePassword itself
// never produces a value like this today, this exercises the default
// branch directly.
func TestPrintPasswordViolationsUnrecognizedValuePrintsRawValue(t *testing.T) {
	ctx := newTestContext()
	unknown := auth.PasswordViolation("some_future_rule")

	out := captureStdout(t, func() { printPasswordViolations(ctx, []auth.PasswordViolation{unknown}) })

	if !strings.Contains(out, "some_future_rule") {
		t.Errorf("expected the unrecognized violation's raw value in the output, got %q", out)
	}
}

// TestPrintPasswordViolationsEmptyPrintsNothing - This test verifies that
// printPasswordViolations prints nothing at all for an empty violations
// slice.
func TestPrintPasswordViolationsEmptyPrintsNothing(t *testing.T) {
	ctx := newTestContext()

	out := captureStdout(t, func() { printPasswordViolations(ctx, nil) })

	if out != "" {
		t.Errorf("expected no output for an empty violations slice, got %q", out)
	}
}
