// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestSessionDirectoryRegisterAppearsInList - This test verifies that a
// freshly registered session is reported by List, with the session ID
// Register itself returned, the username and Command Level it was
// registered with, and a non-zero ConnectedAt and LastActivity.
func TestSessionDirectoryRegisterAppearsInList(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	id, farewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id == "" {
		t.Fatal("Register returned an empty session ID")
	}
	if farewell == nil {
		t.Fatal("Register returned a nil farewell channel")
	}

	sessions, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("List returned %d sessions, want 1", len(sessions))
	}
	got := sessions[0]
	if got.ID != id || got.Username != "alice" || got.CommandLevel != "exec" {
		t.Errorf("List returned %+v, want ID %q, Username alice, CommandLevel exec", got, id)
	}
	if got.ConnectedAt.IsZero() || got.LastActivity.IsZero() {
		t.Errorf("List returned a zero ConnectedAt or LastActivity: %+v", got)
	}
}

// TestSessionDirectoryListSortsByConnectedAt - This test verifies that
// List reports sessions oldest first, matching its own doc comment, rather
// than a plain map's unordered iteration.
func TestSessionDirectoryListSortsByConnectedAt(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	firstID, _, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	secondID, _, err := d.Register("bob", "exec")
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}

	sessions, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("List returned %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != firstID || sessions[1].ID != secondID {
		t.Errorf("List order = [%s, %s], want [%s, %s]", sessions[0].ID, sessions[1].ID, firstID, secondID)
	}
}

// TestSessionDirectoryTouchUpdatesCommandLevelAndActivity - This test
// verifies that Touch updates both CommandLevel and LastActivity, and that
// LastActivity advances rather than staying pinned to whatever Register
// itself set.
func TestSessionDirectoryTouchUpdatesCommandLevelAndActivity(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	id, _, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	sessions, _ := d.List()
	before := sessions[0].LastActivity

	time.Sleep(2 * time.Millisecond)
	if err := d.Touch(id, "admin"); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	sessions, err = d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := sessions[0]
	if got.CommandLevel != "admin" {
		t.Errorf("CommandLevel = %q, want admin", got.CommandLevel)
	}
	if !got.LastActivity.After(before) {
		t.Errorf("LastActivity did not advance: before %v, after %v", before, got.LastActivity)
	}
}

// TestSessionDirectoryTouchUnknownSessionReturnsErrSessionNotFound - This
// test verifies that Touch against a session ID that was never registered,
// or was already unregistered, reports ErrSessionNotFound rather than
// silently doing nothing.
func TestSessionDirectoryTouchUnknownSessionReturnsErrSessionNotFound(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	if err := d.Touch("no-such-id", "exec"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Touch on an unknown session returned %v, want ErrSessionNotFound", err)
	}
}

// TestSessionDirectoryUnregisterRemovesFromList - This test verifies that
// Unregister removes a session from what List reports, and that a second
// Unregister for the same, already removed session ID reports
// ErrSessionNotFound.
func TestSessionDirectoryUnregisterRemovesFromList(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	id, _, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := d.Unregister(id); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	sessions, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("List returned %d sessions after Unregister, want 0", len(sessions))
	}

	if err := d.Unregister(id); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("second Unregister returned %v, want ErrSessionNotFound", err)
	}
}

// TestSessionDirectoryFarewellSingleMatchPushesText - This test verifies
// that Farewell, given a username matching exactly one attached session
// and no session ID, pushes text to that session's farewell channel and
// returns its ID.
func TestSessionDirectoryFarewellSingleMatchPushesText(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	id, farewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	matched, err := d.Farewell("alice", "", FarewellDisconnected)
	if err != nil {
		t.Fatalf("Farewell: %v", err)
	}
	if matched != id {
		t.Errorf("Farewell matched %q, want %q", matched, id)
	}

	select {
	case text := <-farewell:
		if text != FarewellDisconnected {
			t.Errorf("farewell text = %q, want %q", text, FarewellDisconnected)
		}
	default:
		t.Error("Farewell did not push anything to the matched session's channel")
	}
}

// TestSessionDirectoryFarewellAmbiguousUsernameRefuses - This test
// verifies that Farewell, given a username matching more than one attached
// session and no session ID, refuses with ErrAmbiguousSession and pushes
// nothing to either session, matching the disambiguation rule.
func TestSessionDirectoryFarewellAmbiguousUsernameRefuses(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	_, firstFarewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	_, secondFarewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}

	if _, err := d.Farewell("alice", "", FarewellDisconnected); !errors.Is(err, ErrAmbiguousSession) {
		t.Errorf("Farewell returned %v, want ErrAmbiguousSession", err)
	}

	select {
	case text := <-firstFarewell:
		t.Errorf("ambiguous Farewell pushed %q to the first session", text)
	default:
	}
	select {
	case text := <-secondFarewell:
		t.Errorf("ambiguous Farewell pushed %q to the second session", text)
	default:
	}
}

// TestSessionDirectoryFarewellWithSessionIDResolvesAmbiguity - This test
// verifies that naming a session ID explicitly resolves the ambiguous,
// same username, more than one session case cleanly, pushing to only the
// named session.
func TestSessionDirectoryFarewellWithSessionIDResolvesAmbiguity(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	firstID, firstFarewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	_, secondFarewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}

	matched, err := d.Farewell("alice", firstID, FarewellDisconnected)
	if err != nil {
		t.Fatalf("Farewell: %v", err)
	}
	if matched != firstID {
		t.Errorf("Farewell matched %q, want %q", matched, firstID)
	}

	select {
	case <-firstFarewell:
	default:
		t.Error("Farewell did not push to the explicitly named session")
	}
	select {
	case text := <-secondFarewell:
		t.Errorf("Farewell pushed %q to the session that was not named", text)
	default:
	}
}

// TestSessionDirectoryFarewellNoMatchReturnsErrNoMatchingSession - This
// test verifies that Farewell against a username with no attached session
// at all, and against a session ID that does not belong to the named
// username, both report ErrNoMatchingSession.
func TestSessionDirectoryFarewellNoMatchReturnsErrNoMatchingSession(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	if _, err := d.Farewell("nobody", "", FarewellDisconnected); !errors.Is(err, ErrNoMatchingSession) {
		t.Errorf("Farewell against an unknown username returned %v, want ErrNoMatchingSession", err)
	}

	aliceID, _, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := d.Farewell("alice", "wrong-session-id", FarewellDisconnected); !errors.Is(err, ErrNoMatchingSession) {
		t.Errorf("Farewell with a session ID belonging to nobody returned %v, want ErrNoMatchingSession", err)
	}
	_ = aliceID
}

// TestSessionDirectoryBroadcastFarewellReachesEveryAttachedSession - This
// test verifies that BroadcastFarewell pushes text to every currently
// attached session, including one standing in for the session that itself
// triggered a reboot, matching this method's doc comment that a requester
// is never special cased out.
func TestSessionDirectoryBroadcastFarewellReachesEveryAttachedSession(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	_, firstFarewell, err := d.Register("alice", "exec")
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	_, secondFarewell, err := d.Register("bob", "admin")
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}

	if err := d.BroadcastFarewell(FarewellRebooting); err != nil {
		t.Fatalf("BroadcastFarewell: %v", err)
	}

	for name, ch := range map[string]<-chan string{"first": firstFarewell, "second": secondFarewell} {
		select {
		case text := <-ch:
			if text != FarewellRebooting {
				t.Errorf("%s session received %q, want %q", name, text, FarewellRebooting)
			}
		default:
			t.Errorf("%s session received nothing from BroadcastFarewell", name)
		}
	}
}

// TestSessionDirectoryConcurrentAccess - This test verifies that many
// goroutines registering, touching, listing, and unregistering sessions at
// once, run under go test -race as a hard gate the same way this package's
// store_test.go already is, never corrupts SessionDirectory's state; every
// session this test registers is unregistered by the same goroutine that
// registered it, so the registry's size returns to zero once every
// goroutine finishes.
func TestSessionDirectoryConcurrentAccess(t *testing.T) {
	d := NewSessionDirectory()
	defer d.Close()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			id, _, err := d.Register("user", "exec")
			if err != nil {
				t.Errorf("Register: %v", err)
				return
			}
			if err := d.Touch(id, "admin"); err != nil {
				t.Errorf("Touch: %v", err)
			}
			if _, err := d.List(); err != nil {
				t.Errorf("List: %v", err)
			}
			if err := d.Unregister(id); err != nil {
				t.Errorf("Unregister: %v", err)
			}
		}()
	}
	wg.Wait()

	sessions, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("List returned %d sessions after every goroutine unregistered its own, want 0", len(sessions))
	}
}
