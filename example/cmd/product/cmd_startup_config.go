// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// init - This function registers "write memory" and
// "erase startup-config", both reachable only from admin.
//
// "write memory" is the one command that writes to disk. It does three
// things: it writes what "show running-config" prints to
// StartupConfigFile, it writes a VendorRestricted level's unredacted
// state to VendorConfigFile, and it saves every in memory change to the
// user database.
//
// Account changes, role changes, password changes, and TOTP enrollment
// all leave the users file untouched until this runs.
//
// Cisco and HP ship "write memory" alongside a separate
// "copy running-config startup-config" that covers only the first of
// those three. RouterCLI ships one command rather than two that almost,
// but not quite, do the same thing.
//
// "erase startup-config" removes StartupConfigFile alone, never
// VendorConfigFile. Erasing a file that does not exist is not an error,
// matching Cisco and HP.
//
// Neither command redacts anything itself. Whatever runningConfigLines
// decided to print is exactly what gets written.
func init() {
	command.Register("write.memory", func(ctx *command.AppContext, args []string) error {
		if err := writeRunningConfigToStartupConfig(ctx); err != nil {
			return err
		}
		if err := writeVendorConfig(ctx); err != nil {
			return err
		}
		if ctx.UsersFile != "" {
			if err := auth.SaveUsers(ctx.UsersFile, ctx.Users); err != nil {
				return fmt.Errorf("%s", ctx.Translator.T("write_memory.failed", err))
			}
			ctx.Logger.Debugln("DEBUG: users.yaml saved to", ctx.UsersFile)
		}
		fmt.Println(ctx.Translator.T("write_memory.confirm"))
		return nil
	})

	command.Register("erase.startup-config", func(ctx *command.AppContext, args []string) error {
		err := os.Remove(ctx.StartupConfigFile)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println(ctx.Translator.T("erase_startup_config.nothing_to_erase"))
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("erase_startup_config.failed", err))
		}

		ctx.Logger.Debugln("DEBUG: startup-config erased at", ctx.StartupConfigFile)
		fmt.Println(ctx.Translator.T("erase_startup_config.confirm"))
		return nil
	})
}

// writeVendorConfig - This function writes the unredacted state of every
// VendorRestricted level to VendorConfigFile, creating the parent
// directory on first save.
//
// It does nothing at all, not even creating an empty file, when there is
// nothing to write. That is the common case for a deployment that marks no
// level VendorRestricted, and leaving a stray empty file would serve
// nobody.
//
// An existing file is left alone in that case rather than cleared.
// Removing state a vendor's tooling may depend on is not this command's
// decision to make.
func writeVendorConfig(ctx *command.AppContext) error {
	lines := vendorConfigLines(ctx)
	if len(lines) == 0 {
		return nil
	}
	text := strings.Join(lines, "\n") + "\n"

	if dir := filepath.Dir(ctx.VendorConfigFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("startup_config.save_failed", err))
		}
	}
	if err := os.WriteFile(ctx.VendorConfigFile, []byte(text), 0640); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("startup_config.save_failed", err))
	}

	ctx.Logger.Debugln("DEBUG: vendor-restricted state saved to vendor-config at", ctx.VendorConfigFile)
	return nil
}

// writeRunningConfigToStartupConfig - This function writes exactly what
// "show running-config" prints to StartupConfigFile, creating the parent
// directory on first save.
//
// runningConfigLines already prepends "enable" when it has exec rooted
// content, so what lands on disk is self contained and replayable from
// base with nothing further needed here.
func writeRunningConfigToStartupConfig(ctx *command.AppContext) error {
	state := ctx.State.(*State)
	text := strings.Join(runningConfigLines(ctx, state), "\n") + "\n"

	if dir := filepath.Dir(ctx.StartupConfigFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("startup_config.save_failed", err))
		}
	}
	if err := os.WriteFile(ctx.StartupConfigFile, []byte(text), 0640); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("startup_config.save_failed", err))
	}

	ctx.Logger.Debugln("DEBUG: running-config saved to startup-config at", ctx.StartupConfigFile)
	return nil
}
