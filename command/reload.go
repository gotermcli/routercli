// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"sync"
	"time"
)

// PendingReload - This type tracks at most one scheduled, delayed reboot
// at a time. "reboot" and its "reload" alias share this one pending state.
//
// The delay is an ordinary background timer inside this process, not a
// request sent anywhere. Even with a daemon configured, it is the CLI
// process that counts down and then acts.
//
// A zero PendingReload is not ready to use; construct one with
// NewPendingReload.
//
// Every method is safe from more than one goroutine, which matters here:
// "reboot <seconds>" and "no reboot" both run on the dispatch goroutine,
// while a scheduled timer fires on one of its own.
type PendingReload struct {
	mu     sync.Mutex
	timer  *time.Timer
	fireCh chan struct{}
}

// NewPendingReload returns a ready to use PendingReload, with nothing
// currently scheduled. main.go constructs exactly one of these, at
// startup, and stores it on AppContext.ReloadScheduler.
func NewPendingReload() *PendingReload {
	return &PendingReload{
		// Buffered by one, never more, so Schedule's callback can always
		// deliver a fire notification without blocking on whether anything
		// is currently reading FireChannel(), and so that a fire
		// notification, once sent, can never accumulate more than the one
		// real reload it is reporting.
		fireCh: make(chan struct{}, 1),
	}
}

// FireChannel returns the channel that receives exactly one value once a
// scheduled reload fires, uncancelled. main.go's runLoop selects on this
// alongside reading the next typed line, so a scheduled reload can end the
// session even while the loop is otherwise blocked waiting on interactive
// input from a real terminal. The same channel is returned every time; it
// is never replaced, so a caller may safely select on it once and keep
// using that same value for the life of the process.
func (p *PendingReload) FireChannel() <-chan struct{} {
	return p.fireCh
}

// Schedule - This method arms a pending reboot, delay from now, replacing
// whatever was scheduled before. Only the most recent one is ever pending,
// matching Cisco, where typing it again with a new delay reschedules.
//
// When the delay elapses without a Cancel or a later Schedule superseding
// it, exactly one value is sent, non-blocking, on FireChannel.
//
// The goroutine checks whether this specific timer is still the pending
// one. That closes a real race: Go is explicit that time.Timer.Stop
// returning false does not mean the callback has finished, or even
// started, only that it cannot be prevented.
//
// Comparing identity under the same mutex Cancel takes means a callback
// that loses that race is dropped, rather than firing a reboot a session
// already believed it had cancelled.
func (p *PendingReload) Schedule(delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
	}

	var t *time.Timer
	t = time.AfterFunc(delay, func() {
		p.mu.Lock()
		if p.timer != t {
			// Superseded by Cancel or a later Schedule call before this
			// callback got to run; see this method's doc comment for the
			// Stop/fire race this guards against.
			p.mu.Unlock()
			return
		}
		p.timer = nil
		p.mu.Unlock()

		select {
		case p.fireCh <- struct{}{}:
		default:
			// FireChannel is buffered by exactly one and nothing has
			// drained a previous, already superseded notification yet.
			// That can never represent more than the one real reload
			// pending.
		}
	})
	p.timer = t
}

// Cancel stops whatever reload is currently pending, if any, and reports
// whether there was one to stop. "no reload" and "no reboot" both call
// this; a false result means neither had anything scheduled to begin with,
// which example/cmd/core/cmd_admin.go reports back to the session as an error
// rather than a silent no-op, the same "fail loudly on a malformed
// request" convention this project already applies elsewhere.
func (p *PendingReload) Cancel() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer == nil {
		return false
	}
	p.timer.Stop()
	p.timer = nil
	return true
}

// Pending reports whether a reload is currently scheduled.
func (p *PendingReload) Pending() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.timer != nil
}
