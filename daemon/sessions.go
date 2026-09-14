// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrNoMatchingSession is returned by SessionDirectory.Farewell when
// username, or username together with sessionID, matches no currently
// attached session.
var ErrNoMatchingSession = errors.New("daemon: no matching session")

// ErrAmbiguousSession is returned by SessionDirectory.Farewell when
// sessionID is empty and more than one currently attached session belongs
// to username, matching the disambiguation rule: "the daemon answers with
// an error listing the matching session IDs rather than guessing which one
// was meant." The matching session IDs themselves are not carried on this
// error value; a caller builds its own message from the SessionInfo values
// ListUsers or Farewell's candidates return alongside it.
var ErrAmbiguousSession = errors.New("daemon: more than one session for this user, specify a session ID")

// ErrSessionNotFound is returned by SessionDirectory.Touch and
// SessionDirectory.Unregister when id names no currently registered
// session, most commonly a session that has already ended, a second
// Unregister call for the same connection for instance.
var ErrSessionNotFound = errors.New("daemon: session not found")

// SessionInfo - This type is everything ListUsers reports about one
// attached session, and everything sent in a ListUsersResponsePayload: a
// daemon assigned session ID, the username that sent Hello, the Command
// Level that session last reported, when the connection was accepted, and
// when it was last seen doing anything.
//
// Idle time is not stored. It is how long ago LastActivity was, computed
// at response time rather than carried as a field that goes stale.
//
// This is the only session shaped type sent across the wire.
// trackedSession adds one private channel that has no business being
// serialized or handed outside this package.
type SessionInfo struct {
	ID           string
	Username     string
	CommandLevel string
	ConnectedAt  time.Time
	LastActivity time.Time
}

// trackedSession is this package's private bookkeeping for one attached
// session: everything SessionInfo carries, plus the channel Farewell and
// BroadcastFarewell use to push at whatever goroutine serves this
// connection.
//
// That channel is buffered by exactly one, so a push can never block this
// directory's single writer goroutine.
type trackedSession struct {
	SessionInfo
	farewell chan<- string
}

// sessionRegistry is the raw, in memory state one SessionDirectory owns:
// every currently attached session, keyed by its own daemon assigned
// session ID. A zero sessionRegistry, an unallocated map, is not valid
// state to construct a Store around directly; see NewSessionDirectory,
// which allocates one.
type sessionRegistry struct {
	sessions map[string]*trackedSession
}

// SessionDirectory is a real daemon's session and connection registry,
// reusing this package's generic Store[S any] directly as its concurrency
// safety, the identical pattern StandaloneClient already established for
// canonical state: one long running goroutine owning a sessionRegistry in
// ordinary unshared Go memory, every read and every mutation arriving as a
// function submitted through Do, applied strictly one at a time. A zero
// SessionDirectory is not ready to use; construct one with
// NewSessionDirectory.
type SessionDirectory struct {
	store *Store[sessionRegistry]
}

// NewSessionDirectory returns a ready to use SessionDirectory, its own
// private Store already running, starting from an empty registry, no
// sessions attached.
func NewSessionDirectory() *SessionDirectory {
	return &SessionDirectory{
		store: NewStore(sessionRegistry{sessions: make(map[string]*trackedSession)}),
	}
}

// newSessionID - This function returns a fresh, random session ID: eight
// bytes from crypto/rand, hex encoded, sixteen characters.
//
// The length is a balance. It is short enough to read off a screen and
// type back on a "disconnect user bob <session-id>" command line, and long
// enough that two sessions attaching at the same moment never collide by
// chance.
func newSessionID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("daemon: generating a session ID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Register - This method adds a session, starting at commandLevel with
// ConnectedAt and LastActivity set to now, and returns the session ID it
// was assigned.
//
// farewell is the channel whatever goroutine serves this connection reads
// to learn it has been targeted by a disconnect or a broadcast.
//
// It is wrapped here in a channel buffered by one, allocated internally
// rather than taken from the caller, so Farewell and BroadcastFarewell can
// always push without blocking this directory's writer goroutine,
// whatever the caller passed in.
func (d *SessionDirectory) Register(username, commandLevel string) (id string, farewell <-chan string, err error) {
	sessionID, err := newSessionID()
	if err != nil {
		return "", nil, err
	}

	ch := make(chan string, 1)
	now := time.Now()
	_, doErr := d.store.Do(func(r *sessionRegistry) (any, error) {
		r.sessions[sessionID] = &trackedSession{
			SessionInfo: SessionInfo{
				ID:           sessionID,
				Username:     username,
				CommandLevel: commandLevel,
				ConnectedAt:  now,
				LastActivity: now,
			},
			farewell: ch,
		}
		return nil, nil
	})
	if doErr != nil {
		return "", nil, doErr
	}
	return sessionID, ch, nil
}

// Touch updates a session's CommandLevel and LastActivity to now, called
// whenever that session sends an AuditEvent, its own dispatched command
// naming whichever Command Level it ran at; this is the only source this
// package uses for "idle time" rather than a separate heartbeat message,
// since a real daemon already hears from an active session at least once
// per dispatched command regardless. Touch returns ErrSessionNotFound if
// id names no currently registered session.
func (d *SessionDirectory) Touch(id, commandLevel string) error {
	_, err := d.store.Do(func(r *sessionRegistry) (any, error) {
		s, ok := r.sessions[id]
		if !ok {
			return nil, ErrSessionNotFound
		}
		s.CommandLevel = commandLevel
		s.LastActivity = time.Now()
		return nil, nil
	})
	return err
}

// Unregister - This method removes a session outright, called once its
// connection has closed, whether it sent a Goodbye first or simply
// disappeared. Either way this directory must not keep reporting a session
// nothing can reach.
//
// It returns ErrSessionNotFound when id names no registered session, most
// commonly a second call for the same connection. A caller that cannot
// tell whether it already called this should ignore that specific error.
func (d *SessionDirectory) Unregister(id string) error {
	_, err := d.store.Do(func(r *sessionRegistry) (any, error) {
		if _, ok := r.sessions[id]; !ok {
			return nil, ErrSessionNotFound
		}
		delete(r.sessions, id)
		return nil, nil
	})
	return err
}

// List returns one SessionInfo per currently attached session, sorted by
// ConnectedAt, oldest first, so "show users" reports a stable, predictable
// order from one call to the next rather than a plain Go map's unordered
// iteration. This is everything ListUsersResponsePayload carries back to
// whichever CLI session asked.
func (d *SessionDirectory) List() ([]SessionInfo, error) {
	value, err := d.store.Do(func(r *sessionRegistry) (any, error) {
		infos := make([]SessionInfo, 0, len(r.sessions))
		for _, s := range r.sessions {
			infos = append(infos, s.SessionInfo)
		}
		return infos, nil
	})
	if err != nil {
		return nil, err
	}
	infos := value.([]SessionInfo)
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ConnectedAt.Before(infos[j].ConnectedAt)
	})
	return infos, nil
}

// Farewell - This method resolves username, and optionally sessionID,
// against every attached session, then pushes text to the one that matches.
//
// The disambiguation rule is:
//
//   - A non-empty sessionID names one session, refused with
//     ErrNoMatchingSession if it does not belong to username.
//   - An empty sessionID with exactly one session for username resolves to
//     that one.
//   - An empty sessionID with more than one is refused with
//     ErrAmbiguousSession, candidates returned so the caller can list them.
//
// On a match it pushes to that session's farewell channel and returns its
// ID. The serve loop, seeing that push, sends Farewell down the wire and
// closes the connection.
//
// It does not remove the session. Unregister does that once the connection
// is observed closing, the same as any other session ending.
func (d *SessionDirectory) Farewell(username, sessionID, text string) (matchedID string, err error) {
	value, doErr := d.store.Do(func(r *sessionRegistry) (any, error) {
		var candidates []*trackedSession
		for _, s := range r.sessions {
			if s.Username != username {
				continue
			}
			if sessionID != "" && s.ID != sessionID {
				continue
			}
			candidates = append(candidates, s)
		}

		if sessionID != "" {
			if len(candidates) == 0 {
				return "", ErrNoMatchingSession
			}
			candidates[0].farewell <- text
			return candidates[0].ID, nil
		}

		switch len(candidates) {
		case 0:
			return "", ErrNoMatchingSession
		case 1:
			candidates[0].farewell <- text
			return candidates[0].ID, nil
		default:
			return "", ErrAmbiguousSession
		}
	})
	if doErr != nil {
		return "", doErr
	}
	return value.(string), nil
}

// BroadcastFarewell - This method pushes text to every attached session's
// farewell channel, including the session that triggered it. That produces
// "the requester is disconnected too" with no special casing.
//
// It is the one mechanism behind both a reboot, with text
// FarewellRebooting, and a clean SIGTERM drain, with text left empty.
func (d *SessionDirectory) BroadcastFarewell(text string) error {
	_, err := d.store.Do(func(r *sessionRegistry) (any, error) {
		for _, s := range r.sessions {
			s.farewell <- text
		}
		return nil, nil
	})
	return err
}

// Close stops this SessionDirectory's single writer goroutine. A caller
// should call this exactly once, when a real daemon process itself is
// shutting down, after BroadcastFarewell and after every attached
// connection has been closed.
func (d *SessionDirectory) Close() {
	d.store.Close()
}
