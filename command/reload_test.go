// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"sync"
	"testing"
	"time"
)

// TestNewPendingReloadHasNothingPending - This test verifies that a
// freshly constructed PendingReload reports nothing pending, and that
// FireChannel returns a channel with nothing waiting on it.
func TestNewPendingReloadHasNothingPending(t *testing.T) {
	p := NewPendingReload()

	if p.Pending() {
		t.Error("expected a freshly constructed PendingReload to report Pending() == false")
	}
	select {
	case <-p.FireChannel():
		t.Error("expected FireChannel() to have nothing waiting on it yet")
	default:
	}
}

// TestPendingReloadScheduleSetsPending - This test verifies that Schedule
// marks a reload as pending immediately, well before its own delay has
// elapsed.
func TestPendingReloadScheduleSetsPending(t *testing.T) {
	p := NewPendingReload()

	p.Schedule(time.Hour)
	defer p.Cancel()

	if !p.Pending() {
		t.Error("expected Pending() == true right after Schedule")
	}
}

// TestPendingReloadCancelStopsBeforeFiring - This test verifies that
// Cancel, called well before a scheduled delay elapses, both reports true
// and prevents the reload from ever firing.
func TestPendingReloadCancelStopsBeforeFiring(t *testing.T) {
	p := NewPendingReload()

	p.Schedule(50 * time.Millisecond)
	if !p.Cancel() {
		t.Fatal("expected Cancel() == true when a reload was actually pending")
	}
	if p.Pending() {
		t.Error("expected Pending() == false immediately after Cancel")
	}

	select {
	case <-p.FireChannel():
		t.Error("expected a cancelled reload to never fire")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestPendingReloadCancelWithNothingPendingReturnsFalse - This test
// verifies that Cancel reports false, rather than panicking or doing
// anything else, when nothing was scheduled, the same case
// example/cmd/core/cmd_admin.go's "no reload" handler reports back to the session
// as an error.
func TestPendingReloadCancelWithNothingPendingReturnsFalse(t *testing.T) {
	p := NewPendingReload()

	if p.Cancel() {
		t.Error("expected Cancel() == false when nothing was pending")
	}
}

// TestPendingReloadFiresAfterDelay - This test verifies that an
// uncancelled reload sends on FireChannel once its own delay elapses, and
// that Pending reports false again afterward.
func TestPendingReloadFiresAfterDelay(t *testing.T) {
	p := NewPendingReload()

	p.Schedule(20 * time.Millisecond)

	select {
	case <-p.FireChannel():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected FireChannel() to receive a value once the delay elapsed")
	}

	// The callback clears p.timer under its own lock before it ever sends
	// on fireCh, see Schedule's doc comment, so this is not racing the
	// callback itself, only ordinary scheduling.
	if p.Pending() {
		t.Error("expected Pending() == false once a reload has actually fired")
	}
}

// TestPendingReloadScheduleAgainSupersedesPrevious - This test verifies
// the identity comparison Schedule's doc comment describes: scheduling a
// second time before the first delay elapses replaces it outright, and the
// first, now-superseded timer's callback, even if it was already racing to
// fire, must never deliver a fire notification of its own.
func TestPendingReloadScheduleAgainSupersedesPrevious(t *testing.T) {
	p := NewPendingReload()

	p.Schedule(10 * time.Millisecond)
	p.Schedule(200 * time.Millisecond)

	// Give the first, superseded timer every chance to have fired on its
	// own, if it were ever going to.
	time.Sleep(80 * time.Millisecond)
	select {
	case <-p.FireChannel():
		t.Fatal("expected the superseded first Schedule to never deliver a fire notification")
	default:
	}
	if !p.Pending() {
		t.Error("expected the second Schedule to still be pending after the first delay alone would have elapsed")
	}

	p.Cancel()
}

// TestPendingReloadConcurrentScheduleAndCancel - This test verifies
// PendingReload's documented safety for concurrent use: Schedule and
// Cancel called repeatedly from more than one goroutine at once must never
// panic or deadlock, the same concurrent access main.go's runLoop and a
// firing timer's callback exercise. This test's real value is under `go
// test -race`.
func TestPendingReloadConcurrentScheduleAndCancel(t *testing.T) {
	p := NewPendingReload()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.Schedule(5 * time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			p.Cancel()
		}()
	}
	wg.Wait()
	p.Cancel()

	// Drain a fire notification if one snuck through before the final
	// Cancel above, so this test leaves nothing pending behind it.
	select {
	case <-p.FireChannel():
	case <-time.After(50 * time.Millisecond):
	}
}
