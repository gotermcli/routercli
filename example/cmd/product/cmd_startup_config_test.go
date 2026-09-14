// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// TestWriteMemoryWritesStartupConfigFile - This test verifies that "write
// memory" writes runningConfigLines' own output to ctx.StartupConfigFile,
// creating its parent directory along the way, since a fresh temporary
// directory here never already contains the "var/startup-config" style
// subdirectory a real deployment would.
func TestWriteMemoryWritesStartupConfigFile(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{}
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "var", "config", "startup-config")
	state := ctx.State.(*State)
	state.Hostname = "myrouter"
	state.Description = "a lab router"
	cmd := loadTestCommand(t, "write.memory")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	got, err := os.ReadFile(ctx.StartupConfigFile)
	if err != nil {
		t.Fatalf("expected startup-config file to exist after write memory, ReadFile returned: %v", err)
	}

	want := strings.Join(runningConfigLines(ctx, state), "\n") + "\n"
	if string(got) != want {
		t.Errorf("startup-config file content = %q, want %q", string(got), want)
	}
}

// TestWriteMemoryOverwritesExistingStartupConfigFile - This test verifies
// that running "write memory" a second time, after state has changed,
// replaces the file's content rather than appending to it, matching real
// Cisco and HP, both of which always fully replace startup-config on a
// save.
func TestWriteMemoryOverwritesExistingStartupConfigFile(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{}
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	state := ctx.State.(*State)
	state.Hostname = "first-name"
	cmd := loadTestCommand(t, "write.memory")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("first write.memory call returned unexpected error: %v", err)
	}

	state.Hostname = "second-name"
	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("second write.memory call returned unexpected error: %v", err)
	}

	got, err := os.ReadFile(ctx.StartupConfigFile)
	if err != nil {
		t.Fatalf("ReadFile returned unexpected error: %v", err)
	}
	if strings.Contains(string(got), "first-name") {
		t.Errorf("expected the first hostname to be gone after the second save, got:\n%s", string(got))
	}
	if !strings.Contains(string(got), "second-name") {
		t.Errorf("expected the second hostname to appear after the second save, got:\n%s", string(got))
	}
}

// TestWriteMemorySavesVendorConfigFileWhenALevelIsVendorRestricted - This
// test verifies that "write memory" also writes vendorConfigLines' own
// output, a VendorRestricted level's real, unredacted secret, to
// ctx.VendorConfigFile, a separate file from ctx.StartupConfigFile,
// creating its parent directory the same way.
func TestWriteMemorySavesVendorConfigFileWhenALevelIsVendorRestricted(t *testing.T) {
	const vendorHash = "$6$$super-secret-vendor-hash"
	levels := showRoundTripLevels(t)
	config := levels.ByName["config"]
	config.VendorDefinedPasswordHash = vendorHash
	config.Hidden = true
	config.VendorRestricted = true
	levels.Order = []*command.CommandLevel{levels.ByName["exec"], config}

	ctx := newTestContext()
	ctx.Levels = levels
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "var", "config", "startup-config")
	ctx.VendorConfigFile = filepath.Join(t.TempDir(), "var", "config", "vendor-config")
	cmd := loadTestCommand(t, "write.memory")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	gotVendor, err := os.ReadFile(ctx.VendorConfigFile)
	if err != nil {
		t.Fatalf("expected vendor-config file to exist after write memory, ReadFile returned: %v", err)
	}
	if !strings.Contains(string(gotVendor), vendorHash) {
		t.Errorf("expected the real vendor defined hash in vendor-config, got:\n%s", string(gotVendor))
	}

	gotStartup, err := os.ReadFile(ctx.StartupConfigFile)
	if err != nil {
		t.Fatalf("expected startup-config file to exist after write memory, ReadFile returned: %v", err)
	}
	if strings.Contains(string(gotStartup), vendorHash) {
		t.Errorf("expected the vendor defined hash to never appear in startup-config, got:\n%s", string(gotStartup))
	}
}

// TestWriteMemoryLeavesVendorConfigFileAloneWithoutAVendorRestrictedLevel
// - This test verifies that "write memory" never creates
// ctx.VendorConfigFile at all for a deployment that never marks any
// level VendorRestricted, matching writeVendorConfig's doc
// comment: no stray, empty vendor-config file for a project that does
// not use this feature.
func TestWriteMemoryLeavesVendorConfigFileAloneWithoutAVendorRestrictedLevel(t *testing.T) {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{}
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	ctx.VendorConfigFile = filepath.Join(t.TempDir(), "var", "config", "vendor-config")
	cmd := loadTestCommand(t, "write.memory")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	if _, err := os.Stat(ctx.VendorConfigFile); !os.IsNotExist(err) {
		t.Errorf("expected no vendor-config file to be created, stat returned: %v", err)
	}
}

// TestEraseStartupConfigLeavesVendorConfigFileAlone - This test verifies
// that "erase startup-config" removes only ctx.StartupConfigFile, never
// ctx.VendorConfigFile, matching this project's promise that a
// VendorRestricted level's state is never reachable by whatever puts a
// deployment back into a factory reset state.
func TestEraseStartupConfigLeavesVendorConfigFileAlone(t *testing.T) {
	ctx := newTestContext()
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	ctx.VendorConfigFile = filepath.Join(t.TempDir(), "vendor-config")
	if err := os.WriteFile(ctx.StartupConfigFile, []byte("! (example running-config)\n!\n"), 0640); err != nil {
		t.Fatalf("failed to seed startup-config file: %v", err)
	}
	if err := os.WriteFile(ctx.VendorConfigFile, []byte("! (vendor-config)\npassword manager hash $6$$vendorhash\n!\n"), 0640); err != nil {
		t.Fatalf("failed to seed vendor-config file: %v", err)
	}
	cmd := loadTestCommand(t, "erase.startup-config")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("erase.startup-config handler returned unexpected error: %v", err)
	}

	if _, err := os.Stat(ctx.StartupConfigFile); !os.IsNotExist(err) {
		t.Errorf("expected startup-config to be removed, stat returned: %v", err)
	}
	if _, err := os.Stat(ctx.VendorConfigFile); err != nil {
		t.Errorf("expected vendor-config to survive erase startup-config untouched, stat returned: %v", err)
	}
}

// TestWriteAloneIsIncompleteAndDoesNotSave - This test verifies that
// resolving bare "write", with no "memory" following it, against the exact
// "write" and "write memory" shape example/var/tree/level_admin.yaml declares
// never resolves to a runnable command, matching real Cisco and HP, both
// of which require the full "write memory" before anything is saved. A
// command.Command with no RunFunc of its own is never RunnableAsIs, see
// runnableAsIs's doc comment in command/command.go, so this is the same
// generic property TestResolveRunnableAsIsFalseForPureContainer in
// command/command_test.go already establishes, checked again here against
// the real, shipped shape "write" and "write memory" declare, so a future
// change to level_admin.yaml that accidentally added a "run:" directly
// onto "write" itself would be caught here.
func TestWriteAloneIsIncompleteAndDoesNotSave(t *testing.T) {
	tree := loadTestTree(t, `commands:
  write:
    subcommands:
      memory:
        run: write.memory
`)
	res := command.Resolve(tree, []string{"write"})

	if res.RunnableAsIs {
		t.Error("RunnableAsIs = true, want false: bare \"write\" must not run \"write memory\" on its own")
	}
	if res.Command == nil || res.Command.RunFunc != nil {
		t.Error("expected bare \"write\" to resolve to a pure container with no RunFunc of its own")
	}
}

// TestWriteMemoryResolvesToARunnableCommand - This test verifies that
// "write memory", the full two word command, resolves to a runnable
// command bound to the registered "write.memory" handler, against the same
// tree shape as the test above.
func TestWriteMemoryResolvesToARunnableCommand(t *testing.T) {
	tree := loadTestTree(t, `commands:
  write:
    subcommands:
      memory:
        run: write.memory
`)
	res := command.Resolve(tree, []string{"write", "memory"})

	if !res.RunnableAsIs {
		t.Error("RunnableAsIs = false, want true: \"write memory\" is a complete command")
	}
	if res.Command == nil || res.Command.RunFunc == nil {
		t.Fatal("expected \"write memory\" to resolve to a runnable command")
	}

	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{}
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	if err := res.Command.RunFunc(ctx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}
	if _, err := os.Stat(ctx.StartupConfigFile); err != nil {
		t.Errorf("expected startup-config file to exist after \"write memory\", os.Stat returned: %v", err)
	}
}

// TestEraseStartupConfigRemovesExistingFile - This test verifies that
// "erase startup-config" deletes an existing file and reports the
// "confirm" outcome, not the "nothing to erase" one.
func TestEraseStartupConfigRemovesExistingFile(t *testing.T) {
	ctx := newTestContext()
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	if err := os.WriteFile(ctx.StartupConfigFile, []byte("! (example running-config)\n!\n"), 0640); err != nil {
		t.Fatalf("failed to seed startup-config file: %v", err)
	}
	cmd := loadTestCommand(t, "erase.startup-config")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("erase.startup-config handler returned unexpected error: %v", runErr)
	}
	if !strings.Contains(out, "erase_startup_config.confirm") {
		t.Errorf("expected the \"confirm\" outcome, got: %q", out)
	}
	if _, err := os.Stat(ctx.StartupConfigFile); !os.IsNotExist(err) {
		t.Errorf("expected the startup-config file to be gone after erase, os.Stat returned: %v", err)
	}
}

// TestEraseStartupConfigNonexistentFileIsNotError - This test verifies
// that erasing a startup-config file that was never saved in the first
// place is not treated as an error, the same forgiving behavior real Cisco
// and HP devices give when "erase startup-config" runs again on an already
// clean device.
func TestEraseStartupConfigNonexistentFileIsNotError(t *testing.T) {
	ctx := newTestContext()
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "never-saved-startup-config")
	cmd := loadTestCommand(t, "erase.startup-config")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("erase.startup-config handler returned unexpected error for a nonexistent file: %v", runErr)
	}
	if !strings.Contains(out, "erase_startup_config.nothing_to_erase") {
		t.Errorf("expected the \"nothing to erase\" outcome, got: %q", out)
	}
}

// ----------------------------------------------------------------------
//
// write memory, execEnterWords prepending, and the full cold boot
// round trip, base and user Command Levels included
//
// ----------------------------------------------------------------------

// TestWriteMemoryPrependsExecEnterWords - This test verifies the one
// difference between what "write memory" writes and runningConfigLines'
// own raw output for a bare exec-rooted state with no base or user
// content: when ctx.Levels resolves a real "exec" level with a real
// Parent, "enable" is prepended as its own leading line, ahead of
// everything else runningConfigLines itself produces. This now happens
// inside runningConfigLines itself, not as a separate step
// writeRunningConfigToStartupConfig takes, see that function's doc
// comment. showRoundTripLevels, cmd_show_test.go, only ever builds exec,
// config, and config-if, with no Parent at all on exec, so this test
// builds its own small three level tree, base and exec, with exec.Parent
// set to base and exec.EnterCommand set to "enable", to exercise the
// prepending path TestWriteMemoryWritesStartupConfigFile above does not,
// since that test's empty ctx.Levels leaves execEnterWords with nothing to
// resolve.
func TestWriteMemoryPrependsExecEnterWords(t *testing.T) {
	baseTree := loadTestTree(t, `commands:
  enable:
    run: enable
`)
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{ByName: map[string]*command.CommandLevel{
		"base": {Name: "base", Tree: baseTree},
		"exec": {Name: "exec", Parent: "base", EnterCommand: "enable"},
	}}
	ctx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	state := ctx.State.(*State)
	state.Hostname = "myrouter"
	cmd := loadTestCommand(t, "write.memory")

	if err := cmd.RunFunc(ctx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	got, err := os.ReadFile(ctx.StartupConfigFile)
	if err != nil {
		t.Fatalf("ReadFile returned unexpected error: %v", err)
	}

	want := strings.Join(runningConfigLines(ctx, state), "\n") + "\n"
	if string(got) != want {
		t.Errorf("startup-config file content = %q, want %q", string(got), want)
	}
	if !strings.HasPrefix(string(got), "! (example running-config)\nenable\n") {
		t.Errorf("expected \"enable\" right after the header comment, got:\n%s", string(got))
	}
}

// startupConfigRoundTripLevels - This function builds on
// showRoundTripLevels, cmd_show_test.go, adding a fourth level, base,
// beneath exec, with exec.Parent set to base and exec.EnterCommand set to
// "enable", the one piece showRoundTripLevels itself leaves out since
// ordinary "show running-config" replay never needs it. base's tree
// carries a real "enable" command, run: enable, resolved through
// cmd/core's registered handler, this file's blank import of that package
// by way of cmd_show_test.go, so
// TestStartupConfigReplaysFromAColdBootWithNobodyHavingTypedEnable below
// exercises the exact same command.EnterCommandLevel path a real "enable"
// typed by hand would.
func startupConfigRoundTripLevels(t *testing.T) *command.TreeStructure {
	levels := showRoundTripLevels(t)
	baseTree := loadTestTree(t, `commands:
  enable:
    run: enable
`)
	levels.ByName["base"] = &command.CommandLevel{Name: "base", Tree: baseTree}
	exec := levels.ByName["exec"]
	exec.Parent = "base"
	exec.EnterCommand = "enable"
	return levels
}

// TestStartupConfigReplaysFromAColdBootWithNobodyHavingTypedEnable - This
// test is the actual promise the user asked for: a saved startup-config
// file, written by a real session sitting at exec, replays cleanly through
// command.ReplayLines against a second, completely fresh AppContext
// starting cold at base, the same Command Level a freshly started process,
// before login, before "enable", before anything else, begins at, landing
// on a State equal to the one that produced it, and a session sitting back
// at exec once replay finishes, not still stuck inside config. This is
// command.LoadStartupConfig, minus the file read and paging.CaptureOutput
// wrapper, both of which are exercised directly by TestMain, since neither
// one has anything to do with whether the saved text itself resolves
// correctly starting from base.
func TestStartupConfigReplaysFromAColdBootWithNobodyHavingTypedEnable(t *testing.T) {
	levels := startupConfigRoundTripLevels(t)

	saveCtx := newTestContext()
	saveCtx.Levels = levels
	saveCtx.Position = command.NewCommandLevelStack("exec", "", levels.ByName["exec"].Tree)
	saveCtx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	state := saveCtx.State.(*State)
	state.Hostname = "myrouter"
	state.Description = "a lab router"
	state.Interface("eth0").Description = "uplink"
	state.Interface("eth1").Shutdown = true
	cmd := loadTestCommand(t, "write.memory")
	if err := cmd.RunFunc(saveCtx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	saved, err := os.ReadFile(saveCtx.StartupConfigFile)
	if err != nil {
		t.Fatalf("ReadFile returned unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(saved), "\n"), "\n")
	if lines[1] != "enable" {
		t.Fatalf("expected the saved file's second line, right after the header comment, to be \"enable\", got %q", lines[1])
	}

	replayCtx := newTestContext()
	replayCtx.Levels = levels
	replayCtx.Position = command.NewCommandLevelStack("base", "", levels.ByName["base"].Tree)
	replayCtx.Session = &auth.Session{CommandLevel: "base"}
	if err := command.ReplayLines(replayCtx, lines, true); err != nil {
		t.Fatalf("command.ReplayLines returned unexpected error: %v", err)
	}
	replayState := replayCtx.State.(*State)

	if replayState.Hostname != state.Hostname {
		t.Errorf("replayed Hostname = %q, want %q", replayState.Hostname, state.Hostname)
	}
	if replayState.Description != state.Description {
		t.Errorf("replayed Description = %q, want %q", replayState.Description, state.Description)
	}
	if len(replayState.Interfaces) != len(state.Interfaces) {
		t.Fatalf("replayed Interfaces has %d entries, want %d", len(replayState.Interfaces), len(state.Interfaces))
	}
	for name, iface := range state.Interfaces {
		got, ok := replayState.Interfaces[name]
		if !ok {
			t.Fatalf("replayed state is missing interface %q", name)
		}
		if got.Description != iface.Description || got.Shutdown != iface.Shutdown {
			t.Errorf("replayed interface %q = %+v, want %+v", name, got, iface)
		}
	}
	if replayCtx.Position.Current().Name != "exec" {
		t.Errorf("expected the replayed session to land back at exec (from \"end\"), got %q", replayCtx.Position.Current().Name)
	}
	if replayCtx.ReplayingStartupConfig {
		t.Error("expected ReplayingStartupConfig to be false again once ReplayLines returns")
	}
}

// TestStartupConfigReplaysBaseAndUserAliasesWithNobodyLoggedIn - This test
// verifies the piece
// TestStartupConfigReplaysFromAColdBootWithNobodyHavingTypedEnable above
// does not cover: a runtime defined command alias belonging to base or
// user, both reached without ever running "enable", replays back in
// correctly during a cold boot, even though replayCtx.Session here is nil,
// nobody has logged in at all, the exact case example/cmd/core/cmd_user.go's
// "user" handler waives its ordinary login requirement for, through
// command.AppContext.ReplayingStartupConfig, see that field's doc comment
// in command/model.go.
func TestStartupConfigReplaysBaseAndUserAliasesWithNobodyLoggedIn(t *testing.T) {
	levels := showRoundTripLevels(t)

	baseTree := loadTestTree(t, `commands:
  enable:
    run: enable
  user:
    run: user
  alias:
    minargs: 2
    negatable: true
    run: alias
`)
	levels.ByName["base"] = &command.CommandLevel{
		Name:    "base",
		Tree:    baseTree,
		Aliases: map[string][]string{"b1": {"enable"}},
	}
	userTree := loadTestTree(t, `commands:
  alias:
    minargs: 2
    negatable: true
    run: alias
  end:
    run: end
`)
	levels.ByName["user"] = &command.CommandLevel{
		Name:         "user",
		Parent:       "base",
		EnterCommand: "user",
		Tree:         userTree,
		Aliases: map[string][]string{
			"u1": {"show", "version"},
		},
	}
	exec := levels.ByName["exec"]
	exec.Parent = "base"
	exec.EnterCommand = "enable"

	saveCtx := newTestContext()
	saveCtx.Levels = levels
	saveCtx.Position = command.NewCommandLevelStack("exec", "", levels.ByName["exec"].Tree)
	saveCtx.StartupConfigFile = filepath.Join(t.TempDir(), "startup-config")
	cmd := loadTestCommand(t, "write.memory")
	if err := cmd.RunFunc(saveCtx, nil); err != nil {
		t.Fatalf("write.memory handler returned unexpected error: %v", err)
	}

	saved, err := os.ReadFile(saveCtx.StartupConfigFile)
	if err != nil {
		t.Fatalf("ReadFile returned unexpected error: %v", err)
	}
	text := string(saved)
	if !strings.Contains(text, "alias b1 enable") {
		t.Errorf("expected the base alias to render unwrapped, got:\n%s", text)
	}
	if !strings.Contains(text, "user\nalias u1 show version\nend") {
		t.Errorf("expected the user alias wrapped in \"user\" ... \"end\", got:\n%s", text)
	}

	// Cleared before replay, same reasoning
	// TestShowRunningConfigOutputReplaysBackToTheSameState follows in
	// cmd_show_test.go: this proves the rendered text alone reconstructs
	// both aliases, not that they were still sitting there from the save
	// above, levels being the same *command.TreeStructure both saveCtx and
	// replayCtx point at.
	levels.ByName["base"].Aliases = nil
	levels.ByName["user"].Aliases = nil

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	replayCtx := newTestContext()
	replayCtx.Levels = levels
	replayCtx.Position = command.NewCommandLevelStack("base", "", levels.ByName["base"].Tree)
	// Deliberately nil: this is the whole point of the test, a cold boot
	// replay has no session at all yet.
	replayCtx.Session = nil
	// rewireDaemonClient shares this exact TreeStructure with
	// ctx.DaemonClient's Store, so cmd/core's "alias" handler, replayed
	// below by way of this file's blank import of that package, reaches
	// the same ctx.Levels this test asserts against afterward through its
	// own ctx.DaemonClient.MutateLevels call. See rewireDaemonClient's doc
	// comment in testhelpers_test.go.
	rewireDaemonClient(replayCtx)
	if err := command.ReplayLines(replayCtx, lines, true); err != nil {
		t.Fatalf("command.ReplayLines returned unexpected error with nobody logged in: %v", err)
	}

	if got := levels.ByName["base"].Aliases["b1"]; strings.Join(got, " ") != "enable" {
		t.Errorf("replayed base alias %q, want %q", got, "enable")
	}
	if got := levels.ByName["user"].Aliases["u1"]; strings.Join(got, " ") != "show version" {
		t.Errorf("replayed user alias %q, want %q", got, "show version")
	}
}
