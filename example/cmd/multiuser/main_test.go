// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotermcli/routercli/config"
	"github.com/gotermcli/routercli/example/cmd/product"

	"github.com/gologme/log"
)

// discardTestLogger - This function returns a *log.Logger that
// buildDaemonState can call Debugln and friends on without needing a real
// destination, matching completer_test.go's testLogger pattern in spirit.
func discardTestLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// writeDaemonTestTree - This function writes a minimal, valid tree
// structure fixture into t.TempDir, one base Command Level, and returns
// the manifest and common tree file paths cfg.TreeStructure and
// cfg.CommonTreeFile need. When withHostnameCommand is true, the base
// level also gets a "hostname" entry, registered by cmd/product's init
// function, run against the exact same example/var/tree/level_config.yaml shape
// this project ships, so a test can replay a "hostname" line through
// buildDaemonState rather than only confirming an empty tree loads.
func writeDaemonTestTree(t *testing.T, withHostnameCommand bool) (manifest, common string) {
	t.Helper()
	dir := t.TempDir()

	treeBody := "commands: {}\n"
	if withHostnameCommand {
		treeBody = "commands:\n" +
			"  hostname:\n" +
			"    desc: \"Set the system hostname\"\n" +
			"    minargs: 1\n" +
			"    maxargs: 1\n" +
			"    run: hostname\n"
	}

	treeFile := filepath.Join(dir, "level_base.yaml")
	if err := os.WriteFile(treeFile, []byte(treeBody), 0640); err != nil {
		t.Fatalf("writing base tree file: %v", err)
	}

	commonFile := filepath.Join(dir, "level_common.yaml")
	if err := os.WriteFile(commonFile, []byte("commands: {}\n"), 0640); err != nil {
		t.Fatalf("writing common tree file: %v", err)
	}

	manifestFile := filepath.Join(dir, "tree_structure.yaml")
	manifestBody := "trees:\n  base:\n    tree_file: " + treeFile + "\n    is_base: true\n"
	if err := os.WriteFile(manifestFile, []byte(manifestBody), 0640); err != nil {
		t.Fatalf("writing tree structure manifest: %v", err)
	}

	return manifestFile, commonFile
}

// testDaemonConfig - This function returns a config.SystemConfig pointed
// entirely at fresh, empty locations under t.TempDir, so buildDaemonState
// has something complete and valid to load without borrowing anything from
// this repository's shipped etc/ or var/ files. cfg.RolesFile names a path
// that does not exist yet; command.LoadRoles treats a missing file as
// valid and empty, see its own doc comment, and AuthRequired stays false
// so no users file is read at all either. withHostnameCommand is forwarded
// to writeDaemonTestTree.
func testDaemonConfig(t *testing.T, withHostnameCommand bool) config.SystemConfig {
	t.Helper()
	manifest, common := writeDaemonTestTree(t, withHostnameCommand)
	dir := t.TempDir()

	cfg := config.DefaultSystemConfig()
	cfg.TreeStructure = manifest
	cfg.CommonTreeFile = common
	cfg.RolesFile = filepath.Join(dir, "roles.yaml")
	cfg.AuthRequired = false
	cfg.StartupConfigFile = filepath.Join(dir, "startup-config", "startup-config")
	return cfg
}

// TestBuildDaemonStateSucceedsAgainstAMinimalValidConfig - This test
// verifies that buildDaemonState returns a fully wired daemon.State given a
// valid tree structure and no roles, users, or startup-config file on disk.
//
// ProductState must come back as a usable, freshly zeroed *product.State,
// not merely a non-nil any a caller can do nothing with.
//
// This is the ordinary first run path rather than an edge case. LoadRoles
// and command.LoadStartupConfig both treat a missing file as valid and
// empty, which is exactly the state a new deployment is in.
func TestBuildDaemonStateSucceedsAgainstAMinimalValidConfig(t *testing.T) {
	cfg := testDaemonConfig(t, false)

	state, err := buildDaemonState(&cfg, discardTestLogger())
	if err != nil {
		t.Fatalf("buildDaemonState returned unexpected error: %v", err)
	}

	if state.Levels == nil {
		t.Fatal("expected state.Levels to be set, got nil")
	}
	if got := state.Levels.Base().Name; got != "base" {
		t.Errorf("state.Levels.Base().Name = %q, want %q", got, "base")
	}
	ps, ok := state.ProductState.(*product.State)
	if !ok || ps == nil {
		t.Fatalf("state.ProductState = %#v, want a non-nil *product.State", state.ProductState)
	}
	if ps.Hostname != "" {
		t.Errorf("freshly built product.State.Hostname = %q, want empty, nothing was replayed", ps.Hostname)
	}
	if state.Roles == nil {
		t.Fatal("expected state.Roles to be set, even with no roles.yaml on disk, got nil")
	}
	if len(state.Users) != 0 {
		t.Errorf("state.Users has %d entries, want 0, AuthRequired was false", len(state.Users))
	}
}

// TestBuildDaemonStateReplaysStartupConfigIntoProductState - This test
// verifies that buildDaemonState replays cfg.StartupConfigFile rather than
// merely loading the tree structure and returning an empty ProductState. It
// writes a "hostname" line and confirms the returned state carries it.
//
// The test above never writes a startup-config file, so a daemon that
// silently skipped the replay would still pass it. This is the one that
// exercises the replay.
func TestBuildDaemonStateReplaysStartupConfigIntoProductState(t *testing.T) {
	cfg := testDaemonConfig(t, true)
	if err := os.MkdirAll(filepath.Dir(cfg.StartupConfigFile), 0750); err != nil {
		t.Fatalf("creating startup-config directory: %v", err)
	}
	if err := os.WriteFile(cfg.StartupConfigFile, []byte("hostname router1\n"), 0640); err != nil {
		t.Fatalf("writing startup-config fixture: %v", err)
	}

	state, err := buildDaemonState(&cfg, discardTestLogger())
	if err != nil {
		t.Fatalf("buildDaemonState returned unexpected error: %v", err)
	}

	ps, ok := state.ProductState.(*product.State)
	if !ok || ps == nil {
		t.Fatalf("state.ProductState = %#v, want a non-nil *product.State", state.ProductState)
	}
	if ps.Hostname != "router1" {
		t.Errorf("product.State.Hostname = %q, want %q from the replayed startup-config", ps.Hostname, "router1")
	}
}

// TestBuildDaemonStateInvalidTreeStructureIsAnError - This test verifies
// that buildDaemonState reports an error, rather than a partially built
// daemon.State, when cfg.TreeStructure names a manifest LoadTreeStructure
// cannot parse at all, the same "fail loudly at startup rather than serve
// broken state" convention every other configuration loader in this
// project already follows.
func TestBuildDaemonStateInvalidTreeStructureIsAnError(t *testing.T) {
	cfg := testDaemonConfig(t, false)
	dir := t.TempDir()
	badManifest := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(badManifest, []byte("not: [valid"), 0640); err != nil {
		t.Fatalf("writing broken manifest fixture: %v", err)
	}
	cfg.TreeStructure = badManifest

	if _, err := buildDaemonState(&cfg, discardTestLogger()); err == nil {
		t.Error("buildDaemonState returned nil error for an unparseable tree structure manifest, want an error")
	}
}
