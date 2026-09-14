// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"sync"
)

// ErrStoreClosed is returned by Do once a Store has been closed, or is in
// the process of closing, rather than letting a caller submit a function
// against state that may no longer be owned by a live goroutine. A Store
// that has ever produced this error stays closed; Close is not reversible
// and a new Store must be constructed instead.
var ErrStoreClosed = errors.New("daemon: store is closed")

// storeRequest bundles one caller's request, a function to run against the
// current state, together with the one channel that call's result is
// delivered back on. Every field is set once, by Do, and read once, by
// run; nothing about storeRequest itself needs its own synchronization
// beyond that single handoff through Store's commands channel. Named
// storeRequest rather than the more obvious command specifically to avoid
// colliding with this project's package command, imported by state.go
// right alongside this file.
type storeRequest[S any] struct {
	fn     func(*S) (any, error)
	result chan<- doResult
}

// doResult is the value Do's function call to fn ultimately returns,
// carried back from the single writer goroutine to whichever caller's Do
// call is waiting for it.
type doResult struct {
	value any
	err   error
}

// Store - This type is the concurrency primitive: one long running
// goroutine owning a value of type S in ordinary unshared memory, with no
// mutex, because nothing outside that goroutine ever touches it.
//
// A zero Store is not ready to use; construct one with NewStore. Every
// method is safe to call from more than one goroutine.
type Store[S any] struct {
	commands  chan storeRequest[S]
	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once
}

// NewStore returns a ready to use Store, its single writer goroutine
// already running, starting from initial as the current state. The caller
// passes ownership of initial to the Store; nothing outside a function
// passed to Do should read or write the value initial itself referred to
// ever again, the same way nothing outside a goroutine holding a mutex
// should touch what that mutex protects without holding it.
func NewStore[S any](initial S) *Store[S] {
	st := &Store[S]{
		commands: make(chan storeRequest[S]),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go st.run(initial)
	return st
}

// run is the one goroutine that ever touches state directly, for the
// entire life of a Store. It never returns until stop is closed, and every
// request it ever receives off commands is applied to state, and answered,
// strictly one at a time, in the order run happened to receive them off
// that one channel.
func (s *Store[S]) run(state S) {
	defer close(s.stopped)
	for {
		select {
		case cmd := <-s.commands:
			value, err := cmd.fn(&state)
			// result is always buffered by exactly one, see Do, and used
			// by exactly one command value, exactly once, so this send can
			// never block.
			cmd.result <- doResult{value: value, err: err}
		case <-s.stop:
			return
		}
	}
}

// Do - This method submits fn to the single writer goroutine, blocks until
// it has run, and returns whatever fn returned.
//
// fn receives a pointer to the live state and may read and mutate it
// directly. No other Do call ever runs concurrently with fn, which is this
// type's whole reason to exist.
//
// fn MUST NOT retain that pointer past its own return, and should not
// block for long, since every other caller waits for it.
//
// Do returns ErrStoreClosed without running fn if the Store was already
// closed. A call already in flight when Close runs may still complete and
// mutate state, so the one at a time guarantee holds, but Do may report
// ErrStoreClosed to that caller anyway if Close finishes at the same
// moment. A caller that must know whether its mutation landed should not
// race Close this closely.
func (s *Store[S]) Do(fn func(*S) (any, error)) (any, error) {
	resultCh := make(chan doResult, 1)
	select {
	case s.commands <- storeRequest[S]{fn: fn, result: resultCh}:
	case <-s.stop:
		return nil, ErrStoreClosed
	}

	select {
	case r := <-resultCh:
		return r.value, r.err
	case <-s.stop:
		return nil, ErrStoreClosed
	}
}

// Close stops this Store's single writer goroutine and waits for it to
// exit before returning. Close is safe to call more than once, from more
// than one goroutine at once; only the first call does anything, every
// call, including the first, blocks until the goroutine has stopped. Every
// Do call still in flight when Close is called either completes normally
// or observes ErrStoreClosed, per Do's doc comment; no command is ever
// left half applied.
func (s *Store[S]) Close() {
	s.closeOnce.Do(func() { close(s.stop) })
	<-s.stopped
}
