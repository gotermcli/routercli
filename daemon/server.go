// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/armorchan"
	"github.com/gotermcli/routercli/auditlog"
)

// Server is a real RouterCLI daemon's connection acceptor and per
// connection message dispatcher, the piece of code that implements this
// project's message framing design against real, accepted connections,
// reusing every other piece this package already built: a Listener, this
// daemon's canonical *Store[State], a SessionDirectory, and an
// *auditlog.AuditLog to append AuditEvent messages to. A zero Server is
// not ready to use; construct one with NewServer.
type Server struct {
	listener      *Listener
	staticPrivate *ecdh.PrivateKey
	store         *Store[State]
	sessions      *SessionDirectory
	audit         *auditlog.AuditLog
	logger        *log.Logger

	// slots bounds how many connections are served concurrently. A send
	// into it takes a slot, and the serving goroutine returns the slot when
	// it finishes. See Serve for why a connection beyond the ceiling is
	// refused rather than queued.
	slots chan struct{}

	// reload rebuilds a fresh State from whatever StartupConfigFile,
	// UsersFile, and RolesFile currently hold on disk.
	//
	// It is supplied by the caller rather than implemented here, because
	// building a fresh product state needs command.LoadStartupConfig and a
	// concrete product package this generic package stays free of. That is
	// the same reason ProductState is an opaque any.
	reload func() (State, error)

	// privilegedUIDs is the second, narrower peer credential check
	// KindRebootRequest and KindDisconnectUserRequest MUST pass, on top of
	// whatever Listener.Accept already required of every connection. A nil
	// privilegedUIDs authorizes every already accepted connection to issue
	// either, matching this Server's behavior before this field existed.
	//
	// The AllowedUIDs a Listener was built with decides who may open a
	// session on this socket at all. This decides which of those sessions
	// may also reboot the daemon or force another session closed.
	// AllowedRoles gates the same two actions in a CLI client's own command
	// tree, but that gate runs only inside a CLI process choosing to honor
	// it before it ever dials the socket; nothing here stops a peer that
	// speaks this wire protocol directly from skipping straight past it,
	// since HelloPayload carries no role or credential of its own for this
	// package to check. privilegedUIDs is this Server's own, independent
	// answer to who may issue these two requests, checked against the
	// kernel verified peer credential rather than anything a requester
	// claims about itself.
	privilegedUIDs PeerCredentialChecker
}

// MaxConcurrentConnections bounds how many connections a Server will serve at
// once.
//
// Every allowed peer can open connections freely, so without a ceiling one of
// them can exhaust the process's file descriptors, and a daemon out of
// descriptors cannot accept the session an operator needs during an incident.
//
// A real deployment has a handful of attached sessions. This is set far above
// that so an ordinary operator never meets it, and far below any limit a
// default descriptor budget imposes.
const MaxConcurrentConnections = 256

// auditTimeTolerance bounds how far an audit event's client supplied timestamp
// may sit from the daemon's own clock before the daemon substitutes its own.
//
// The dispatch time is worth carrying, since it is when the operator acted
// rather than when the daemon got around to writing it, and on a loaded system
// those differ. Two minutes is far more than that gap ever is, and small enough
// that nothing can be hidden outside the window an investigation would look at.
const auditTimeTolerance = 2 * time.Minute

// NewServer - This function returns a ready Server.
//
// listener and staticPrivate are what AcceptAndHandshake needs for each
// incoming connection. store is the canonical state, sessions the session
// registry, and audit the daemon's audit log, already enabled if
// configured. reload is called by TriggerReboot to rebuild state from
// disk. logger receives connection lifecycle and reboot tracing.
//
// privilegedUIDs gates KindRebootRequest and KindDisconnectUserRequest; see
// Server's privilegedUIDs field for what it checks and why. nil is the
// permissive default, every already accepted connection may issue either,
// which is correct for a deployment where AllowedUIDs itself names only
// trusted accounts and there is no finer distinction to draw among them.
func NewServer(listener *Listener, staticPrivate *ecdh.PrivateKey, store *Store[State], sessions *SessionDirectory, audit *auditlog.AuditLog, reload func() (State, error), logger *log.Logger, privilegedUIDs PeerCredentialChecker) *Server {
	return &Server{
		slots:          make(chan struct{}, MaxConcurrentConnections),
		listener:       listener,
		staticPrivate:  staticPrivate,
		store:          store,
		sessions:       sessions,
		audit:          audit,
		reload:         reload,
		logger:         logger,
		privilegedUIDs: privilegedUIDs,
	}
}

// authorizedForPrivilegedRequest reports whether conn's kernel verified peer
// credential passes s.privilegedUIDs, the gate KindRebootRequest and
// KindDisconnectUserRequest both require before this Server acts on them.
//
// A nil privilegedUIDs authorizes every connection, matching this Server's
// behavior before that field existed. A credential this cannot read, see
// peerCredentialOf, is refused rather than guessed at: a session an
// administrator cannot positively identify is not one this method will call
// privileged.
func (s *Server) authorizedForPrivilegedRequest(conn net.Conn) bool {
	if s.privilegedUIDs == nil {
		return true
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	return s.privilegedUIDs.CheckPeer(uc) == nil
}

// Serve - This method accepts connections until the Listener stops
// accepting, usually because Shutdown or Close was called, spawning one
// goroutine per accepted connection.
//
// A connection that passes the peer credential check but fails its
// armorchan handshake is not served, and Serve keeps accepting. One bad
// connection attempt must never take the socket down.
func (s *Server) Serve() {
	for {
		ch, conn, err := AcceptAndHandshake(s.listener, s.staticPrivate)
		if err != nil {
			// A closed listener is the only error that ends this loop.
			// Anything else belongs to the one connection that produced
			// it, and MUST NOT take the socket down: a peer that sends
			// four bytes of garbage would otherwise stop the daemon
			// accepting anything ever again.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if s.logger != nil {
				s.logger.Debugln("DEBUG: refusing connection:", err)
			}
			continue
		}
		// Bound how many connections are served at once. An allowed UID
		// can otherwise open them until the process runs out of file
		// descriptors, and a daemon that has run out cannot accept the
		// session an operator needs during the incident.
		//
		// A connection beyond the ceiling is closed immediately rather
		// than queued. Refusing quickly, with a log line, beats a
		// backlog that makes the daemon look alive while nothing it
		// accepts ever gets served.
		select {
		case s.slots <- struct{}{}:
		default:
			if s.logger != nil {
				s.logger.Debugln("DEBUG: refusing connection: at the concurrent connection ceiling")
			}
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-s.slots }()
			s.serveConnection(ch, conn)
		}()
	}
}

// Shutdown - This method broadcasts a farewell with no text to every
// attached session, a clean drain rather than a reboot, then closes the
// Listener so Serve stops accepting.
//
// A receiving client reports an empty farewell as "Line closed by remote
// host", the same wording a targeted disconnect uses. Nothing more
// specific applies to an orderly shutdown.
//
// It does not wait for connections to finish closing. A caller wanting
// that waits on Serve returning.
func (s *Server) Shutdown() error {
	_ = s.sessions.BroadcastFarewell("")
	return s.listener.Close()
}

// TriggerReboot rebuilds a fresh State through this Server's reload
// function, replaces this daemon's canonical state with it wholesale, then
// sends every currently attached session a "Device is rebooting" farewell,
// matching what a reboot means with a daemon running. This is the one
// function both a privileged session's KindRebootRequest, through
// serveConnection, and this daemon's SIGHUP handler converge on, exactly
// as that section describes; SIGHUP calls this directly, with no requester
// of its own to answer first.
func (s *Server) TriggerReboot() error {
	newState, err := s.reload()
	if err != nil {
		return err
	}
	if err := s.replaceState(newState); err != nil {
		return err
	}
	return s.sessions.BroadcastFarewell(FarewellRebooting)
}

// replaceState swaps this daemon's canonical state for newState wholesale,
// not merged field by field, the same "one function producing one
// canonical text" discipline State.Config's doc comment already describes
// for this exact operation.
func (s *Server) replaceState(newState State) error {
	_, err := s.store.Do(func(st *State) (any, error) {
		*st = newState
		return nil, nil
	})
	return err
}

// sendMessage encodes kind and payload through EncodeMessage and writes it
// to ch, the small helper every response this file sends funnels through.
func (s *Server) sendMessage(ch *armorchan.Channel, kind MessageKind, payload any) error {
	raw, err := EncodeMessage(kind, payload)
	if err != nil {
		return err
	}
	return ch.Send(raw)
}

// decodeHello - This function decodes the single Hello message every
// connection must send first, right after the handshake completes. It
// reports false, and the caller drops the connection without replying, for
// anything that is not a well formed Hello: a frame that does not decode,
// a message of some other kind arriving first, or a body that is not valid
// Hello JSON. None of those is worth distinguishing to a peer that has not
// yet proved it speaks this protocol at all.
func decodeHello(raw []byte) (HelloPayload, bool) {
	kind, body, err := DecodeMessage(raw)
	if err != nil || kind != KindHello {
		return HelloPayload{}, false
	}

	var hello HelloPayload
	if err := json.Unmarshal(body, &hello); err != nil {
		return HelloPayload{}, false
	}
	return hello, true
}

// serveConnection - This method is the per connection goroutine body: read
// the session's Hello, register it, answer with its session ID, then
// dispatch every message until the connection ends.
//
// It ends on an ordinary Goodbye, a targeted Farewell, or the connection
// failing.
//
// A message that fails to decode ends this one connection, matching
// armorchan's rule that a faulted channel is fatal. It never brings down
// the Server or any other connection.
func (s *Server) serveConnection(ch *armorchan.Channel, conn net.Conn) {
	defer conn.Close()

	raw, err := ch.Receive()
	if err != nil {
		return
	}
	hello, ok := decodeHello(raw)
	if !ok {
		return
	}

	// Captured once, here, from the Hello this connection opened with.
	// Every audit entry from this connection is attributed to it, so a
	// later message cannot rename the actor.
	sessionUsername := hello.Username

	// Read once from the kernel, for the audit log's peer field. The
	// username above is whatever the client said it was; this is not.
	peer := PeerIdentity(conn)

	sessionID, farewellCh, err := s.sessions.Register(sessionUsername, "")
	if err != nil {
		if s.logger != nil {
			s.logger.Errorf("daemon: registering session for %q: %v", hello.Username, err)
		}
		return
	}
	defer func() { _ = s.sessions.Unregister(sessionID) }()

	if err := s.sendMessage(ch, KindHelloResponse, HelloResponsePayload{SessionID: sessionID}); err != nil {
		return
	}
	if s.logger != nil {
		s.logger.Debugln("DEBUG: daemon accepted session", sessionID, "for user", hello.Username)
	}

	// This connection's farewell watcher: the one goroutine that ever
	// reads farewellCh, pushed to by SessionDirectory.Farewell or
	// BroadcastFarewell elsewhere, possibly from a different connection's
	// goroutine entirely, a targeted "disconnect user" or a reboot.
	// Closing conn here is what makes this connection's blocked Receive
	// call below return an error, ending this function the same way any
	// other connection failure already does, so no separate bookkeeping is
	// needed for that path. doneCh stops this goroutine cleanly on every
	// other exit path, an ordinary KindGoodbye among them, so it never
	// leaks past this function returning.
	doneCh := make(chan struct{})
	defer close(doneCh)
	go func() {
		select {
		case text := <-farewellCh:
			_ = s.sendMessage(ch, KindFarewell, FarewellPayload{Text: text})
			conn.Close()
		case <-doneCh:
		}
	}()

	for {
		raw, err := ch.Receive()
		if err != nil {
			return
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			return
		}

		switch kind {
		case KindGoodbye:
			return

		case KindAuditEvent:
			var payload AuditEventPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				return
			}
			// The username comes from the Hello this connection opened
			// with, never from the event. A client that supplied it per
			// event could attribute anything to anyone, which would make
			// the audit log worthless as evidence: the one record of who
			// did what would be written by whoever is being recorded.
			//
			// The timestamp is clamped rather than trusted. Carrying the
			// dispatch time is deliberate, see AuditEventPayload, but an
			// unbounded client clock lets an attacker backdate an entry
			// out of whatever window an investigation is looking at.
			// Anything outside the tolerance is replaced with the
			// daemon's own clock.
			when := payload.Time
			if skew := time.Since(when); skew > auditTimeTolerance || skew < -auditTimeTolerance {
				when = time.Now()
			}
			s.audit.LogAt(when, sessionUsername, peer, payload.Command, payload.Success)
			_ = s.sessions.Touch(sessionID, payload.Level)

		case KindListUsersRequest:
			infos, lerr := s.sessions.List()
			if lerr != nil {
				return
			}
			if err := s.sendMessage(ch, KindListUsersResponse, ListUsersResponsePayload{Sessions: infos}); err != nil {
				return
			}

		case KindDisconnectUserRequest:
			var req DisconnectUserRequestPayload
			if err := json.Unmarshal(body, &req); err != nil {
				return
			}
			resp := DisconnectUserResponsePayload{}
			if !s.authorizedForPrivilegedRequest(conn) {
				resp.Error = "daemon: not authorized to disconnect a session"
				s.audit.LogAt(time.Now(), sessionUsername, peer, "disconnect.user", false)
			} else if _, ferr := s.sessions.Farewell(req.Username, req.SessionID, FarewellDisconnected); ferr != nil {
				resp.Error = s.disconnectErrorText(req.Username, ferr)
			}
			if err := s.sendMessage(ch, KindDisconnectUserResponse, resp); err != nil {
				return
			}

		case KindRebootRequest:
			if !s.authorizedForPrivilegedRequest(conn) {
				s.audit.LogAt(time.Now(), sessionUsername, peer, "reboot", false)
				if err := s.sendMessage(ch, KindRebootResponse, RebootResponsePayload{Error: "daemon: not authorized to reboot"}); err != nil {
					return
				}
				continue
			}
			newState, rerr := s.reload()
			if rerr == nil {
				rerr = s.replaceState(newState)
			}
			if rerr != nil {
				_ = s.sendMessage(ch, KindRebootResponse, RebootResponsePayload{Error: rerr.Error()})
				continue
			}
			if err := s.sendMessage(ch, KindRebootResponse, RebootResponsePayload{}); err != nil {
				return
			}
			if s.logger != nil {
				s.logger.Debugln("DEBUG: daemon reboot triggered by session", sessionID, "for user", hello.Username)
			}
			_ = s.sessions.BroadcastFarewell(FarewellRebooting)

		default:
			// An unrecognized or out of place message kind, KindHello a
			// second time on an already established connection among them,
			// is treated the same as any other malformed input: fatal to
			// this one connection, never to this Server.
			return
		}
	}
}

// disconnectErrorText turns ferr, whatever SessionDirectory.Farewell
// returned, into the text a requesting session's "disconnect user" handler
// prints. ErrAmbiguousSession specifically is expanded into a message
// naming the actual candidate session IDs, resolved fresh through
// s.sessions.List, matching the disambiguation rule: "the daemon answers
// with an error listing the matching session IDs rather than guessing
// which one was meant." Every other error, most commonly
// ErrNoMatchingSession, is reported as is.
func (s *Server) disconnectErrorText(username string, ferr error) string {
	if !errors.Is(ferr, ErrAmbiguousSession) {
		return ferr.Error()
	}
	infos, err := s.sessions.List()
	if err != nil {
		return ferr.Error()
	}
	var ids []string
	for _, info := range infos {
		if info.Username == username {
			ids = append(ids, info.ID)
		}
	}
	return fmt.Sprintf("more than one session for %q, specify a session ID: %s", username, strings.Join(ids, ", "))
}
