// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auditlog

import (
	"fmt"
	"os"
	"time"
)

// ----------------------------------------------------------------------
// Public Methods - AuditLog
// ----------------------------------------------------------------------

// Enable - This method turns on audit logging and opens the underlying
// file in append mode, if it is not already open. This is safe to call
// whether logging was previously on or off, and safe to call from a
// running session.
func (a *AuditLog) Enable() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.file == nil {
		f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			return fmt.Errorf("error opening audit log %q: %w", a.path, err)
		}
		a.file = f
	}
	a.enabled = true
	return nil
}

// Disable - This method turns off audit logging. It leaves the file handle
// open rather than closing it. There is no reason to release the handle
// just because logging is paused, and leaving it open lets auditing be
// re-enabled later without reopening the file.
func (a *AuditLog) Disable() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = false
}

// Enabled - This method reports whether audit logging is currently on.
func (a *AuditLog) Enabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enabled
}

// WouldLog - "audit-log disable" turns auditing off as its own side
// effect. By the time it returns, a plain Log call would find auditing
// already off and swallow the entry for the command that did it.
//
// This reports whether Log would write anything right now. A caller
// snapshots it before dispatching, and uses that snapshot afterward to
// decide whether to record the command. See ForceLog for the write half.
func (a *AuditLog) WouldLog() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enabled && a.file != nil
}

// Close - This method closes the underlying file, if one was ever opened.
// Safe to call even if auditing was never enabled.
func (a *AuditLog) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}

// Log - This method records one audit entry holding a timestamp, who ran
// the command, the command line, and whether it succeeded. username is
// recorded as "-" for a session that is not authenticated.
//
// It does nothing when auditing is disabled or the file is not open, so
// most callers do not check WouldLog first. The exception is a command
// that disables auditing as its own side effect; see ForceLog.
//
// The timestamp is RFC 3339 rather than the standard library default,
// because an audit record has to be unambiguous across time zones and
// machine readable by whatever later parses the file.
func (a *AuditLog) Log(username, command string, success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.enabled || a.file == nil {
		return
	}
	a.writeLocked(time.Now(), username, command, success)
}

// ForceLog - This method writes an entry whenever the file is open,
// skipping the enabled check Log performs.
//
// It exists for one situation: a caller read WouldLog as true, then ran a
// command whose side effect turned auditing off, such as
// "audit-log disable". Without ForceLog that command swallows its own
// entry, because auditing is already off by the time anything tries to
// write it. Every other caller uses Log.
func (a *AuditLog) ForceLog(username, command string, success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.file == nil {
		return
	}
	a.writeLocked(time.Now(), username, command, success)
}

// LogAt - This method is Log with an explicit timestamp instead of the
// current time.
//
// The daemon uses it when writing an AuditEvent a CLI session sent, so the
// entry carries the moment that session dispatched the command rather than
// the later moment the daemon wrote it. Every other rule Log follows
// applies here unchanged.
func (a *AuditLog) LogAt(when time.Time, username, command string, success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.enabled || a.file == nil {
		return
	}
	a.writeLocked(when, username, command, success)
}

// ----------------------------------------------------------------------
// Public Functions
// ----------------------------------------------------------------------

// FormatEntry - Two things write audit lines: this package, and a daemon
// writing on behalf of a session that sent it an AuditEvent. One
// formatting function keeps them from drifting into two slightly
// different ideas of what an audit line looks like.
//
// This builds one line: timestamp, username, OK or FAIL, and the command
// text, tab separated. username is "-" when empty, the convention for an
// unauthenticated session.
//
// The timestamp is RFC 3339 so a record stays unambiguous across time
// zones and machine readable.
//
// when is a parameter rather than a time.Now call, so a daemon can record
// the moment a session dispatched the command rather than the later
// moment it got around to writing it.
func FormatEntry(when time.Time, username, command string, success bool) string {
	if username == "" {
		username = "-"
	}
	result := "OK"
	if !success {
		result = "FAIL"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\n", when.Format(time.RFC3339), username, result, command)
}

// ----------------------------------------------------------------------
// Private Methods
// ----------------------------------------------------------------------

// writeLocked - This method does the actual write. Callers must hold a.mu
// and have already checked that a.file is not nil.
func (a *AuditLog) writeLocked(when time.Time, username, command string, success bool) {
	line := FormatEntry(when, username, command, success)
	// A transient write failure here must never crash the CLI. An audit
	// log that can bring down the tool would itself be a significant
	// reliability problem, so the error is reported to the general logger
	// instead of being returned or panicked on.
	if _, err := a.file.WriteString(line); err != nil {
		a.logger.Errorf("audit log write failed: %v", err)
	}
}
