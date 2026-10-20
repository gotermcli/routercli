// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/auditlog"
)

// newTestAuditor - This function constructs a real *auditlog.AuditLog
// writing to a throwaway file under t.TempDir(), with a discard logger,
// since these tests care about Enable/Disable/Enabled state, not anything
// the AuditLog's logger would report.
func newTestAuditor(t *testing.T) *auditlog.AuditLog {
	t.Helper()
	return auditlog.New(filepath.Join(t.TempDir(), "audit.log"), log.New(io.Discard, "", 0))
}

// TestAuditLogEnableHandlerReportsNotConfiguredWithoutAuditor - This test
// verifies that "audit-log enable" reports "not configured" rather than
// erroring or panicking when ctx.Audit is nil, the state a project that
// never wired an audit log in at all leaves it in.
func TestAuditLogEnableHandlerReportsNotConfiguredWithoutAuditor(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "audit-log.enable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Errorf("audit-log.enable handler returned unexpected error: %v", err)
	}
}

// TestAuditLogEnableHandlerActuallyEnablesRealAuditor - This test verifies
// that "audit-log enable" with a real *auditlog.AuditLog wired onto
// ctx.Audit turns logging on, confirmed through the same
// *auditlog.AuditLog's Enabled method.
func TestAuditLogEnableHandlerActuallyEnablesRealAuditor(t *testing.T) {
	ctx := newTestContext()
	a := newTestAuditor(t)
	ctx.Audit = a
	cmd := loadTestCommand(t, "audit-log.enable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("audit-log.enable handler returned unexpected error: %v", err)
	}
	if !a.Enabled() {
		t.Error("expected the AuditLog to be enabled after audit-log.enable")
	}
}

// TestAuditLogDisableHandlerActuallyDisablesRealAuditor - This test
// verifies that "audit-log disable" turns a previously enabled real
// auditor back off.
func TestAuditLogDisableHandlerActuallyDisablesRealAuditor(t *testing.T) {
	ctx := newTestContext()
	a := newTestAuditor(t)
	if err := a.Enable(); err != nil {
		t.Fatalf("test setup: Enable returned error: %v", err)
	}
	ctx.Audit = a
	cmd := loadTestCommand(t, "audit-log.disable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("audit-log.disable handler returned unexpected error: %v", err)
	}
	if a.Enabled() {
		t.Error("expected the AuditLog to be disabled after audit-log.disable")
	}
}

// TestAuditLogDisableHandlerReportsNotConfiguredWithoutAuditor - This test
// verifies the same "not configured" reporting as the enable handler's
// test, for the disable handler.
func TestAuditLogDisableHandlerReportsNotConfiguredWithoutAuditor(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "audit-log.disable")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Errorf("audit-log.disable handler returned unexpected error: %v", err)
	}
}

// TestAuditLogStatusHandlerReflectsRealAuditorState - This test verifies
// that "audit-log status" runs without error both before and after a real
// auditor has been enabled. The handler only prints one of two fixed lines
// depending on Enabled(), so this confirms neither branch errors rather
// than asserting on captured output.
func TestAuditLogStatusHandlerReflectsRealAuditorState(t *testing.T) {
	ctx := newTestContext()
	a := newTestAuditor(t)
	ctx.Audit = a
	cmd := loadTestCommand(t, "audit-log.status")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("audit-log.status handler returned unexpected error (disabled): %v", err)
	}
	if err := a.Enable(); err != nil {
		t.Fatalf("test setup: Enable returned error: %v", err)
	}
	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("audit-log.status handler returned unexpected error (enabled): %v", err)
	}
}

// TestAuditLogStatusHandlerReportsNotConfiguredWithoutAuditor - This test
// verifies the same "not configured" reporting as the enable and disable
// handlers' own tests, for the status handler.
func TestAuditLogStatusHandlerReportsNotConfiguredWithoutAuditor(t *testing.T) {
	ctx := newTestContext()
	cmd := loadTestCommand(t, "audit-log.status")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Errorf("audit-log.status handler returned unexpected error: %v", err)
	}
}
