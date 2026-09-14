// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestStoreOrdinaryReadAndWrite - This test verifies the ordinary case,
// well before any concurrency is involved: Do can both read and mutate the
// state a Store owns, and a later Do call sees whatever an earlier one
// wrote.
func TestStoreOrdinaryReadAndWrite(t *testing.T) {
	s := NewStore(0)
	defer s.Close()

	if _, err := s.Do(func(n *int) (any, error) {
		*n = 42
		return nil, nil
	}); err != nil {
		t.Fatalf("Do (write): %v", err)
	}

	got, err := s.Do(func(n *int) (any, error) {
		return *n, nil
	})
	if err != nil {
		t.Fatalf("Do (read): %v", err)
	}
	if got.(int) != 42 {
		t.Errorf("Do (read) = %v, want 42", got)
	}
}

// TestStorePropagatesFunctionError - This test verifies that an error
// returned by a function passed to Do comes straight back out of Do
// itself, and that state is exactly as that function itself left it,
// nothing this package's machinery reverts on error.
func TestStorePropagatesFunctionError(t *testing.T) {
	s := NewStore(0)
	defer s.Close()

	wantErr := errors.New("deliberate test error")
	_, err := s.Do(func(n *int) (any, error) {
		*n = 7
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("Do error = %v, want %v", err, wantErr)
	}

	got, err := s.Do(func(n *int) (any, error) { return *n, nil })
	if err != nil {
		t.Fatalf("Do (read): %v", err)
	}
	if got.(int) != 7 {
		t.Errorf("state after an error-returning Do = %v, want 7, this package never reverts on error", got)
	}
}

// TestStoreConcurrentIncrementsNeverLoseAnUpdate - This is this package's
// direct test for the required behavior of the single writer goroutine: "a
// real burst of concurrent requests from many goroutines at
// once... asserting every request is answered exactly once and the final
// state is always exactly one of the attempted values, never a torn or
// partially applied mix." A concurrent, non-atomic read-increment-write on
// an ordinary int, run under go test -race, is exactly the pattern that
// panics under the race detector, or silently loses updates, the instant
// more than one goroutine is ever allowed to touch state directly; run
// entirely through Do instead, neither can happen, by construction.
func TestStoreConcurrentIncrementsNeverLoseAnUpdate(t *testing.T) {
	s := NewStore(0)
	defer s.Close()

	const goroutines = 200
	const incrementsEach = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*incrementsEach)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsEach; j++ {
				if _, err := s.Do(func(n *int) (any, error) {
					*n = *n + 1
					return nil, nil
				}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected Do error during concurrent increments: %v", err)
	}

	got, err := s.Do(func(n *int) (any, error) { return *n, nil })
	if err != nil {
		t.Fatalf("Do (final read): %v", err)
	}
	want := goroutines * incrementsEach
	if got.(int) != want {
		t.Errorf("final state = %v, want exactly %d; a lower value means a lost update, a race the single writer goroutine exists to make impossible", got, want)
	}
}

// TestStoreConcurrentWritesLandAsExactlyOneCompleteValue - This test
// verifies the other half of the same design doc sentence, "the final
// state is always exactly one of the attempted values, never a torn or
// partially applied mix": many goroutines race to overwrite a multi field
// struct, each writing a different, internally consistent candidate value,
// and afterward the state MUST be exactly one goroutine's complete
// candidate, matching field for field, never a mix of one goroutine's Name
// with another's Note, which is exactly the kind of corruption that
// becomes possible the moment more than one goroutine is ever allowed to
// mutate a shared struct without something serializing them.
func TestStoreConcurrentWritesLandAsExactlyOneCompleteValue(t *testing.T) {
	type record struct {
		Name string
		Note string
	}

	s := NewStore(record{})
	defer s.Close()

	const goroutines = 100
	candidates := make([]record, goroutines)
	for i := range candidates {
		candidates[i] = record{
			Name: fmt.Sprintf("writer-%d", i),
			Note: fmt.Sprintf("note-from-writer-%d", i),
		}
	}

	var wg sync.WaitGroup
	for _, candidate := range candidates {
		wg.Add(1)
		go func(c record) {
			defer wg.Done()
			if _, err := s.Do(func(r *record) (any, error) {
				*r = c
				return nil, nil
			}); err != nil {
				t.Errorf("Do: %v", err)
			}
		}(candidate)
	}
	wg.Wait()

	got, err := s.Do(func(r *record) (any, error) { return *r, nil })
	if err != nil {
		t.Fatalf("Do (final read): %v", err)
	}
	final := got.(record)

	matched := false
	for _, c := range candidates {
		if final == c {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("final state %+v does not exactly match any single candidate write; this is exactly the torn or partially applied mix this test exists to rule out", final)
	}
}

// TestStoreDoAfterCloseReturnsErrStoreClosed - This test verifies that Do
// refuses to run its function at all once a Store has been closed,
// reporting ErrStoreClosed instead, rather than either panicking on a
// goroutine that no longer exists or silently doing nothing.
func TestStoreDoAfterCloseReturnsErrStoreClosed(t *testing.T) {
	s := NewStore(0)
	s.Close()

	ran := false
	_, err := s.Do(func(n *int) (any, error) {
		ran = true
		return nil, nil
	})
	if !errors.Is(err, ErrStoreClosed) {
		t.Errorf("Do after Close error = %v, want ErrStoreClosed", err)
	}
	if ran {
		t.Error("expected Do's function to never run once the Store is closed")
	}
}

// TestStoreCloseIsSafeToCallConcurrentlyAndRepeatedly - This test verifies
// Close's documented safety: many goroutines calling Close on the same
// Store at once, some of them more than once, never panics or deadlocks,
// and every call still only returns once the single writer goroutine has
// stopped. This test's real value is under go test -race.
func TestStoreCloseIsSafeToCallConcurrentlyAndRepeatedly(t *testing.T) {
	s := NewStore(0)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Close()
			s.Close()
		}()
	}
	wg.Wait()

	if _, err := s.Do(func(n *int) (any, error) { return nil, nil }); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("Do after concurrent Close calls = %v, want ErrStoreClosed", err)
	}
}

// TestStoreConcurrentDoAndCloseNeverPanicsOrDeadlocks - This test verifies
// that Do calls racing directly against a concurrent Close, exactly the
// shutdown race Do's doc comment describes, never panics, never deadlocks,
// and every single Do call returns, one way or the other, within this
// test's deadline. This test's real value is under go test -race.
func TestStoreConcurrentDoAndCloseNeverPanicsOrDeadlocks(t *testing.T) {
	s := NewStore(0)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Do(func(n *int) (any, error) {
				*n = *n + 1
				return nil, nil
			})
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Close()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Do/Close race did not finish, likely deadlocked")
	}
}
