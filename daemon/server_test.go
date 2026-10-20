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

	"crypto/ecdh"
	"crypto/rand"
	"github.com/gotermcli/routercli/armorchan"
	"github.com/gotermcli/routercli/auditlog"
	"github.com/gotermcli/routercli/command"
	"net"
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

	return NewServer(nil, priv, store, sessions, audit, reload, nil, nil)
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
	s := NewServer(nil, priv, store, sessions, audit, noopReload(t), nil, nil)

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

// TestServerServeConnectionDisconnectUserUnambiguousDeliversFarewell -
// This
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
// verifies TriggerReboot, the function a privileged session's reboot request
// and a daemon's SIGHUP handler both converge on.
//
// It calls the Server's reload function, replaces canonical state with what
// that returned, and pushes FarewellRebooting to every attached session.
//
// It is called directly here, with no session having sent a reboot request,
// which is how SIGHUP triggers it in a real deployment.
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
	}, nil, nil)

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

// TestServerServeConnectionRebootRequestRespondsBeforeItsOwnFarewell - This
// test verifies that a session's reboot request, sent through
// RemoteClient.Reboot, returns a clean nil rather than the connection ended
// error a call sometimes returns when a farewell races it.
//
// That is the ordering serveConnection's reboot case exists to provide: send
// the response, then broadcast.
//
// The requesting session's FarewellChannel still fires FarewellRebooting
// shortly after Reboot returns, the same as every other attached session.
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
// daemon side pipeline over a real Unix domain socket, rather than the
// net.Pipe shortcut the other tests in this file use.
//
// Listen opens the socket, Serve accepts a real Dial from a CLI style client,
// that client completes Hello and finds its own session through ListUsers,
// and Shutdown broadcasts the farewell a SIGTERM drain sends.
//
// The client observes that as FarewellDisconnected, the same text a targeted
// disconnect reports. A clean shutdown earns no more specific wording.
//
// A real Dial completing means a real Accept ran, so this carries the same
// Linux only limitation every other real socket test here skips for.
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

	s := NewServer(ln, priv, store, sessions, audit, noopReload(t), nil, nil)
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

// newRealSocketTestServer opens a real Unix domain socket, Listen and Serve
// both already running against it, AllowedUIDs naming exactly this test
// process's own real UID. It returns the ready *Server, the socket path a
// real Dial connects to, and the audit log path a test wants to inspect
// afterward. Every piece it constructs is closed through t.Cleanup.
//
// privilegedUIDs is passed straight through to NewServer, so a caller
// controls exactly what KindRebootRequest and KindDisconnectUserRequest
// require. This exists separately from newTestServer's net.Pipe shortcut
// because checkPeerCredential cannot read a peer credential from a pipe, so
// privilegedUIDs would never be meaningfully exercised there; only a real
// *net.UnixConn carries one.
func newRealSocketTestServer(t *testing.T, initialState State, reload func() (State, error), privilegedUIDs PeerCredentialChecker) (srv *Server, sockPath, auditPath string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking, and this package's tests for it, are Linux only for now; see peercred_other.go")
	}

	sockPath = socketPath(t)
	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}
	ln, err := Listen(sockPath, NewAllowedUIDs(uint32(os.Getuid())))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	store := NewStore(initialState)
	t.Cleanup(store.Close)
	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)
	auditPath = filepath.Join(t.TempDir(), "audit.log")
	audit := auditlog.New(auditPath, nil)
	if err := audit.Enable(); err != nil {
		t.Fatalf("audit.Enable: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	srv = NewServer(ln, priv, store, sessions, audit, reload, nil, privilegedUIDs)
	go srv.Serve()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return srv, sockPath, auditPath
}

// dialRealSocketTestServer completes a real Dial and Hello against sockPath,
// as returned by newRealSocketTestServer, and returns the ready
// *RemoteClient, closed through t.Cleanup.
func dialRealSocketTestServer(t *testing.T, s *Server, sockPath, username string) *RemoteClient {
	t.Helper()
	ch, conn, err := Dial(sockPath, s.staticPrivate.PublicKey())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c, err := NewRemoteClient(ch, conn, username, "test-tty")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestServerServeConnectionRebootRequestRefusedWhenPeerUIDNotPrivileged -
// This test verifies that KindRebootRequest is refused, over a real Unix
// domain socket where a real peer UID exists to check, for a peer whose UID
// AllowedUIDs already let onto the socket but privilegedUIDs does not
// separately name. reload MUST NOT run, no farewell of any kind goes out,
// and the refusal MUST be written to the audit log naming the command and
// FAIL, exactly the accountability Findings 10 and 11 already require of
// every other privileged action.
func TestServerServeConnectionRebootRequestRefusedWhenPeerUIDNotPrivileged(t *testing.T) {
	// One away from this test process's own real UID names an account
	// nothing here runs as, so privilegedUIDs refuses every connection this
	// test opens, even though AllowedUIDs, checked at accept time, already
	// let this same connection through.
	other := NewAllowedUIDs(uint32(os.Getuid()) + 1)
	s, sockPath, auditPath := newRealSocketTestServer(t, State{}, noopReload(t), other)
	c := dialRealSocketTestServer(t, s, sockPath, "root")

	err := c.Reboot()
	if err == nil {
		t.Fatal("expected Reboot to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("Reboot error = %q, want it to name an authorization refusal", err)
	}

	select {
	case text := <-c.FarewellChannel():
		t.Errorf("session was unexpectedly ended with farewell %q after a refused reboot", text)
	case <-time.After(200 * time.Millisecond):
	}

	got, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "root") || !strings.Contains(string(got), "reboot") || !strings.Contains(string(got), "FAIL") {
		t.Errorf("audit log does not contain the expected refusal entry, got:\n%s", got)
	}
}

// TestServerServeConnectionRebootRequestAllowedWhenPeerUIDPrivileged - This
// test verifies that setting privilegedUIDs is not itself a new
// restriction: a peer whose real UID AllowedUIDs already let onto the
// socket may still reboot it, exactly as before privilegedUIDs existed,
// once that same UID is named on both lists.
func TestServerServeConnectionRebootRequestAllowedWhenPeerUIDPrivileged(t *testing.T) {
	self := NewAllowedUIDs(uint32(os.Getuid()))
	s, sockPath, _ := newRealSocketTestServer(t, State{}, func() (State, error) { return State{}, nil }, self)
	c := dialRealSocketTestServer(t, s, sockPath, "root")

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

// TestServerServeConnectionDisconnectUserRequestRefusedWhenPeerUIDNotPrivileged
// - This test verifies that KindDisconnectUserRequest is refused under the
// same privilegedUIDs gate as KindRebootRequest, over a real socket, that
// the refusal is audited under its own command name, and that a refused
// request leaves the targeted session running rather than disconnecting it
// anyway.
func TestServerServeConnectionDisconnectUserRequestRefusedWhenPeerUIDNotPrivileged(t *testing.T) {
	other := NewAllowedUIDs(uint32(os.Getuid()) + 1)
	s, sockPath, auditPath := newRealSocketTestServer(t, State{}, noopReload(t), other)
	requester := dialRealSocketTestServer(t, s, sockPath, "mallory")
	target := dialRealSocketTestServer(t, s, sockPath, "bob")

	err := requester.DisconnectUser("bob", "")
	if err == nil {
		t.Fatal("expected DisconnectUser to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("DisconnectUser error = %q, want it to name an authorization refusal", err)
	}

	select {
	case text := <-target.FarewellChannel():
		t.Errorf("target session was unexpectedly ended with farewell %q after a refused disconnect", text)
	case <-time.After(200 * time.Millisecond):
	}

	got, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "mallory") || !strings.Contains(string(got), "disconnect.user") || !strings.Contains(string(got), "FAIL") {
		t.Errorf("audit log does not contain the expected refusal entry, got:\n%s", got)
	}
}

// TestServeSurvivesAConnectionThatFailsItsHandshake - This test verifies that
// one connection failing its handshake does not stop the Server accepting
// others.
//
// This is a denial of service any permitted peer can trigger with four bytes.
// A handshake failure is entirely under the peer's control, so an accept loop
// that returns on one ends the daemon's ability to accept anything ever again.
// The process stays alive and already attached sessions keep working, so the
// daemon looks healthy while no new session can connect.
//
// The test dials, writes bytes that cannot begin a valid handshake, hangs up,
// and then requires a legitimate handshake to succeed.
func TestServeSurvivesAConnectionThatFailsItsHandshake(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential checking is implemented for Linux only, so a real socket cannot be accepted here")
	}

	sock := filepath.Join(t.TempDir(), "d.sock")
	staticPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating the daemon static key: %v", err)
	}

	ln, err := Listen(sock, AllowedUIDs{uint32(os.Getuid()): true})
	if err != nil {
		t.Skipf("cannot listen on a Unix socket here: %v", err)
	}
	defer ln.Close()

	store := NewStore(State{Levels: &command.TreeStructure{}})
	defer store.Close()
	sessions := NewSessionDirectory()
	defer sessions.Close()

	srv := NewServer(ln, staticPrivate, store, sessions, nil,
		func() (State, error) { return State{}, nil }, nil, nil)
	go srv.Serve()

	bad, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dialling as the hostile peer: %v", err)
	}
	if _, err := bad.Write([]byte{0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("writing garbage: %v", err)
	}
	bad.Close()
	time.Sleep(150 * time.Millisecond)

	good, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("the daemon stopped accepting after one bad handshake: %v", err)
	}
	defer good.Close()
	if err := good.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting a deadline: %v", err)
	}
	if _, err := armorchan.ClientHandshake(good, staticPrivate.PublicKey()); err != nil {
		t.Fatalf("the accept loop died: one garbage handshake stopped the daemon: %v", err)
	}
}

// TestAuditEventIsAttributedToTheSessionNotThePayload - This test verifies
// that an audit entry names the user the connection registered as, and carries
// the daemon's clock, rather than whatever the event itself claimed.
//
// Every field of an AuditEventPayload arrives from the client. If the daemon
// wrote payload.Username, any connected session could attribute any command to
// any user, and the one record of who did what would be written by whoever is
// being recorded. If it wrote payload.Time unchecked, an entry could be
// backdated out of whatever window an investigation looks at.
//
// The command text and the success flag still come from the client, because
// only the client knows what was typed. What this pins is that the actor and
// the moment are the daemon's to decide.
func TestAuditEventIsAttributedToTheSessionNotThePayload(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	audit := auditlog.New(auditPath, nil)
	if err := audit.Enable(); err != nil {
		t.Fatalf("enabling the audit log: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	store := NewStore(State{})
	t.Cleanup(store.Close)
	sessions := NewSessionDirectory()
	t.Cleanup(sessions.Close)
	priv, err := armorchan.GenerateStaticKeyPair()
	if err != nil {
		t.Fatalf("GenerateStaticKeyPair: %v", err)
	}
	s := NewServer(nil, priv, store, sessions, audit,
		func() (State, error) { return State{}, nil }, nil, nil)

	clientCh, daemonCh, clientConn, daemonConn := newTestChannelPair(t)
	go s.serveConnection(daemonCh, daemonConn)

	c, err := NewRemoteClient(clientCh, clientConn, "operator", "test-tty")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// A raw protocol message naming a different user and a distant time,
	// which is what a hostile client would send.
	raw, err := EncodeMessage(KindAuditEvent, AuditEventPayload{
		Username: "someone-else",
		Command:  "erase startup-config",
		Level:    "admin",
		Time:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		Success:  true,
	})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if err := clientCh.Send(raw); err != nil {
		t.Fatalf("sending the event: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	line := string(data)

	if strings.Contains(line, "someone-else") {
		t.Errorf("the entry was attributed to the username in the payload: %q", line)
	}
	if !strings.Contains(line, "operator") {
		t.Errorf("the entry should name the session's own user, got %q", line)
	}
	if strings.Contains(line, "2020") {
		t.Errorf("the entry carried the client's timestamp: %q", line)
	}
}
