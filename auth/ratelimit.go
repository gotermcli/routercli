// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"time"
)

// ----------------------------------------------------------------------
// Public Methods - RateLimiter
// ----------------------------------------------------------------------

// Allow - This method reports whether an attempt may proceed right now.
// When locked out, ok is false and retryAfter is how much longer the
// lockout has to run, which callers use to build a "try again in %s"
// message rather than a bare refusal. Calling Allow does not itself count
// as an attempt. A caller checks Allow before prompting for a password,
// then calls RecordFailure or RecordSuccess based on the actual outcome.
// See EnterCommandLevel and main.go's runLoop for the two real call sites.
func (r *RateLimiter) Allow() (ok bool, retryAfter time.Duration) {
	if r == nil || r.maxAttempts <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if now.Before(r.lockedUntil) {
		return false, r.lockedUntil.Sub(now)
	}
	return true, 0
}

// RecordFailure - This method records one failed attempt. If this failure
// brings the count of failures within the last window up to maxAttempts, a
// lockout starting now and lasting for lockout is triggered, and the next
// Allow call, and every one until the lockout expires, refuses. Failures
// older than window are pruned lazily here, which is what makes this a
// sliding window rather than a fixed one. Three failures spread across an
// hour never trigger a lockout meant for three failures in two minutes.
func (r *RateLimiter) RecordFailure() {
	if r == nil || r.maxAttempts <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.failures = pruneBefore(r.failures, now.Add(-r.window))
	r.failures = append(r.failures, now)

	if len(r.failures) >= r.maxAttempts {
		r.lockedUntil = now.Add(r.lockout)
		// The lockout itself is the point going forward. Once triggered,
		// the failure history that led to it is no longer needed, and
		// clearing it here means the very next failure after the lockout
		// expires starts counting a fresh window rather than immediately
		// re-triggering off stale history.
		r.failures = nil
	}
}

// RecordSuccess - This method clears failure history and any active
// lockout. A successful login, elevation, or password check resets the
// counter entirely, matching how a real account lockout normally works. An
// account does not stay almost locked out forever just because someone
// once mistyped a password a few times before eventually getting it right.
func (r *RateLimiter) RecordSuccess() {
	if r == nil || r.maxAttempts <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = nil
	r.lockedUntil = time.Time{}
}

// ----------------------------------------------------------------------
// Private Functions - RateLimiter
// ----------------------------------------------------------------------

// pruneBefore - This function returns the subset of times that are at or
// after cutoff, preserving order. Used by RecordFailure to drop failures
// that have aged out of the sliding window.
func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

// ----------------------------------------------------------------------
// Public Functions - KeyedRateLimiter
// ----------------------------------------------------------------------

// NewKeyedRateLimiter - This function constructs a KeyedRateLimiter.
// maxAttempts at or below zero disables rate limiting entirely, the same
// as RateLimiter.
func NewKeyedRateLimiter(maxAttempts int, window, lockout time.Duration) *KeyedRateLimiter {
	return &KeyedRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		lockout:     lockout,
		limiters:    make(map[string]*RateLimiter),
	}
}

// limiterFor - This method returns key's RateLimiter, creating one on
// first use.
func (k *KeyedRateLimiter) limiterFor(key string) *RateLimiter {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.limiters[key]
	if !ok {
		l = NewRateLimiter(k.maxAttempts, k.window, k.lockout)
		k.limiters[key] = l
	}
	return l
}

// Allow - This method reports whether an attempt for key may proceed right
// now. See RateLimiter.Allow.
func (k *KeyedRateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	if k == nil || k.maxAttempts <= 0 {
		return true, 0
	}
	return k.limiterFor(key).Allow()
}

// RecordFailure - This method records a failed attempt for key. See
// RateLimiter.RecordFailure.
func (k *KeyedRateLimiter) RecordFailure(key string) {
	if k == nil || k.maxAttempts <= 0 {
		return
	}
	k.limiterFor(key).RecordFailure()
}

// RecordSuccess - This method clears key's failure history and any
// lockout. See RateLimiter.RecordSuccess.
func (k *KeyedRateLimiter) RecordSuccess(key string) {
	if k == nil || k.maxAttempts <= 0 {
		return
	}
	k.limiterFor(key).RecordSuccess()
}
