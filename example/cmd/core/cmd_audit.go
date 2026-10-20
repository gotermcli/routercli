// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"fmt"

	"github.com/gotermcli/routercli/command"
)

// enableDisabler is the small interface both *auditlog.AuditLog and
// *daemon.RemoteClient satisfy, Enable, Disable, and Enabled, letting the
// three handlers below operate against ctx.Audit however it is backed
// today, a plain local file in standalone mode or a real daemon connection
// once one is configured, without package core needing to import package
// daemon just to name *daemon.RemoteClient specifically here; see that
// type's doc comment in daemon/remoteclient.go, which anticipates exactly
// this interface.
type enableDisabler interface {
	Enable() error
	Disable()
	Enabled() bool
}

// init - This registers "audit-log enable", "audit-log disable", and
// "audit-log status".
//
// The first two are reachable only past the base level. That is a property
// of the tree files, not of this one: both are listed in level_exec.yaml
// and never in level_base.yaml or level_common.yaml, so a base level
// session cannot resolve them at all.
//
// Turning audit logging off is exactly what someone covering their tracks
// reaches for first, so it must never be available unelevated. Tree
// placement alone guarantees that.
//
// ctx.Audit is typed as command.Auditor, which has only Log. These three
// handlers need Enable, Disable, and Enabled, so each asserts back to the
// small enableDisabler interface above rather than naming either concrete
// type.
func init() {
	command.Register("audit-log.enable", func(ctx *command.AppContext, args []string) error {
		a, ok := ctx.Audit.(enableDisabler)
		if !ok || a == nil {
			fmt.Println("%", ctx.Translator.T("audit_log.not_configured"))
			return nil
		}
		if err := a.Enable(); err != nil {
			return err
		}
		fmt.Println(ctx.Translator.T("audit_log.enabled"))
		return nil
	})

	command.Register("audit-log.disable", func(ctx *command.AppContext, args []string) error {
		a, ok := ctx.Audit.(enableDisabler)
		if !ok || a == nil {
			fmt.Println("%", ctx.Translator.T("audit_log.not_configured"))
			return nil
		}
		a.Disable()
		fmt.Println(ctx.Translator.T("audit_log.disabled"))
		return nil
	})

	command.Register("audit-log.status", func(ctx *command.AppContext, args []string) error {
		a, ok := ctx.Audit.(enableDisabler)
		if !ok || a == nil {
			fmt.Println(ctx.Translator.T("audit_log.not_configured_period"))
			return nil
		}
		if a.Enabled() {
			fmt.Println(ctx.Translator.T("audit_log.status_enabled"))
		} else {
			fmt.Println(ctx.Translator.T("audit_log.status_disabled"))
		}
		return nil
	})
}
