// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gotermcli/routercli/armorchan"
	"github.com/gotermcli/routercli/command"
)

// newTestChannelPair returns two already handshaken *armorchan.Channel
// values, and the two net.Conn values underneath them, talking to each
// other over a real net.Pipe, the same "a real handshake completing over a
// real connection" standard socket_test.go's end to end test already sets,
// just without a real Unix domain socket in between, which this package's
// daemon side responder in each test below stands in for directly. The
// caller is responsible for closing both conns; RemoteClient.Close, in
// every test that constructs one, already closes clientConn on the
// client's side.
func newTestChannelPair(t *testing.T) (client, daemonSide *armorchan.Channel, clientConn, daemonConn net.Conn) {
	t.Helper()

	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	clientConn, daemonConn = net.Pipe()

	// net.Pipe is fully synchronous, with no buffering at all, so both
	// sides of a multi message handshake must be actively reading and
	// writing at the same time or the whole exchange deadlocks; both calls
	// run in their own goroutine here, exactly the pattern armorchan's
	// handshakeOverPipe in armorchan_test.go already establishes, rather
	// than one of the two running in this test's goroutine while only the
	// other is backgrounded.
	type result struct {
		ch  *armorchan.Channel
		err error
	}
	serverResultCh := make(chan result, 1)
	clientResultCh := make(chan result, 1)
	go func() {
		ch, err := armorchan.ServerHandshake(daemonConn, priv)
		serverResultCh <- result{ch, err}
	}()
	go func() {
		ch, err := armorchan.ClientHandshake(clientConn, priv.PublicKey())
		clientResultCh <- result{ch, err}
	}()

	var sr, cr result
	select {
	case sr = <-serverResultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ServerHandshake never returned")
	}
	select {
	case cr = <-clientResultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ClientHandshake never returned")
	}
	if sr.err != nil {
		t.Fatalf("ServerHandshake: %v", sr.err)
	}
	if cr.err != nil {
		t.Fatalf("ClientHandshake: %v", cr.err)
	}

	return cr.ch, sr.ch, clientConn, daemonConn
}

// respondToHello reads one KindHello frame off daemonSide and answers it
// with a KindHelloResponse carrying sessionID, the minimal fake daemon
// behavior every test below needs before it can do anything else, since
// NewRemoteClient itself blocks on exactly this exchange.
func respondToHello(t *testing.T, daemonSide *armorchan.Channel, sessionID string) {
	t.Helper()

	raw, err := daemonSide.Receive()
	if err != nil {
		t.Fatalf("daemon side Receive (Hello): %v", err)
	}
	kind, _, err := DecodeMessage(raw)
	if err != nil {
		t.Fatalf("daemon side DecodeMessage (Hello): %v", err)
	}
	if kind != KindHello {
		t.Fatalf("daemon side received kind %s, want %s", kind, KindHello)
	}

	resp, err := EncodeMessage(KindHelloResponse, HelloResponsePayload{SessionID: sessionID})
	if err != nil {
		t.Fatalf("EncodeMessage(HelloResponse): %v", err)
	}
	if err := daemonSide.Send(resp); err != nil {
		t.Fatalf("daemon side Send (HelloResponse): %v", err)
	}
}

// newConnectedTestRemoteClient dials a RemoteClient against a fake daemon
// that has already answered Hello with sessionID, returning the client and
// the daemon side Channel and conns so the test can keep driving the
// daemon side of the exchange. The client side conn is closed by
// RemoteClient.Close; the caller closes daemonConn.
func newConnectedTestRemoteClient(t *testing.T, sessionID string) (client *RemoteClient, daemonSide *armorchan.Channel, daemonConn net.Conn) {
	t.Helper()

	clientCh, daemonCh, clientConn, daemonConn := newTestChannelPair(t)

	helloDone := make(chan struct{})
	go func() {
		defer close(helloDone)
		respondToHello(t, daemonCh, sessionID)
	}()

	c, err := NewRemoteClient(clientCh, clientConn, "alice", "pts/0")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	<-helloDone

	return c, daemonCh, daemonConn
}

// closeClientDrainingDaemon closes c after starting a background drain of
// daemonSide's Receive loop, so a Close call that still needs to send a
// best effort Goodbye, this connection not yet known to have ended any
// other way, always has a reader on the daemon side to land on. net.Pipe,
// used throughout this file, is fully synchronous with no buffering at
// all, unlike a real socket's kernel buffer, so a Send with nothing
// reading it blocks forever rather than merely delaying, a condition a
// real deployment would never hit in practice, only this test harness's
// more demanding transport, once whatever fake daemon goroutine a given
// test spawned has already returned after handling the one exchange that
// test cared about.
func closeClientDrainingDaemon(t *testing.T, c *RemoteClient, daemonSide *armorchan.Channel) {
	t.Helper()
	go func() {
		for {
			if _, err := daemonSide.Receive(); err != nil {
				return
			}
		}
	}()
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestNewRemoteClientPerformsHelloAndRecordsSessionID - This test verifies
// that NewRemoteClient sends a KindHello carrying the username and
// terminal it was given, and that the session ID the fake daemon answered
// with in KindHelloResponse comes back from SessionID.
func TestNewRemoteClientPerformsHelloAndRecordsSessionID(t *testing.T) {
	clientCh, daemonCh, clientConn, daemonConn := newTestChannelPair(t)
	defer daemonConn.Close()

	helloRaw := make(chan []byte, 1)
	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		helloRaw <- raw
		resp, err := EncodeMessage(KindHelloResponse, HelloResponsePayload{SessionID: "session-1"})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	c, err := NewRemoteClient(clientCh, clientConn, "alice", "pts/0")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	defer closeClientDrainingDaemon(t, c, daemonCh)

	if c.SessionID() != "session-1" {
		t.Errorf("SessionID() = %q, want session-1", c.SessionID())
	}

	raw := <-helloRaw
	kind, body, err := DecodeMessage(raw)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if kind != KindHello {
		t.Fatalf("kind = %s, want %s", kind, KindHello)
	}
	hello, err := unmarshalPayload[HelloPayload](body)
	if err != nil {
		t.Fatalf("unmarshal HelloPayload: %v", err)
	}
	if hello.Username != "alice" || hello.Terminal != "pts/0" {
		t.Errorf("Hello = %+v, want Username alice, Terminal pts/0", hello)
	}
	if hello.PID == 0 {
		t.Error("Hello.PID = 0, want this test process's real PID")
	}
}

// TestRemoteClientListUsersConvertsSessionsAndIdleFor - This test verifies
// that ListUsers decodes the daemon's ListUsersResponsePayload into
// command.SessionInfo, computing IdleFor from each session's LastActivity
// rather than carrying whatever the daemon happened to compute.
func TestRemoteClientListUsersConvertsSessionsAndIdleFor(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	lastActivity := time.Now().Add(-5 * time.Minute)
	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		kind, _, err := DecodeMessage(raw)
		if err != nil || kind != KindListUsersRequest {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindListUsersRequest)
			return
		}
		resp, err := EncodeMessage(KindListUsersResponse, ListUsersResponsePayload{
			Sessions: []SessionInfo{
				{ID: "session-1", Username: "alice", CommandLevel: "exec", ConnectedAt: time.Now(), LastActivity: lastActivity},
			},
		})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	got, err := c.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListUsers returned %d sessions, want 1", len(got))
	}
	if got[0].ID != "session-1" || got[0].Username != "alice" || got[0].CommandLevel != "exec" {
		t.Errorf("ListUsers()[0] = %+v", got[0])
	}
	if got[0].IdleFor < 4*time.Minute || got[0].IdleFor > 6*time.Minute {
		t.Errorf("IdleFor = %v, want roughly 5m", got[0].IdleFor)
	}
}

// TestRemoteClientDisconnectUserSuccess - This test verifies that
// DisconnectUser reports no error when the daemon answers with an empty
// DisconnectUserResponsePayload.Error, and that the request it sent
// carried the username and session ID it was given.
func TestRemoteClientDisconnectUserSuccess(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil || kind != KindDisconnectUserRequest {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindDisconnectUserRequest)
			return
		}
		req, err := unmarshalPayload[DisconnectUserRequestPayload](body)
		if err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Username != "bob" || req.SessionID != "abc123" {
			t.Errorf("request = %+v, want Username bob, SessionID abc123", req)
		}
		resp, err := EncodeMessage(KindDisconnectUserResponse, DisconnectUserResponsePayload{})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	if err := c.DisconnectUser("bob", "abc123"); err != nil {
		t.Errorf("DisconnectUser: %v", err)
	}
}

// TestRemoteClientDisconnectUserPropagatesDaemonError - This test verifies
// that a non-empty DisconnectUserResponsePayload.Error, the ambiguous
// session case for instance, comes back from DisconnectUser as a plain
// error carrying that same text.
func TestRemoteClientDisconnectUserPropagatesDaemonError(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		if _, _, err := DecodeMessage(raw); err != nil {
			t.Errorf("DecodeMessage: %v", err)
			return
		}
		resp, err := EncodeMessage(KindDisconnectUserResponse, DisconnectUserResponsePayload{Error: ErrAmbiguousSession.Error()})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	err := c.DisconnectUser("bob", "")
	if err == nil || err.Error() != ErrAmbiguousSession.Error() {
		t.Errorf("DisconnectUser returned %v, want an error reading %q", err, ErrAmbiguousSession.Error())
	}
}

// TestRemoteClientRebootSuccess - This test verifies that Reboot reports
// no error once the daemon answers KindRebootResponse with an empty Error,
// without Reboot itself waiting for any farewell.
func TestRemoteClientRebootSuccess(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		kind, _, err := DecodeMessage(raw)
		if err != nil || kind != KindRebootRequest {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindRebootRequest)
			return
		}
		resp, err := EncodeMessage(KindRebootResponse, RebootResponsePayload{})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	if err := c.Reboot(); err != nil {
		t.Errorf("Reboot: %v", err)
	}
}

// TestRemoteClientFarewellChannelReceivesDaemonPushedText - This test
// verifies that a KindFarewell pushed by the daemon, unprompted, arrives
// on FarewellChannel with its own text intact, and that a call already in
// flight when the push arrives fails rather than hanging forever waiting
// for a response that will never come.
func TestRemoteClientFarewellChannelReceivesDaemonPushedText(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer c.Close()

	listUsersDone := make(chan error, 1)
	go func() {
		_, err := c.ListUsers()
		listUsersDone <- err
	}()

	// Drain the ListUsersRequest the goroutine above just sent, then push
	// a farewell instead of ever answering it, standing in for a reboot or
	// a targeted disconnect landing on this exact session while it
	// happened to have a request outstanding.
	if _, err := daemonCh.Receive(); err != nil {
		t.Fatalf("daemon side Receive: %v", err)
	}
	farewell, err := EncodeMessage(KindFarewell, FarewellPayload{Text: FarewellRebooting})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if err := daemonCh.Send(farewell); err != nil {
		t.Fatalf("daemon side Send: %v", err)
	}

	select {
	case text := <-c.FarewellChannel():
		if text != FarewellRebooting {
			t.Errorf("FarewellChannel delivered %q, want %q", text, FarewellRebooting)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FarewellChannel never fired")
	}

	select {
	case err := <-listUsersDone:
		if err == nil {
			t.Error("ListUsers returned no error after the connection ended with a farewell instead of a response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the in flight ListUsers call never returned after the farewell arrived")
	}
}

// TestRemoteClientFarewellChannelReportsConnectionLostOnAbruptClose - This
// test verifies that closing the daemon side connection with no farewell
// at all, standing in for the daemon being killed outright, delivers
// ConnectionLostText on FarewellChannel, matching this project's settled
// design for a daemon killed outright.
func TestRemoteClientFarewellChannelReportsConnectionLostOnAbruptClose(t *testing.T) {
	c, _, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer c.Close()

	if err := daemonConn.Close(); err != nil {
		t.Fatalf("closing the daemon side conn: %v", err)
	}

	select {
	case text := <-c.FarewellChannel():
		if text != ConnectionLostText {
			t.Errorf("FarewellChannel delivered %q, want %q", text, ConnectionLostText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FarewellChannel never fired after the daemon side connection closed abruptly")
	}
}

// TestRemoteClientLogForwardsAuditEventOnlyWhenEnabled - This test
// verifies that Log sends nothing at all while disabled, matching
// auditlog.AuditLog.Log's "silently does nothing when auditing is
// disabled" behavior, and, once Enable is called, forwards a
// KindAuditEvent carrying username, command, level, and success exactly,
// SetLevel's value included.
func TestRemoteClientLogForwardsAuditEventOnlyWhenEnabled(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	if c.WouldLog() {
		t.Error("WouldLog() = true before Enable was ever called")
	}
	c.Log("alice", "show version", true)

	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !c.WouldLog() {
		t.Error("WouldLog() = false after Enable")
	}
	c.SetLevel("exec")

	received := make(chan AuditEventPayload, 1)
	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil || kind != KindAuditEvent {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindAuditEvent)
			return
		}
		evt, err := unmarshalPayload[AuditEventPayload](body)
		if err != nil {
			t.Errorf("unmarshal AuditEventPayload: %v", err)
			return
		}
		received <- evt
	}()

	c.Log("alice", "show version", true)

	select {
	case evt := <-received:
		if evt.Username != "alice" || evt.Command != "show version" || evt.Level != "exec" || !evt.Success {
			t.Errorf("AuditEvent = %+v, want Username alice, Command \"show version\", Level exec, Success true", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no AuditEvent arrived after Enable and Log")
	}

	c.Disable()
	if c.WouldLog() {
		t.Error("WouldLog() = true after Disable")
	}
}

// TestRemoteClientForceLogSendsEvenWhileDisabled - This test verifies that
// ForceLog forwards a KindAuditEvent regardless of Enable ever having been
// called, matching auditlog.AuditLog.ForceLog's doc comment and its one
// real use, "audit-log disable" logging its own entry despite the very
// state change it is reporting.
func TestRemoteClientForceLogSendsEvenWhileDisabled(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()
	defer closeClientDrainingDaemon(t, c, daemonCh)

	if c.WouldLog() {
		t.Fatal("WouldLog() = true before Enable was ever called")
	}

	received := make(chan AuditEventPayload, 1)
	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		_, body, err := DecodeMessage(raw)
		if err != nil {
			t.Errorf("DecodeMessage: %v", err)
			return
		}
		evt, err := unmarshalPayload[AuditEventPayload](body)
		if err != nil {
			t.Errorf("unmarshal AuditEventPayload: %v", err)
			return
		}
		received <- evt
	}()

	c.ForceLog("bob", "audit-log disable", true)

	select {
	case evt := <-received:
		if evt.Username != "bob" || evt.Command != "audit-log disable" {
			t.Errorf("AuditEvent = %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no AuditEvent arrived from ForceLog while disabled")
	}
}

// TestRemoteClientCloseSendsGoodbye - This test verifies that Close sends
// a KindGoodbye before closing the underlying connection, matching the
// description of when a Goodbye is sent.
func TestRemoteClientCloseSendsGoodbye(t *testing.T) {
	c, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()

	goodbyeReceived := make(chan MessageKind, 1)
	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			return
		}
		kind, _, err := DecodeMessage(raw)
		if err != nil {
			return
		}
		goodbyeReceived <- kind
	}()

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	select {
	case kind := <-goodbyeReceived:
		if kind != KindGoodbye {
			t.Errorf("received kind %s, want %s", kind, KindGoodbye)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no Goodbye arrived after Close")
	}
}

// TestConnectedClientSatisfiesCommandDaemonClient - This test verifies
// that a ConnectedClient combining a StandaloneClient and a RemoteClient
// forwards Reboot to the RemoteClient half rather than the
// StandaloneClient half's defensive ErrDaemonNotConfigured answer, the
// exact ambiguity Close's doc comment in connectedclient.go explains Go
// does not resolve automatically. See TestConnectedClientForwardsListUsers
// DisconnectUserAndFarewellChannelToRemoteClient just below for the same
// check against ConnectedClient's other three forwarding methods, and
// TestConnectedClientCloseClosesBothHalves for Close.
func TestConnectedClientSatisfiesCommandDaemonClient(t *testing.T) {
	standalone := NewStandaloneClient(NewState(nil, nil, nil, nil, nil))
	remote, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()

	cc := NewConnectedClient(standalone, remote)
	defer standalone.Close()
	defer closeClientDrainingDaemon(t, cc.RemoteClient, daemonCh)

	go func() {
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive: %v", err)
			return
		}
		if _, _, err := DecodeMessage(raw); err != nil {
			t.Errorf("DecodeMessage: %v", err)
			return
		}
		resp, err := EncodeMessage(KindRebootResponse, RebootResponsePayload{})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send: %v", err)
		}
	}()

	if err := cc.Reboot(); err != nil {
		t.Errorf("ConnectedClient.Reboot() = %v, want nil (the embedded RemoteClient's success), not command.ErrDaemonNotConfigured", err)
	}

	// The embedded StandaloneClient's Reboot, left untouched here, still
	// reports ErrDaemonNotConfigured on its own; this confirms
	// ConnectedClient.Reboot did reach the RemoteClient half above rather
	// than merely happening to return a nil error for some unrelated
	// reason.
	if err := standalone.Reboot(); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("the embedded StandaloneClient's Reboot() = %v, want command.ErrDaemonNotConfigured", err)
	}
}

// TestConnectedClientForwardsListUsersDisconnectUserAndFarewellChannelToRemoteClient
// - This test verifies that ListUsers, DisconnectUser, and
// FarewellChannel, the three ConnectedClient methods
// TestConnectedClientSatisfiesCommandDaemonClient above does not
// itself reach, also forward to the embedded RemoteClient half rather
// than the embedded StandaloneClient half's defensive
// ErrDaemonNotConfigured answers (nil for FarewellChannel, a channel
// a select statement simply never chooses). See ConnectedClient's
// doc comment in connectedclient.go for why each of these needs its
// own explicit, one line forwarding method in the first place, an
// ambiguous selector Go does not promote automatically since
// StandaloneClient and RemoteClient both implement every one of these
// under the identical name.
func TestConnectedClientForwardsListUsersDisconnectUserAndFarewellChannelToRemoteClient(t *testing.T) {
	standalone := NewStandaloneClient(NewState(nil, nil, nil, nil, nil))
	remote, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()

	cc := NewConnectedClient(standalone, remote)
	defer standalone.Close()
	defer closeClientDrainingDaemon(t, cc.RemoteClient, daemonCh)

	go func() {
		// ListUsers request/response.
		raw, err := daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive (ListUsers): %v", err)
			return
		}
		kind, _, err := DecodeMessage(raw)
		if err != nil || kind != KindListUsersRequest {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindListUsersRequest)
			return
		}
		resp, err := EncodeMessage(KindListUsersResponse, ListUsersResponsePayload{
			Sessions: []SessionInfo{{ID: "session-1", Username: "alice", CommandLevel: "exec"}},
		})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send (ListUsers): %v", err)
			return
		}

		// DisconnectUser request/response.
		raw, err = daemonCh.Receive()
		if err != nil {
			t.Errorf("daemon side Receive (DisconnectUser): %v", err)
			return
		}
		kind, _, err = DecodeMessage(raw)
		if err != nil || kind != KindDisconnectUserRequest {
			t.Errorf("daemon side received kind %s, err %v, want %s", kind, err, KindDisconnectUserRequest)
			return
		}
		resp, err = EncodeMessage(KindDisconnectUserResponse, DisconnectUserResponsePayload{})
		if err != nil {
			t.Errorf("EncodeMessage: %v", err)
			return
		}
		if err := daemonCh.Send(resp); err != nil {
			t.Errorf("daemon side Send (DisconnectUser): %v", err)
		}
	}()

	gotUsers, err := cc.ListUsers()
	if err != nil {
		t.Fatalf("ConnectedClient.ListUsers() = %v, want nil (the embedded RemoteClient's success), not command.ErrDaemonNotConfigured", err)
	}
	if len(gotUsers) != 1 || gotUsers[0].Username != "alice" {
		t.Errorf("ConnectedClient.ListUsers() = %+v, want one session for alice", gotUsers)
	}

	if err := cc.DisconnectUser("bob", "abc123"); err != nil {
		t.Errorf("ConnectedClient.DisconnectUser() = %v, want nil (the embedded RemoteClient's success), not command.ErrDaemonNotConfigured", err)
	}

	// The embedded StandaloneClient's versions, left untouched here, still
	// report ErrDaemonNotConfigured on their own, confirming the two calls
	// above did reach the RemoteClient half rather than merely happening
	// to succeed for some unrelated reason.
	if _, err := standalone.ListUsers(); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("the embedded StandaloneClient's ListUsers() = %v, want command.ErrDaemonNotConfigured", err)
	}
	if err := standalone.DisconnectUser("bob", ""); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("the embedded StandaloneClient's DisconnectUser() = %v, want command.ErrDaemonNotConfigured", err)
	}

	// FarewellChannel: the embedded StandaloneClient's version always
	// returns nil, a channel a select statement never chooses, so
	// receiving the pushed text below only happens at all if
	// ConnectedClient.FarewellChannel did return the embedded
	// RemoteClient's real channel.
	farewell, err := EncodeMessage(KindFarewell, FarewellPayload{Text: FarewellRebooting})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if err := daemonCh.Send(farewell); err != nil {
		t.Fatalf("daemon side Send (Farewell): %v", err)
	}
	select {
	case text := <-cc.FarewellChannel():
		if text != FarewellRebooting {
			t.Errorf("ConnectedClient.FarewellChannel() delivered %q, want %q", text, FarewellRebooting)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ConnectedClient.FarewellChannel() never fired")
	}
}

// TestConnectedClientCloseClosesBothHalves - This test verifies that
// ConnectedClient.Close closes both the embedded StandaloneClient's Store,
// confirmed here through ErrStoreClosed on a MutateProductState call made
// directly against that same StandaloneClient afterward, and the embedded
// RemoteClient's connection, confirmed through the real KindGoodbye
// TestRemoteClientCloseSendsGoodbye already checks for a bare
// RemoteClient.Close, so neither half is left lingering once a
// ConnectedClient built around them is closed.
func TestConnectedClientCloseClosesBothHalves(t *testing.T) {
	standalone := NewStandaloneClient(NewState(nil, nil, nil, nil, nil))
	remote, daemonCh, daemonConn := newConnectedTestRemoteClient(t, "session-1")
	defer daemonConn.Close()

	cc := NewConnectedClient(standalone, remote)

	goodbyeReceived := make(chan MessageKind, 1)
	go func() {
		for {
			raw, err := daemonCh.Receive()
			if err != nil {
				return
			}
			kind, _, err := DecodeMessage(raw)
			if err != nil {
				continue
			}
			if kind == KindGoodbye {
				goodbyeReceived <- kind
				return
			}
		}
	}()

	if err := cc.Close(); err != nil {
		t.Errorf("ConnectedClient.Close() = %v, want nil", err)
	}

	select {
	case <-goodbyeReceived:
	case <-time.After(2 * time.Second):
		t.Error("no Goodbye arrived after ConnectedClient.Close(), the embedded RemoteClient does not appear to have been closed")
	}

	if _, err := standalone.MutateProductState(func(ps any) (any, error) { return nil, nil }); err != ErrStoreClosed {
		t.Errorf("the embedded StandaloneClient's MutateProductState after ConnectedClient.Close() = %v, want ErrStoreClosed", err)
	}
}
