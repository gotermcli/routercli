// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gologme/log"
)

// TestAuditLogDisabledByDefaultWritesNothing - This test verifies that a
// freshly constructed AuditLog, never Enabled, never creates its log file
// at all when Log is called.
func TestAuditLogDisabledByDefaultWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a := New(path, nil)
	a.Log("alice", "show version", true)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file to be created while disabled, got err=%v", err)
	}
}

// TestAuditLogLogAtUsesTheGivenTimestampNotNow - This test verifies that
// LogAt records the timestamp it is given, the moment a session dispatched
// the command, rather than the moment the daemon wrote it.
//
// It also checks that LogAt follows Log's rule of doing nothing while
// disabled, tested before Enable is ever called.
func TestAuditLogLogAtUsesTheGivenTimestampNotNow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a := New(path, nil)

	a.LogAt(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), "alice", "show version", true)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file to be created while disabled, got err=%v", err)
	}

	if err := a.Enable(); err != nil {
		t.Fatalf("Enable returned error: %v", err)
	}
	when := time.Date(2020, 1, 1, 12, 30, 0, 0, time.UTC)
	a.LogAt(when, "bob", "reboot", true)
	if err := a.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	want := FormatEntry(when, "bob", "reboot", true)
	if string(got) != want {
		t.Errorf("log file content = %q, want %q", string(got), want)
	}
}

// TestAuditLogEnableWritesEntries - This test verifies that once Enabled,
// both a successful and a failed, unauthenticated command are written to
// the log file with the expected username, outcome, and command text.
func TestAuditLogEnableWritesEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a := New(path, nil)

	if err := a.Enable(); err != nil {
		t.Fatalf("Enable returned error: %v", err)
	}
	if !a.Enabled() {
		t.Error("Enabled() = false after Enable()")
	}

	a.Log("alice", "show running-config", true)
	a.Log("", "enable", false)

	if err := a.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "alice\tOK\tshow running-config") {
		t.Errorf("audit log missing expected success entry, got:\n%s", content)
	}
	if !strings.Contains(content, "-\tFAIL\tenable") {
		t.Errorf("audit log missing expected failure entry with '-' for unauthenticated, got:\n%s", content)
	}
}

// TestAuditLogDisableStopsWritesWithoutClosingFile - This test verifies
// that Disable stops further Log calls from being written, and that a
// later Enable resumes writing without needing to reopen the file. This is
// the toggle it on in a running instance requirement.
func TestAuditLogDisableStopsWritesWithoutClosingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a := New(path, nil)
	_ = a.Enable()
	a.Log("alice", "one", true)

	a.Disable()
	if a.Enabled() {
		t.Error("Enabled() = true after Disable()")
	}
	a.Log("alice", "should-not-appear", true)

	_ = a.Enable()
	a.Log("alice", "should-appear-again", true)
	_ = a.Close()

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "should-not-appear") {
		t.Error("a log call while disabled should not have been written")
	}
	if !strings.Contains(content, "should-appear-again") {
		t.Error("a log call after re-enabling should have been written")
	}
}

// TestAuditLogCloseWithoutEnableIsSafe - This test verifies that Close on
// an AuditLog that was never Enabled is a safe no-op rather than an error
// or a nil pointer dereference.
func TestAuditLogCloseWithoutEnableIsSafe(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "audit.log"), nil)
	if err := a.Close(); err != nil {
		t.Errorf("Close on a never enabled AuditLog should be a safe no-op, got error: %v", err)
	}
}

// TestForceLogRequiresFileToBeOpen - This test verifies that ForceLog does
// not create or write a file that was never opened through Enable, even
// though ForceLog otherwise bypasses the Enabled check.
func TestForceLogRequiresFileToBeOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a := New(path, nil)
	a.ForceLog("alice", "should not appear", true) // never Enabled, so a.file is still nil

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("ForceLog should not create or write a file that was never opened via Enable")
	}
}

// TestWriteLockedReportsWriteFailureToLogger - This test verifies that a
// failed write is reported to the logger rather than swallowed.
//
// The failure is induced by closing the file out from under an AuditLog
// that still believes it is enabled. Close leaves enabled true and file
// non-nil, so the next Log call reaches writeLocked and its write fails.
func TestWriteLockedReportsWriteFailureToLogger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	var buf bytes.Buffer
	testLogger := log.New(&buf, "", 0)
	testLogger.EnableLevel("error")

	a := New(path, testLogger)
	if err := a.Enable(); err != nil {
		t.Fatalf("Enable returned error: %v", err)
	}

	// Close the underlying file directly, while a still believes it is
	// enabled and open, to force the next write to fail.
	if err := a.file.Close(); err != nil {
		t.Fatalf("failed to close underlying file for test setup: %v", err)
	}

	a.Log("alice", "show version", true)

	if !strings.Contains(buf.String(), "audit log write failed") {
		t.Errorf("expected write failure to be reported to the logger, got:\n%s", buf.String())
	}
}

// TestNewFallsBackToStderrLoggerWhenNil - This test verifies that New does
// not panic or leave logger nil when called with a nil logger, since
// callers are not required to supply one.
func TestNewFallsBackToStderrLoggerWhenNil(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "audit.log"), nil)
	if a.logger == nil {
		t.Fatal("New(path, nil) should fall back to a default logger, got nil")
	}
}

// TestAuditLogWouldLogSnapshotsPreCommandState - This test verifies that a
// caller can ask whether a write would happen now, before running a command
// that might itself flip that answer, and use the snapshot to decide
// whether to log it afterward.
func TestAuditLogWouldLogSnapshotsPreCommandState(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "audit.log"), nil)
	_ = a.Enable()

	if !a.WouldLog() {
		t.Fatal("WouldLog() should be true while enabled")
	}

	shouldLog := a.WouldLog() // snapshot, as a caller would do before RunFunc()
	a.Disable()               // simulates the command's side effect
	if shouldLog {
		a.ForceLog("alice", "audit-log disable", true)
	}

	if a.WouldLog() {
		t.Error("WouldLog() should be false after Disable()")
	}

	data, _ := os.ReadFile(a.path)
	if !strings.Contains(string(data), "audit-log disable") {
		t.Error("the disable command itself should still have been logged, using the pre-command snapshot")
	}
}
