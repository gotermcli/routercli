// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/command"
)

// TestShowUsersHandlerNoDaemonReportsFailure - This test verifies that
// "show users", run against the plain daemon.NewStandaloneClient
// newTestContext already provides, no real daemon configured, reports a
// clear failure rather than an empty listing or a panic. This exercises
// the defense-in-depth path only, since a real deployment with no daemon
// configured has this command pruned out of its own tree entirely, see
// main.go's featureFlags.
func TestShowUsersHandlerNoDaemonReportsFailure(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "show.users")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr == nil {
		t.Fatal("expected show.users to fail with no daemon configured")
	}
	if out != "" {
		t.Errorf("expected no output when show.users fails, got %q", out)
	}
}

// TestShowUsersHandlerEmptyPrintsEmptyMessage - This test verifies that a
// real daemon connection reporting zero attached sessions prints the "no
// sessions" message rather than an empty, silent listing, the same
// convention show.aliases and show.history already follow for their own
// empty cases.
func TestShowUsersHandlerEmptyPrintsEmptyMessage(t *testing.T) {
	ctx := newTestContext()
	ctx.DaemonClient = &fakeDaemonClient{}
	cmd := loadTestCommand(t, "show.users")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.users handler returned unexpected error: %v", runErr)
	}
	if !strings.Contains(out, "[[show.users.empty]]") {
		t.Errorf("output = %q, want the empty users message", out)
	}
}

// TestShowUsersHandlerPrintsEverySession - This test verifies that "show
// users" prints one row per session ctx.DaemonClient.ListUsers reports,
// each row naming that session's ID, username, and Command Level.
func TestShowUsersHandlerPrintsEverySession(t *testing.T) {
	ctx := newTestContext()
	ctx.DaemonClient = &fakeDaemonClient{
		ListUsersResult: []command.SessionInfo{
			{ID: "aaaa1111", Username: "alice", CommandLevel: "exec", ConnectedAt: time.Now(), IdleFor: 2 * time.Minute},
			{ID: "bbbb2222", Username: "bob", CommandLevel: "admin", ConnectedAt: time.Now(), IdleFor: 0},
		},
	}
	cmd := loadTestCommand(t, "show.users")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("show.users handler returned unexpected error: %v", runErr)
	}
	for _, want := range []string{"aaaa1111", "alice", "exec", "bbbb2222", "bob", "admin"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
}

// TestShowUsersHandlerListFailurePropagatesError - This test verifies that
// a genuine ListUsers failure, anything other than an empty result, is
// reported back rather than silently printing nothing.
func TestShowUsersHandlerListFailurePropagatesError(t *testing.T) {
	ctx := newTestContext()
	ctx.DaemonClient = &fakeDaemonClient{ListUsersErr: errors.New("daemon: connection lost")}
	cmd := loadTestCommand(t, "show.users")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Fatal("expected show.users to fail when ListUsers itself fails")
	}
}

// TestUsersLinesPadsColumnsToTheLongestValue - This test verifies that
// usersLines pads every column wide enough to hold its own longest value,
// this call's session data included, not only its own fixed header text,
// so a long username or session ID never runs straight into the next
// column with no separation.
func TestUsersLinesPadsColumnsToTheLongestValue(t *testing.T) {
	ctx := newTestContext()
	sessions := []command.SessionInfo{
		{ID: "s1", Username: "a-very-long-username", CommandLevel: "exec", ConnectedAt: time.Now(), IdleFor: 0},
	}
	lines := usersLines(ctx, sessions)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (header plus one row)", len(lines))
	}
	if !strings.Contains(lines[1], "a-very-long-username") {
		t.Errorf("row = %q, want it to contain the long username", lines[1])
	}
	headerUsernameCol := strings.Index(lines[0], "Username")
	rowUsernameCol := strings.Index(lines[1], "a-very-long-username")
	if headerUsernameCol != rowUsernameCol {
		t.Errorf("header's Username column starts at %d, row's username starts at %d, want them aligned", headerUsernameCol, rowUsernameCol)
	}
}
