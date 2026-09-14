// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gotermcli/routercli/armorchan"
	"github.com/gotermcli/routercli/command"
)

// ConnectionLostText is the farewell text a RemoteClient reports on its own
// FarewellChannel when the connection ends with no explicit Farewell push,
// the daemon killed outright for instance.
//
// One text covers every such case: a crashed daemon, a socket level error,
// or a message this client could not decode. From a session's point of
// view all three are the same situation, and none earns its own wording.
const ConnectionLostText = "Connection to routercli daemon lost"

// rawMessage is one decoded, but not yet unmarshaled, frame this package's
// readLoop handed off to whichever call is waiting for it: a MessageKind
// and the JSON body that followed it.
type rawMessage struct {
	kind MessageKind
	body []byte
}

// RemoteClient - This type is a CLI process's live connection to a daemon,
// wrapping an already handshaken armorchan.Channel.
//
// One background goroutine, started by NewRemoteClient, is the only caller
// of Channel.Receive for the life of this client. Every exported method
// sends through Channel.Send, serialized by sendMu, and a request expecting
// an answer waits for that goroutine to hand the response back.
//
// A CLI dispatch loop runs at most one command at a time, so a response is
// correlated to whichever call is currently waiting rather than by tagging
// each message with a request ID.
//
// This satisfies command.Auditor, forwarding each call as an AuditEvent
// rather than writing a local file. It also satisfies the four non-Mutate
// methods of command.DaemonClient. ConnectedClient combines it with a
// StandaloneClient to satisfy the whole interface.
//
// A zero RemoteClient is not ready to use; construct one with
// NewRemoteClient.
type RemoteClient struct {
	ch   *armorchan.Channel
	conn net.Conn

	// sendMu serializes every call to ch.Send, both an ordinary call
	// waiting on a response and a one way send with none expected, audit
	// forwarding and Goodbye among them, so two goroutines can never
	// interleave two messages into one another on the wire; see Channel's
	// doc comment on why Send itself allows only one caller at a time per
	// direction.
	sendMu sync.Mutex

	// respCh receives exactly one rawMessage per call awaiting a response,
	// handed off by readLoop; buffered by one so readLoop never blocks
	// handing one off even if call itself has not reached its own select
	// yet.
	respCh chan rawMessage

	// farewell is the channel FarewellChannel returns, buffered by one,
	// the same non-blocking, single slot convention
	// PendingReload.FireChannel's doc comment already establishes for a
	// different signal.
	farewell chan string
	// done closes exactly once, the moment this connection is known to be
	// over, whether that is a genuine KindFarewell push or an ordinary
	// Receive error; see fail and readLoop.
	done     chan struct{}
	doneOnce sync.Once

	closeOnce sync.Once

	sessionID string

	// auditMu guards auditEnabled and level together, both small, audit
	// adjacent pieces of state a caller updates from outside this type's
	// background goroutine; see Enable, Disable, and SetLevel.
	auditMu      sync.Mutex
	auditEnabled bool
	level        string
}

// NewRemoteClient - This function takes an already handshaken Channel and
// its underlying connection, starts the background read loop, and performs
// the Hello exchange every connection begins with: username, this
// process's PID, and a terminal identifier.
//
// The terminal identifier is supplied by the caller rather than guessed
// here, which keeps this package free of any platform's idea of what one
// looks like.
//
// It blocks until the daemon answers with a session ID, or returns an
// error, closing conn itself first.
func NewRemoteClient(ch *armorchan.Channel, conn net.Conn, username, terminal string) (*RemoteClient, error) {
	c := &RemoteClient{
		ch:       ch,
		conn:     conn,
		respCh:   make(chan rawMessage, 1),
		farewell: make(chan string, 1),
		done:     make(chan struct{}),
	}
	go c.readLoop()

	kind, body, err := c.call(KindHello, HelloPayload{Username: username, PID: os.Getpid(), Terminal: terminal})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("daemon: sending Hello: %w", err)
	}
	if kind != KindHelloResponse {
		c.Close()
		return nil, fmt.Errorf("daemon: Hello: unexpected response kind %s", kind)
	}
	var resp HelloResponsePayload
	if err := json.Unmarshal(body, &resp); err != nil {
		c.Close()
		return nil, fmt.Errorf("daemon: decoding HelloResponse: %w", err)
	}
	c.sessionID = resp.SessionID
	return c, nil
}

// SessionID returns the session ID the daemon assigned this connection in
// its own HelloResponse, the same short identifier ListUsers reports for
// this session.
func (c *RemoteClient) SessionID() string {
	return c.sessionID
}

// readLoop is the one goroutine that ever calls c.ch.Receive, started by
// NewRemoteClient and running for the life of this RemoteClient. Every
// frame it decodes is either a KindFarewell push, handled entirely here,
// or handed off to whichever call is currently waiting on respCh; this
// connection is expected to carry at most one outstanding call at a time,
// see this type's doc comment, so a frame that is not a farewell push is
// always the answer to whatever call is currently blocked in its own
// select.
func (c *RemoteClient) readLoop() {
	for {
		raw, err := c.ch.Receive()
		if err != nil {
			c.fail(ConnectionLostText)
			return
		}

		kind, body, err := DecodeMessage(raw)
		if err != nil {
			c.fail(ConnectionLostText)
			return
		}

		if kind == KindFarewell {
			text := FarewellDisconnected
			var p FarewellPayload
			if jsonErr := json.Unmarshal(body, &p); jsonErr == nil && p.Text != "" {
				text = p.Text
			}
			c.fail(text)
			return
		}

		select {
		case c.respCh <- rawMessage{kind: kind, body: body}:
		case <-c.done:
			return
		}
	}
}

// fail marks this connection over, delivering text on farewell, exactly
// once no matter how many times or from which goroutine it is called; both
// readLoop's two exit paths, a genuine farewell push and an ordinary
// connection failure, converge on this one method, so FarewellChannel
// always receives exactly one value for the one way this connection can
// end, never zero and never more than one.
func (c *RemoteClient) fail(text string) {
	c.doneOnce.Do(func() {
		select {
		case c.farewell <- text:
		default:
			// farewell is buffered by exactly one; this can only be
			// reached if something already delivered a value that nothing
			// has read yet, which doneOnce itself already guarantees never
			// happens twice.
		}
		close(c.done)
	})
}

// call sends kind and payload, encoded through EncodeMessage, and blocks
// until either readLoop hands back the response it produced or this
// connection ends first, whichever happens first. See this type's doc
// comment for why this connection is never expected to carry more than one
// outstanding call at once, and sendMu for what enforces that in practice.
func (c *RemoteClient) call(kind MessageKind, payload any) (MessageKind, []byte, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	raw, err := EncodeMessage(kind, payload)
	if err != nil {
		return 0, nil, err
	}
	if err := c.ch.Send(raw); err != nil {
		c.fail(ConnectionLostText)
		return 0, nil, fmt.Errorf("daemon: sending a %s request: %w", kind, err)
	}

	select {
	case resp := <-c.respCh:
		return resp.kind, resp.body, nil
	case <-c.done:
		return 0, nil, errors.New("daemon: connection to the daemon ended before a response arrived")
	}
}

// send sends kind and payload the same way call does, for a message this
// package's catalog documents as a one way notification, no response ever
// expected, KindGoodbye and KindAuditEvent among them, so this returns as
// soon as the write itself succeeds rather than waiting on anything
// readLoop might hand back.
func (c *RemoteClient) send(kind MessageKind, payload any) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	raw, err := EncodeMessage(kind, payload)
	if err != nil {
		return err
	}
	if err := c.ch.Send(raw); err != nil {
		c.fail(ConnectionLostText)
		return fmt.Errorf("daemon: sending a %s message: %w", kind, err)
	}
	return nil
}

// ListUsers implements command.DaemonClient, sending a
// KindListUsersRequest and converting the daemon's
// ListUsersResponsePayload into command.SessionInfo, computing each
// session's IdleFor against time.Now at the moment this response is
// handled, rather than at whatever earlier moment the daemon itself built
// it, so a slow round trip never makes a session look less idle than it
// is.
func (c *RemoteClient) ListUsers() ([]command.SessionInfo, error) {
	kind, body, err := c.call(KindListUsersRequest, struct{}{})
	if err != nil {
		return nil, err
	}
	if kind != KindListUsersResponse {
		return nil, fmt.Errorf("daemon: ListUsers: unexpected response kind %s", kind)
	}
	var resp ListUsersResponsePayload
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("daemon: decoding ListUsersResponse: %w", err)
	}

	now := time.Now()
	out := make([]command.SessionInfo, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		out = append(out, command.SessionInfo{
			ID:           s.ID,
			Username:     s.Username,
			CommandLevel: s.CommandLevel,
			ConnectedAt:  s.ConnectedAt,
			IdleFor:      now.Sub(s.LastActivity),
		})
	}
	return out, nil
}

// DisconnectUser implements command.DaemonClient, sending a
// KindDisconnectUserRequest and turning a non-empty
// DisconnectUserResponsePayload.Error into a plain error value, the
// ambiguous session case, ErrAmbiguousSession on the daemon's side,
// included.
func (c *RemoteClient) DisconnectUser(username, sessionID string) error {
	kind, body, err := c.call(KindDisconnectUserRequest, DisconnectUserRequestPayload{Username: username, SessionID: sessionID})
	if err != nil {
		return err
	}
	if kind != KindDisconnectUserResponse {
		return fmt.Errorf("daemon: DisconnectUser: unexpected response kind %s", kind)
	}
	var resp DisconnectUserResponsePayload
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("daemon: decoding DisconnectUserResponse: %w", err)
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// Reboot implements command.DaemonClient, sending a KindRebootRequest and
// reporting only whether the daemon accepted it; this session's ending,
// along with every other attached session's, arrives separately, as a
// KindFarewell push readLoop turns into FarewellChannel firing, not as
// this call's return. See command.DaemonClient's doc comment on Reboot for
// why this takes no state of its own to hand across.
func (c *RemoteClient) Reboot() error {
	kind, body, err := c.call(KindRebootRequest, struct{}{})
	if err != nil {
		return err
	}
	if kind != KindRebootResponse {
		return fmt.Errorf("daemon: Reboot: unexpected response kind %s", kind)
	}
	var resp RebootResponsePayload
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("daemon: decoding RebootResponse: %w", err)
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// FarewellChannel implements command.DaemonClient; see fail for the one
// path, a genuine push from the daemon or an ordinary connection failure,
// that ever delivers a value here, always exactly once for the life of
// this RemoteClient.
func (c *RemoteClient) FarewellChannel() <-chan string {
	return c.farewell
}

// SetLevel - This method records the Command Level this session is at,
// included in every AuditEvent sent from that point on.
//
// command.Auditor's Log and ForceLog carry no level parameter, so whatever
// wires a RemoteClient into an AppContext calls this wherever the session's
// level changes. main.go does so once at startup and again in the read loop
// whenever a command changes it.
//
// A client that never has this called reports an empty level, which is
// better than a wrong one.
func (c *RemoteClient) SetLevel(level string) {
	c.auditMu.Lock()
	defer c.auditMu.Unlock()
	c.level = level
}

// Enable - This method turns audit logging on for this session.
//
// Enable, Disable, and Enabled give a RemoteClient the same three method
// shape auditlog.AuditLog has, so the "audit-log" commands work against
// either type through one small interface rather than a type assertion.
//
// Unlike auditlog.AuditLog, this cannot fail, since there is no file to
// open. It still returns an error so one interface covers both types
// without an adapter.
func (c *RemoteClient) Enable() error {
	c.auditMu.Lock()
	defer c.auditMu.Unlock()
	c.auditEnabled = true
	return nil
}

// Disable - This method turns audit logging off for this session. See
// Enable's doc comment for why these three methods exist.
func (c *RemoteClient) Disable() {
	c.auditMu.Lock()
	defer c.auditMu.Unlock()
	c.auditEnabled = false
}

// Enabled - This method reports whether audit logging is currently on for
// this session. See Enable's doc comment for why these three methods
// exist.
func (c *RemoteClient) Enabled() bool {
	c.auditMu.Lock()
	defer c.auditMu.Unlock()
	return c.auditEnabled
}

// WouldLog implements command.Auditor, reporting whether Log would send
// anything; unlike auditlog.AuditLog, there is no separate "file open"
// condition to check here, so this is Enabled.
func (c *RemoteClient) WouldLog() bool {
	return c.Enabled()
}

// Log - This method implements command.Auditor, forwarding username,
// command, and success as an AuditEvent, with this client's current level
// and the current time.
//
// The time is carried as a time.Time rather than pre-formatted text,
// because the daemon, not this client, is what calls FormatEntry.
//
// It does nothing when WouldLog is false. A send failure is not reported:
// command.Auditor.Log has no error return, and an audit failure must never
// crash the CLI.
func (c *RemoteClient) Log(username, cmdText string, success bool) {
	if !c.WouldLog() {
		return
	}
	c.sendAuditEvent(username, cmdText, success)
}

// ForceLog implements command.Auditor, sending unconditionally, skipping
// the WouldLog check Log performs, matching auditlog.AuditLog.ForceLog's
// doc comment and its one real use, a command whose own side effect flips
// audit logging off, "audit-log disable" itself, needing its own entry
// sent despite that.
func (c *RemoteClient) ForceLog(username, cmdText string, success bool) {
	c.sendAuditEvent(username, cmdText, success)
}

func (c *RemoteClient) sendAuditEvent(username, cmdText string, success bool) {
	c.auditMu.Lock()
	level := c.level
	c.auditMu.Unlock()

	// A transient send failure here must never surface to Log or
	// ForceLog's caller, neither of which has an error return to give one
	// through; see this method's two callers' doc comments. A genuine
	// connection failure is already reported through FarewellChannel by
	// send itself, via fail, which is the channel this project's runLoop
	// watches for exactly this kind of fatal, unrecoverable condition.
	_ = c.send(KindAuditEvent, AuditEventPayload{
		Username: username,
		Command:  cmdText,
		Level:    level,
		Time:     time.Now(),
		Success:  success,
	})
}

// Close - This method sends a best effort Goodbye, then closes the
// connection.
//
// A failure sending Goodbye does not stop the close. Closing conn is what
// matters: it makes the blocked Receive return, which drives the same
// cleanup an unexpected failure already triggers, so no separate
// bookkeeping is needed.
//
// Safe to call more than once; only the first call does anything.
//
// Goodbye is skipped entirely when done is already closed. Goodbye means a
// session ending normally, which is no longer true once the connection has
// ended some other way, and sending anyway would write into a connection
// nothing may still be reading.
func (c *RemoteClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		select {
		case <-c.done:
		default:
			_ = c.send(KindGoodbye, struct{}{})
		}
		err = c.conn.Close()
	})
	return err
}

var (
	_ command.Auditor = (*RemoteClient)(nil)
)
