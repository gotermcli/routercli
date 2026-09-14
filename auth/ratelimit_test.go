// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"testing"
	"time"
)

// fakeClock - This type is a small, manually advanceable clock for
// deterministic rate limiter tests. See RateLimiter's doc comment for why
// this matters. It tests window and lockout expiry without real time.Sleep
// calls.
type fakeClock struct {
	t time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time          { return f.t }
func (f *fakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }

func newTestRateLimiter(maxAttempts int, window, lockout time.Duration, clock *fakeClock) *RateLimiter {
	r := NewRateLimiter(maxAttempts, window, lockout)
	r.now = clock.Now
	return r
}

// ----------------------------------------------------------------------
//
// RateLimiter
//
// ----------------------------------------------------------------------

// TestRateLimiterDisabledWhenMaxAttemptsIsZero - This test verifies that a
// zero maxAttempts disables rate limiting entirely, so Allow stays true no
// matter how many failures are recorded.
func TestRateLimiterDisabledWhenMaxAttemptsIsZero(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(0, time.Minute, time.Minute, clock)

	for i := 0; i < 100; i++ {
		r.RecordFailure()
	}
	ok, retryAfter := r.Allow()
	if !ok {
		t.Errorf("expected Allow to always return true when maxAttempts is 0, got ok=%v retryAfter=%v", ok, retryAfter)
	}
}

// TestRateLimiterDisabledWhenMaxAttemptsIsNegative - This test verifies
// that a negative maxAttempts also disables rate limiting, the same as
// zero.
func TestRateLimiterDisabledWhenMaxAttemptsIsNegative(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(-1, time.Minute, time.Minute, clock)
	r.RecordFailure()
	if ok, _ := r.Allow(); !ok {
		t.Error("expected Allow to always return true when maxAttempts is negative")
	}
}

// TestRateLimiterAllowsUpToMaxAttemptsBeforeLockout - This test verifies
// that Allow stays true while the number of recorded failures is still
// below maxAttempts.
func TestRateLimiterAllowsUpToMaxAttemptsBeforeLockout(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(3, 2*time.Minute, 5*time.Minute, clock)

	for i := 0; i < 2; i++ {
		if ok, _ := r.Allow(); !ok {
			t.Fatalf("expected Allow to be true before the 3rd failure (failure %d)", i+1)
		}
		r.RecordFailure()
	}
	// Two failures recorded, not locked out yet.
	if ok, _ := r.Allow(); !ok {
		t.Fatal("expected Allow to still be true after only 2 failures (maxAttempts is 3)")
	}
}

// TestRateLimiterLocksOutAfterMaxAttemptsWithinWindow - This test verifies
// that reaching maxAttempts failures inside a single window locks Allow to
// false and reports a positive retryAfter no larger than the configured
// lockout.
func TestRateLimiterLocksOutAfterMaxAttemptsWithinWindow(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(3, 2*time.Minute, 5*time.Minute, clock)

	r.RecordFailure()
	clock.Advance(10 * time.Second)
	r.RecordFailure()
	clock.Advance(10 * time.Second)
	r.RecordFailure() // 3rd failure within the 2-minute window - triggers lockout

	ok, retryAfter := r.Allow()
	if ok {
		t.Fatal("expected Allow to be false after 3 failures within the window")
	}
	if retryAfter <= 0 || retryAfter > 5*time.Minute {
		t.Errorf("retryAfter = %v, want something in (0, 5m]", retryAfter)
	}
}

// TestRateLimiterFailuresOutsideWindowDoNotAccumulate - This test verifies
// that a failure aging out of the sliding window stops counting toward
// maxAttempts, so a lockout is not triggered by failures spread out past
// the window.
func TestRateLimiterFailuresOutsideWindowDoNotAccumulate(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(3, 2*time.Minute, 5*time.Minute, clock)

	r.RecordFailure()
	clock.Advance(3 * time.Minute) // outside the 2 minute window, should age out
	r.RecordFailure()
	clock.Advance(1 * time.Second)
	r.RecordFailure()

	// Only 2 failures are within any 2 minute window at this point (the
	// first one aged out), so this should not be locked out yet.
	if ok, _ := r.Allow(); !ok {
		t.Error("expected Allow to still be true, the first failure should have aged out of the sliding window")
	}
}

// TestRateLimiterLockoutExpiresAfterLockoutDuration - This test verifies
// that Allow becomes true again on its own once the lockout duration has
// fully elapsed, with no RecordSuccess call needed.
func TestRateLimiterLockoutExpiresAfterLockoutDuration(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(2, time.Minute, 5*time.Minute, clock)

	r.RecordFailure()
	r.RecordFailure() // triggers lockout

	if ok, _ := r.Allow(); ok {
		t.Fatal("expected Allow to be false immediately after lockout triggers")
	}

	clock.Advance(5*time.Minute + time.Second)

	if ok, _ := r.Allow(); !ok {
		t.Error("expected Allow to be true again once the lockout duration has fully elapsed")
	}
}

// TestRateLimiterRecordSuccessClearsFailuresAndLockout - This test
// verifies that a successful attempt clears an active lockout immediately,
// and also clears the failure history behind it, rather than leaving old
// failures that would count toward the next lockout.
func TestRateLimiterRecordSuccessClearsFailuresAndLockout(t *testing.T) {
	clock := newFakeClock()
	r := newTestRateLimiter(2, time.Minute, 5*time.Minute, clock)

	r.RecordFailure()
	r.RecordFailure() // triggers lockout
	if ok, _ := r.Allow(); ok {
		t.Fatal("expected to be locked out before RecordSuccess")
	}

	r.RecordSuccess()

	if ok, _ := r.Allow(); !ok {
		t.Error("expected RecordSuccess to clear the lockout immediately")
	}

	// And the failure history is gone too, two more failures should be
	// needed to trigger another lockout, not just one.
	r.RecordFailure()
	if ok, _ := r.Allow(); !ok {
		t.Error("expected a single failure after RecordSuccess to not immediately relock (history was cleared)")
	}
}

// TestRateLimiterNilReceiverIsAlwaysAllowed - This test verifies that a
// nil *RateLimiter behaves exactly like a disabled one. This matters
// because command.CommandLevel and command.Command fields default to nil
// until explicitly wired up, so every call site needs to be safe to use
// before that wiring happens rather than needing its own nil check.
func TestRateLimiterNilReceiverIsAlwaysAllowed(t *testing.T) {
	var r *RateLimiter
	if ok, _ := r.Allow(); !ok {
		t.Error("expected a nil *RateLimiter's Allow to return true")
	}
	r.RecordFailure() // must not panic
	r.RecordSuccess() // must not panic
}

// ----------------------------------------------------------------------
//
// KeyedRateLimiter
//
// ----------------------------------------------------------------------

// TestKeyedRateLimiterIsolatesKeys - This test verifies that locking out
// one key leaves every other key unaffected. A shared limiter across
// usernames would itself be a denial-of-service vector, locking out an
// arbitrary user by failing their password from another session.
func TestKeyedRateLimiterIsolatesKeys(t *testing.T) {
	k := NewKeyedRateLimiter(2, time.Minute, 5*time.Minute)

	k.RecordFailure("alice")
	k.RecordFailure("alice") // locks out alice

	if ok, _ := k.Allow("alice"); ok {
		t.Fatal("expected alice to be locked out")
	}
	if ok, _ := k.Allow("bob"); !ok {
		t.Error("expected bob to be entirely unaffected by alice's lockout")
	}
}

// TestKeyedRateLimiterDisabledWhenMaxAttemptsIsZero - This test verifies
// that a zero maxAttempts disables rate limiting for every key, the same
// as a plain RateLimiter.
func TestKeyedRateLimiterDisabledWhenMaxAttemptsIsZero(t *testing.T) {
	k := NewKeyedRateLimiter(0, time.Minute, time.Minute)
	for i := 0; i < 10; i++ {
		k.RecordFailure("alice")
	}
	if ok, _ := k.Allow("alice"); !ok {
		t.Error("expected Allow to always return true when maxAttempts is 0")
	}
}

// TestKeyedRateLimiterRecordSuccessClearsOnlyThatKey - This test verifies
// that RecordSuccess clears the lockout and failure history for the key it
// is called with, while leaving a different key's lockout in place.
func TestKeyedRateLimiterRecordSuccessClearsOnlyThatKey(t *testing.T) {
	k := NewKeyedRateLimiter(2, time.Minute, 5*time.Minute)

	k.RecordFailure("alice")
	k.RecordFailure("alice") // locks out alice
	k.RecordFailure("bob")
	k.RecordFailure("bob") // locks out bob

	k.RecordSuccess("alice")

	if ok, _ := k.Allow("alice"); !ok {
		t.Error("expected RecordSuccess to clear alice's lockout")
	}
	if ok, _ := k.Allow("bob"); ok {
		t.Error("expected bob to still be locked out after only alice's RecordSuccess")
	}
}

// TestKeyedRateLimiterNilReceiverIsAlwaysAllowed - This test verifies that
// a nil *KeyedRateLimiter behaves like a disabled one, and that its
// methods are safe to call on a nil receiver without panicking.
func TestKeyedRateLimiterNilReceiverIsAlwaysAllowed(t *testing.T) {
	var k *KeyedRateLimiter
	if ok, _ := k.Allow("alice"); !ok {
		t.Error("expected a nil *KeyedRateLimiter's Allow to return true")
	}
	k.RecordFailure("alice") // must not panic
	k.RecordSuccess("alice") // must not panic
}
