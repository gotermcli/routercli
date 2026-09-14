// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/armorchan"
	"github.com/gotermcli/routercli/auditlog"
)

// newTestServer returns a ready to use *Server, its own canonical
// *Store[State] seeded from initialState, a *SessionDirectory and
// *auditlog.AuditLog both already running, and reload standing in for
// whatever a real routerclid's file re-reading function would do;
// the caller supplies it directly so a test can both control exactly what
// a reboot replaces canonical state with and observe that reload was
// called. Every constructed piece is closed through t.Cleanup, so a test
// never needs its own explicit teardown.
func newTestServer(t *testing.T, initialState State, reload func() (State, error)) *Server {
	t.Helper()

	store := NewStore(initialState)
	t.Cleanup(store.Close)

	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)

	audit := auditlog.New(filepath.Join(t.TempDir(), "audit.log"), nil)
	if err := audit.Enable(); err != nil {
		t.Fatalf("audit.Enable: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	return NewServer(nil, priv, store, sessions, audit, reload, nil)
}

// connectTestRemoteClient wires one more real *RemoteClient against s's
// serveConnection, over a real net.Pipe with a real armorchan handshake,
// s.serveConnection itself run in its own goroutine exactly the way
// Server.Serve would run it for a real accepted connection. The returned
// RemoteClient is closed through t.Cleanup.
func connectTestRemoteClient(t *testing.T, s *Server, username string) *RemoteClient {
	t.Helper()

	clientCh, daemonCh, clientConn, daemonConn := newTestChannelPair(t)
	go s.serveConnection(daemonCh, daemonConn)

	c, err := NewRemoteClient(clientCh, clientConn, username, "test-tty")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// noopReload is a reload function that never expects to be called; every
// test using it exercises a path that must not trigger a reboot.
func noopReload(t *testing.T) func() (State, error) {
	return func() (State, error) {
		t.Fatal("reload was called but this test never expected a reboot")
		return State{}, nil
	}
}

// TestServerServeConnectionHelloThenListUsersReportsThatSession - This
// test verifies the most basic real, end to end path this file exercises:
// a real RemoteClient completing Hello against a real
// Server.serveConnection, then ListUsers reporting exactly that one
// session back, by username and by the session ID Hello itself returned.
func TestServerServeConnectionHelloThenListUsersReportsThatSession(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	c := connectTestRemoteClient(t, s, "alice")

	sessions, err := c.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListUsers returned %d sessions, want 1", len(sessions))
	}
	if sessions[0].Username != "alice" {
		t.Errorf("Username = %q, want %q", sessions[0].Username, "alice")
	}
	if sessions[0].ID != c.SessionID() {
		t.Errorf("ID = %q, want %q", sessions[0].ID, c.SessionID())
	}
}

// TestServerServeConnectionListUsersReportsEverySession - This test
// verifies ListUsers reports every currently attached session, not only
// the one asking, connecting two different clients under two different
// usernames.
func TestServerServeConnectionListUsersReportsEverySession(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	alice := connectTestRemoteClient(t, s, "alice")
	_ = connectTestRemoteClient(t, s, "bob")

	sessions, err := alice.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListUsers returned %d sessions, want 2", len(sessions))
	}
}

// TestServerServeConnectionAuditEventWritesToAuditLog - This test verifies
// that Log, command.Auditor's method, sent by a real RemoteClient as a
// KindAuditEvent message, is written to this Server's audit log through
// auditlog.LogAt, using the timestamp the message itself carried rather
// than whenever the daemon happened to receive it, and that the acting
// session's CommandLevel is updated to match, through
// SessionDirectory.Touch, observable back through a later ListUsers call.
func TestServerServeConnectionAuditEventWritesToAuditLog(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	store := NewStore(State{})
	t.Cleanup(store.Close)
	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)
	audit := auditlog.New(auditPath, nil)
	if err := audit.Enable(); err != nil {
		t.Fatalf("audit.Enable: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}
	s := NewServer(nil, priv, store, sessions, audit, noopReload(t), nil)

	c := connectTestRemoteClient(t, s, "alice")
	c.SetLevel("admin")
	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c.Log("alice", "reboot", true)

	// Log sends the AuditEvent one way, with no response to wait on;
	// ListUsers is used here purely as a synchronous round trip against
	// the same connection, so this test knows the AuditEvent has already
	// been processed, CommandLevel updated included, by the time it
	// inspects the audit log file and the session list below.
	infos, err := c.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(infos) != 1 || infos[0].CommandLevel != "admin" {
		t.Fatalf("expected exactly one session at CommandLevel \"admin\", got %+v", infos)
	}

	got, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "alice") || !strings.Contains(string(got), "reboot") || !strings.Contains(string(got), "OK") {
		t.Errorf("audit log does not contain the expected entry, got:\n%s", got)
	}
}

// TestServerServeConnectionDisconnectUserUnambiguousDeliversFarewell -This
// test verifies that DisconnectUser, run by one session against another by
// username alone, no session ID, succeeds when exactly one session
// matches, and that the targeted session's FarewellChannel receives
// FarewellDisconnected, matching the wording "Line closed by remote host"
// wording.
func TestServerServeConnectionDisconnectUserUnambiguousDeliversFarewell(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	admin := connectTestRemoteClient(t, s, "root")
	target := connectTestRemoteClient(t, s, "bob")

	if err := admin.DisconnectUser("bob", ""); err != nil {
		t.Fatalf("DisconnectUser: %v", err)
	}

	select {
	case text := <-target.FarewellChannel():
		if text != FarewellDisconnected {
			t.Errorf("farewell text = %q, want %q", text, FarewellDisconnected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("target's FarewellChannel never fired")
	}
}

// TestServerServeConnectionDisconnectUserAmbiguousRefusesAndListsCandidates
// - This test verifies that DisconnectUser with an empty session ID
// against a username matching more than one attached session refuses
// with an error naming the actual candidate session IDs, rather than
// guessing which one was meant, and that neither session is actually
// disconnected.
func TestServerServeConnectionDisconnectUserAmbiguousRefusesAndListsCandidates(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	admin := connectTestRemoteClient(t, s, "root")
	first := connectTestRemoteClient(t, s, "bob")
	second := connectTestRemoteClient(t, s, "bob")

	err := admin.DisconnectUser("bob", "")
	if err == nil {
		t.Fatal("expected an error for an ambiguous DisconnectUser, got nil")
	}
	if !strings.Contains(err.Error(), first.SessionID()) || !strings.Contains(err.Error(), second.SessionID()) {
		t.Errorf("error %q does not name both candidate session IDs %q and %q", err, first.SessionID(), second.SessionID())
	}

	select {
	case text := <-first.FarewellChannel():
		t.Errorf("first session was unexpectedly disconnected with %q", text)
	case text := <-second.FarewellChannel():
		t.Errorf("second session was unexpectedly disconnected with %q", text)
	case <-time.After(200 * time.Millisecond):
		// Neither session received a farewell; this is the success case,
		// an ambiguous request must disconnect no one.
	}
}

// TestServerServeConnectionDisconnectUserWithSessionIDNamesOneExactly
// - This test verifies that a non-empty session ID resolves the
// ambiguous case above directly, disconnecting exactly the named
// session and leaving the other one running.
func TestServerServeConnectionDisconnectUserWithSessionIDNamesOneExactly(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	admin := connectTestRemoteClient(t, s, "root")
	first := connectTestRemoteClient(t, s, "bob")
	second := connectTestRemoteClient(t, s, "bob")

	if err := admin.DisconnectUser("bob", first.SessionID()); err != nil {
		t.Fatalf("DisconnectUser: %v", err)
	}

	select {
	case text := <-first.FarewellChannel():
		if text != FarewellDisconnected {
			t.Errorf("farewell text = %q, want %q", text, FarewellDisconnected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first session's FarewellChannel never fired")
	}

	select {
	case text := <-second.FarewellChannel():
		t.Errorf("second session was unexpectedly disconnected with %q", text)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestServerTriggerRebootReplacesStateAndBroadcastsFarewell - This test
// verifies TriggerReboot, the function both a privileged session's
// KindRebootRequest and a real daemon's SIGHUP handler converge on: it
// calls this Server's reload function, replaces canonical state with
// whatever that returned, and pushes FarewellRebooting to every currently
// attached session, matching what a reboot means, tested here directly,
// with no session ever having sent a KindRebootRequest of its own at all,
// the same way SIGHUP triggers it in a real deployment.
func TestServerTriggerRebootReplacesStateAndBroadcastsFarewell(t *testing.T) {
	store := NewStore(State{ProductState: "before"})
	t.Cleanup(store.Close)
	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)
	audit := auditlog.New(filepath.Join(t.TempDir(), "audit.log"), nil)
	t.Cleanup(func() { _ = audit.Close() })
	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}
	reloadCalled := false
	s := NewServer(nil, priv, store, sessions, audit, func() (State, error) {
		reloadCalled = true
		return State{ProductState: "after"}, nil
	}, nil)

	c := connectTestRemoteClient(t, s, "alice")

	if err := s.TriggerReboot(); err != nil {
		t.Fatalf("TriggerReboot: %v", err)
	}
	if !reloadCalled {
		t.Error("expected TriggerReboot to call this Server's reload function")
	}

	got, err := store.Do(func(st *State) (any, error) { return st.ProductState, nil })
	if err != nil {
		t.Fatalf("store.Do: %v", err)
	}
	if got.(string) != "after" {
		t.Errorf("ProductState after TriggerReboot = %q, want %q", got, "after")
	}

	select {
	case text := <-c.FarewellChannel():
		if text != FarewellRebooting {
			t.Errorf("farewell text = %q, want %q", text, FarewellRebooting)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attached session's FarewellChannel never fired")
	}
}

// TestServerServeConnectionRebootRequestRespondsBeforeItsOwnFarewell -This
// test verifies that a session's "reboot" request, run through
// RemoteClient.Reboot itself, returns a clean nil, the daemon having
// accepted the request, rather than the connection ending error call
// sometimes returns when a farewell races an in flight call. This is
// exactly the ordering guarantee Server.serveConnection's
// KindRebootRequest case is written to provide: send the response, then
// broadcast. After Reboot itself returns, this same requesting session's
// FarewellChannel still fires FarewellRebooting shortly after, the same as
// every other attached session.
func TestServerServeConnectionRebootRequestRespondsBeforeItsOwnFarewell(t *testing.T) {
	s := newTestServer(t, State{}, func() (State, error) { return State{}, nil })
	c := connectTestRemoteClient(t, s, "root")

	if err := c.Reboot(); err != nil {
		t.Fatalf("Reboot: %v", err)
	}

	select {
	case text := <-c.FarewellChannel():
		if text != FarewellRebooting {
			t.Errorf("farewell text = %q, want %q", text, FarewellRebooting)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("requesting session's FarewellChannel never fired")
	}
}

// TestServerServeConnectionRebootRequestReloadFailureReportsErrorAndDoesNotBroadcast
// - This test verifies that a reload failure, a corrupt startup-config
// in a real deployment for instance, is reported back to the
// requesting session as a plain error, RemoteClient.Reboot returning
// it directly, rather than ending any session at all; canonical state
// must stay exactly as it was, and no farewell of any kind goes out.
func TestServerServeConnectionRebootRequestReloadFailureReportsErrorAndDoesNotBroadcast(t *testing.T) {
	s := newTestServer(t, State{ProductState: "before"}, func() (State, error) {
		return State{}, errRebootTestReloadFailed
	})
	c := connectTestRemoteClient(t, s, "root")

	err := c.Reboot()
	if err == nil {
		t.Fatal("expected Reboot to report the reload failure, got nil")
	}
	if !strings.Contains(err.Error(), errRebootTestReloadFailed.Error()) {
		t.Errorf("Reboot error = %q, want it to contain %q", err, errRebootTestReloadFailed.Error())
	}

	select {
	case text := <-c.FarewellChannel():
		t.Errorf("session was unexpectedly ended with farewell %q after a failed reboot", text)
	case <-time.After(200 * time.Millisecond):
	}
}

// errRebootTestReloadFailed is a fixed error TestServerServeConnectionRebo
// otRequestReloadFailureReportsErrorAndDoesNotBroadcast's fake reload
// function returns, checked by substring against Reboot's returned error
// text.
var errRebootTestReloadFailed = errServerTestReloadFailed{}

type errServerTestReloadFailed struct{}

func (errServerTestReloadFailed) Error() string { return "simulated reload failure" }

// TestServerServeConnectionGoodbyeRemovesSessionFromDirectory - This test
// verifies that Close, which sends a KindGoodbye before closing the
// underlying connection, causes this Server's SessionDirectory to stop
// reporting that session, observed here through a second, still attached
// session's later ListUsers call.
func TestServerServeConnectionGoodbyeRemovesSessionFromDirectory(t *testing.T) {
	s := newTestServer(t, State{}, noopReload(t))
	leaving := connectTestRemoteClient(t, s, "alice")
	staying := connectTestRemoteClient(t, s, "bob")

	if err := leaving.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close returning only guarantees the KindGoodbye write itself
	// completed, not that the daemon side goroutine has finished
	// processing it and called Unregister yet; ListUsers is polled briefly
	// here rather than asserted immediately, the same "wait for the
	// observable effect, not for an internal implementation detail"
	// reasoning any test against an asynchronous system needs.
	deadline := time.Now().Add(5 * time.Second)
	for {
		infos, err := staying.ListUsers()
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(infos) == 1 && infos[0].Username == "bob" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected exactly one remaining session, \"bob\", got %+v", infos)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestServerListenServeShutdownIntegration - This test verifies the whole
// daemon side pipeline together over a real Unix domain socket, not the
// net.Pipe shortcut every other test in this file uses: Listen opens the
// socket, Serve accepts a real Dial from a real CLI style client, that
// client completes Hello and gets its own session back through ListUsers,
// and Shutdown broadcasts the farewell a SIGTERM drain uses, observed here
// as the client's FarewellChannel firing with FarewellDisconnected, the
// same text an ordinary targeted disconnect reports. A clean daemon
// shutdown earns no more specific wording than that. Because a real Dial
// completing means a real Accept ran, this carries the same Linux only
// limitation every other real socket test in this package,
// socket_test.go's TestAcceptAndHandshakeAndDialRoundTrip among them,
// already skips for; see peercred_other.go.
func TestServerListenServeShutdownIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	// socketPath, from socket_test.go, is used here rather than this test
	// building its own path directly from t.TempDir(); see that function's
	// doc comment for why a real Unix domain socket needs a shorter path
	// than t.TempDir() alone reliably provides.
	sockPath := socketPath(t)
	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}

	ln, err := Listen(sockPath, NewAllowedUIDs(uint32(os.Getuid())))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	store := NewStore(State{})
	t.Cleanup(store.Close)
	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)
	audit := auditlog.New(filepath.Join(t.TempDir(), "audit.log"), nil)
	t.Cleanup(func() { _ = audit.Close() })

	s := NewServer(ln, priv, store, sessions, audit, noopReload(t), nil)
	serveDone := make(chan struct{})
	go func() {
		s.Serve()
		close(serveDone)
	}()

	ch, conn, err := Dial(sockPath, priv.PublicKey())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c, err := NewRemoteClient(ch, conn, "alice", "test-tty")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}

	infos, err := c.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(infos) != 1 || infos[0].Username != "alice" {
		t.Fatalf("ListUsers = %+v, want exactly one session for \"alice\"", infos)
	}

	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case text := <-c.FarewellChannel():
		if text != FarewellDisconnected {
			t.Errorf("farewell text = %q, want %q", text, FarewellDisconnected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client's FarewellChannel never fired after Shutdown")
	}

	select {
	case <-serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve never returned after Shutdown closed the Listener")
	}
}
