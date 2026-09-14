// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package main

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"time"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/daemon"
	"github.com/gotermcli/routercli/example/cmd/product"
	"github.com/gotermcli/routercli/internal/clitest"
	"github.com/gotermcli/routercli/paging"
)

// TestPreventEscapeIgnoresEscapeSignals - This test sends the process real
// SIGINT, SIGTSTP, SIGQUIT, and SIGTERM signals through
// syscall.Kill(self, ...) after calling preventEscape, and checks that the
// test is still running afterward. If any of these signals were not
// ignored at the OS level, this test process would be killed or suspended,
// and the test runner itself would hang or report a failure, not a clean
// assertion failure. That is intended. This is testing an OS level
// guarantee, SIG_IGN, not application logic, so the test suite still
// running is the only meaningful signal that it worked.
//
// This mutates process-wide signal disposition, which is safe here since
// each "go test" invocation is its own throwaway process. It does not
// affect anything outside this test run.
func TestPreventEscapeIgnoresEscapeSignals(t *testing.T) {
	preventEscape()

	signals := []syscall.Signal{
		syscall.SIGINT,
		syscall.SIGTSTP,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	}

	pid := os.Getpid()
	for _, sig := range signals {
		if err := syscall.Kill(pid, sig); err != nil {
			t.Fatalf("failed to send %v to self: %v", sig, err)
		}
	}

	// Give the OS a moment to deliver the signals before declaring
	// victory, since delivery is asynchronous.
	time.Sleep(100 * time.Millisecond)

	// Reaching this line at all is the assertion. If any of the four
	// signals above were not ignored, this goroutine, and the whole test
	// process, would already be dead or suspended.
	t.Log("process survived SIGINT/SIGTSTP/SIGQUIT/SIGTERM after preventEscape()")
}

// ----------------------------------------------------------------------
//
// attachPasswordRateLimiters
//
// ----------------------------------------------------------------------

// TestAttachPasswordRateLimitersOnlySetsLimiterForPasswordProtectedCommands
// - This test verifies that attachPasswordRateLimiters gives a working
// auth.RateLimiter to every command with a PasswordHash set, and leaves an
// unprotected command's PasswordRateLimiter nil. Allow and RecordFailure
// are exercised directly, not just a non-nil check, since a limiter that
// exists but was constructed with the wrong maxAttempts would still pass a
// non-nil check while doing nothing useful.
func TestAttachPasswordRateLimitersOnlySetsLimiterForPasswordProtectedCommands(t *testing.T) {
	protected := &command.Command{PasswordHash: "$0$secret"}
	unprotected := &command.Command{}
	levels := &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec", Tree: map[string]*command.Command{"secret-thing": protected, "plain-thing": unprotected}},
		},
	}

	attachPasswordRateLimiters(levels, 1, time.Minute, 5*time.Minute)

	if unprotected.PasswordRateLimiter != nil {
		t.Error("expected an unprotected command's PasswordRateLimiter to stay nil")
	}
	if protected.PasswordRateLimiter == nil {
		t.Fatal("expected a password-protected command to get a PasswordRateLimiter")
	}

	if ok, _ := protected.PasswordRateLimiter.Allow(); !ok {
		t.Fatal("expected a freshly attached RateLimiter to allow immediately")
	}
	protected.PasswordRateLimiter.RecordFailure() // maxAttempts is 1, so this locks it out
	if ok, _ := protected.PasswordRateLimiter.Allow(); ok {
		t.Error("expected the attached RateLimiter to actually enforce maxAttempts=1, not just exist")
	}
}

// TestAttachPasswordRateLimitersCoversVendorDefinedPasswordHashToo - This
// test verifies that a command whose only secret is a
// VendorDefinedPasswordHash, no ordinary PasswordHash at all, still gets a
// real RateLimiter attached, the same as an ordinary password-protected
// command does. Without EffectivePasswordHash being what this function
// checks, a vendor defined command would go completely unprotected against
// repeated guesses.
func TestAttachPasswordRateLimitersCoversVendorDefinedPasswordHashToo(t *testing.T) {
	vendorProtected := &command.Command{VendorDefinedPasswordHash: "$6$$vendorhash"}
	levels := &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec", Tree: map[string]*command.Command{"vendor-thing": vendorProtected}},
		},
	}

	attachPasswordRateLimiters(levels, 1, time.Minute, 5*time.Minute)

	if vendorProtected.PasswordRateLimiter == nil {
		t.Fatal("expected a command gated only by VendorDefinedPasswordHash to still get a PasswordRateLimiter")
	}
}

// TestAttachPasswordRateLimitersMaxAttemptsZeroLeavesRateLimitingDisabled
// - This test verifies that a configured maxAttempts of zero still gets
// every password-protected command a real RateLimiter, attachment is
// unconditional, see this function's doc comment, but that RateLimiter
// never locks anything out, matching auth.RateLimiter's "maxAttempts at or
// below zero disables" convention. This confirms that convention holds all
// the way through this wiring path, not only when auth.RateLimiter is used
// directly.
func TestAttachPasswordRateLimitersMaxAttemptsZeroLeavesRateLimitingDisabled(t *testing.T) {
	protected := &command.Command{PasswordHash: "$0$secret"}
	levels := &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec", Tree: map[string]*command.Command{"secret-thing": protected}},
		},
	}

	attachPasswordRateLimiters(levels, 0, time.Minute, 5*time.Minute)

	if protected.PasswordRateLimiter == nil {
		t.Fatal("expected a RateLimiter to be attached even when maxAttempts is 0")
	}
	for i := 0; i < 10; i++ {
		protected.PasswordRateLimiter.RecordFailure()
	}
	if ok, _ := protected.PasswordRateLimiter.Allow(); !ok {
		t.Error("expected Allow to stay true regardless of recorded failures when maxAttempts is 0")
	}
}

// TestAttachPasswordRateLimitersDoesNotRecurseForeverOnASharedCommand
// -This test verifies the actual mechanism visited exists for: a command
// reached more than once through the walk is never processed a second
// time. Ordinary YAML-loaded trees are acyclic, so this cannot happen from
// a real tree file, but a hand-built Go tree, or a future change to how
// InheritParent shares commands across levels, could produce a command
// that is reachable from itself. Without visited, walk would recurse into
// that command's Subcommands forever.
//
// It runs on a background goroutine with a short timeout, so a failure
// reports as a failure rather than hanging the whole suite.
func TestAttachPasswordRateLimitersDoesNotRecurseForeverOnASharedCommand(t *testing.T) {
	shared := &command.Command{PasswordHash: "$0$secret"}
	shared.Subcommands = map[string]*command.Command{"self": shared}
	levels := &command.TreeStructure{
		Order: []*command.CommandLevel{
			{Name: "exec", Tree: map[string]*command.Command{"root": shared}},
		},
	}

	done := make(chan struct{})
	go func() {
		attachPasswordRateLimiters(levels, 1, time.Minute, 5*time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("attachPasswordRateLimiters did not return, a self-referencing command likely caused infinite recursion")
	}

	if shared.PasswordRateLimiter == nil {
		t.Error("expected the shared, self-referencing command to still get a PasswordRateLimiter attached")
	}
}

// ----------------------------------------------------------------------
//
// buildPrompt
//
// ----------------------------------------------------------------------

// baseExecLevelsForPrompt - This function builds a minimal
// *command.TreeStructure with a "base" level, marked IsBase, and an "exec"
// level, the shape buildPrompt needs from ctx.Levels.Base() to decide
// whether a session is away from base.
func baseExecLevelsForPrompt() *command.TreeStructure {
	base := &command.CommandLevel{Name: "base", IsBase: true}
	exec := &command.CommandLevel{Name: "exec", Parent: "base"}
	return &command.TreeStructure{Order: []*command.CommandLevel{base, exec}}
}

// TestBuildPromptAtBaseUsesUnprivilegedMarker - This test verifies that a
// session at the base level, root frame, depth 1, gets the "> "
// unprivileged marker, and the default hostname fallback, since
// State.Hostname is still empty.
func TestBuildPromptAtBaseUsesUnprivilegedMarker(t *testing.T) {
	ctx := &command.AppContext{
		State:    &product.State{},
		Session:  &auth.Session{CommandLevel: "base"},
		Levels:   baseExecLevelsForPrompt(),
		Position: command.NewCommandLevelStack("base", "", nil),
	}
	got := buildPrompt(ctx)
	want := defaultHostnamePrompt + "> "
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}

// TestBuildPromptAwayFromBaseUsesPrivilegedMarker - This test verifies
// that a session whose Session.CommandLevel is not the base level's Name,
// "exec" here, gets the "# " privileged marker, even though Position
// itself is still only one frame deep.
func TestBuildPromptAwayFromBaseUsesPrivilegedMarker(t *testing.T) {
	ctx := &command.AppContext{
		State:    &product.State{},
		Session:  &auth.Session{CommandLevel: "exec"},
		Levels:   baseExecLevelsForPrompt(),
		Position: command.NewCommandLevelStack("exec", "", nil),
	}
	got := buildPrompt(ctx)
	want := defaultHostnamePrompt + "# "
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}

// TestBuildPromptNestedFrameUsesPrivilegedMarkerEvenAtBase - This test
// verifies that a session still at the base Session.CommandLevel, but with
// a pushed, nested CommandLevelStack frame such as config, also gets the
// "# " marker, through the ctx.Position.Depth() > 1 half of buildPrompt's
// condition, independent of Session.CommandLevel entirely.
func TestBuildPromptNestedFrameUsesPrivilegedMarkerEvenAtBase(t *testing.T) {
	ctx := &command.AppContext{
		State:    &product.State{},
		Session:  &auth.Session{CommandLevel: "base"},
		Levels:   baseExecLevelsForPrompt(),
		Position: command.NewCommandLevelStack("base", "", nil),
	}
	ctx.Position.Push(command.CommandLevelFrame{Name: "config", PromptSuffix: "(config)"})

	got := buildPrompt(ctx)
	want := defaultHostnamePrompt + "(config)" + "# "
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}

// TestBuildPromptUsesConfiguredHostnameOnceSet - This test verifies that
// once product.State.Hostname is non-empty, buildPrompt shows it instead of
// defaultHostnamePrompt, and reads it live rather than a value captured at
// some earlier point.
func TestBuildPromptUsesConfiguredHostnameOnceSet(t *testing.T) {
	state := &product.State{Hostname: "myrouter"}
	ctx := &command.AppContext{
		State:    state,
		Session:  &auth.Session{CommandLevel: "base"},
		Levels:   baseExecLevelsForPrompt(),
		Position: command.NewCommandLevelStack("base", "", nil),
	}
	got := buildPrompt(ctx)
	want := "myrouter> "
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}

// TestBuildPromptToleratesNilSessionOrLevels - This test verifies that
// buildPrompt does not panic when Session or Levels is nil, the state a
// hand-built test context that never wired them up is in, falling back to
// the unprivileged marker rather than attempting the AtLevel check at all.
func TestBuildPromptToleratesNilSessionOrLevels(t *testing.T) {
	ctx := &command.AppContext{
		State:    &product.State{},
		Position: command.NewCommandLevelStack("base", "", nil),
	}
	got := buildPrompt(ctx)
	want := defaultHostnamePrompt + "> "
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------
//
// historyFilePath, mkdirForFile
//
// ----------------------------------------------------------------------

// TestHistoryFilePathUnderUsersHomeDirectory - This test verifies that
// historyFilePath returns a path under the real current user's home
// directory, ".routercli_history", the fallback used when the
// configuration file does not set HistoryFile explicitly.
func TestHistoryFilePathUnderUsersHomeDirectory(t *testing.T) {
	got := historyFilePath()
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		if got != ".routercli_history" {
			t.Errorf("historyFilePath() = %q, want %q when the current user's home directory cannot be determined", got, ".routercli_history")
		}
		return
	}
	want := filepath.Join(u.HomeDir, ".routercli_history")
	if got != want {
		t.Errorf("historyFilePath() = %q, want %q", got, want)
	}
}

// TestMkdirForFileCreatesMissingParentDirectory - This test verifies that
// mkdirForFile creates path's parent directory, including any missing
// intermediate directories, when it does not already exist.
func TestMkdirForFileCreatesMissingParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	target := filepath.Join(dir, "audit.log")

	if err := mkdirForFile(target); err != nil {
		t.Fatalf("mkdirForFile returned unexpected error: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected %q to exist after mkdirForFile, stat error: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", dir)
	}
}

// TestMkdirForFileNoOpForBarePathWithNoDirectory - This test verifies that
// mkdirForFile does nothing, and returns no error, for a bare filename
// with no directory component, filepath.Dir returning "." rather than
// something to create.
func TestMkdirForFileNoOpForBarePathWithNoDirectory(t *testing.T) {
	if err := mkdirForFile("audit.log"); err != nil {
		t.Errorf("mkdirForFile(\"audit.log\") returned unexpected error: %v", err)
	}
}

// ----------------------------------------------------------------------
//
// command.LoadStartupConfig
//
// ----------------------------------------------------------------------

// writeMainTestTree - This function resolves yamlBody into a real
// map[string]*command.Command by writing it to a throwaway tree file and
// loading it through command.LoadTree, the same loader main.go itself uses
// at startup, mirroring cmd/product's loadTestTree.
func writeMainTestTree(t *testing.T, yamlBody string) map[string]*command.Command {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tree.yaml")
	if err := os.WriteFile(path, []byte(yamlBody), 0644); err != nil {
		t.Fatalf("failed to write test tree file: %v", err)
	}
	tree, err := command.LoadTree(path)
	if err != nil {
		t.Fatalf("LoadTree returned error: %v", err)
	}
	return tree
}

// startupConfigLevelsForLoad - This function builds a real, working two
// level Command Level Tree, base and exec, loaded through command.LoadTree
// the same way LoadTreeStructure itself does, rather than the bare,
// Tree-less levels baseExecLevelsForPrompt above builds for buildPrompt's
// narrower needs. base's tree carries a real "enable" command, resolved
// through cmd/core's registered handler, this file's blank import of that
// package, and exec's tree carries a real "hostname" command, resolved
// through cmd/product's, so
// TestLoadStartupConfigAppliesSavedConfigurationAndResetsPosition below
// exercises command.LoadStartupConfig, and the command.ReplayLines call
// inside it, the same way main() itself does, not a stand-in for either
// one.
func startupConfigLevelsForLoad(t *testing.T) *command.TreeStructure {
	t.Helper()
	baseTree := writeMainTestTree(t, "commands:\n  enable:\n    run: enable\n")
	execTree := writeMainTestTree(t, "commands:\n  hostname:\n    minargs: 1\n    maxargs: 1\n    run: hostname\n")
	base := &command.CommandLevel{Name: "base", IsBase: true, Tree: baseTree}
	exec := &command.CommandLevel{Name: "exec", Parent: "base", EnterCommand: "enable", Tree: execTree}
	return &command.TreeStructure{
		Order:  []*command.CommandLevel{base, exec},
		ByName: map[string]*command.CommandLevel{"base": base, "exec": exec},
	}
}

// TestLoadStartupConfigMissingFileIsNotAnError - This test verifies that
// command.LoadStartupConfig treats a path that does not exist yet, the
// state of a brand new deployment that has never run "copy running-config
// startup-config", as a no-op, not an error, and never even touches
// ctx.Levels or ctx.Position along the way, since the function returns
// before ever reaching either one.
func TestLoadStartupConfigMissingFileIsNotAnError(t *testing.T) {
	ctx := &command.AppContext{
		State:  &product.State{},
		Logger: log.New(io.Discard, "", 0),
	}
	path := filepath.Join(t.TempDir(), "never-saved-startup-config")

	if err := command.LoadStartupConfig(ctx, path); err != nil {
		t.Errorf("command.LoadStartupConfig returned unexpected error for a nonexistent file: %v", err)
	}
}

// TestLoadStartupConfigAppliesSavedConfigurationAndResetsPosition - This
// test is the actual proof the whole feature works end to end through
// main.go's function, not only through command.ReplayLines directly,
// cmd/product's
// TestStartupConfigReplaysFromAColdBootWithNobodyHavingTypedEnable. It
// writes a real saved startup-config file, "enable" followed by a
// "hostname" line, calls command.LoadStartupConfig against a fresh ctx
// sitting at base, and confirms the hostname was applied to ctx.State,
// that ctx.ReplayingStartupConfig is false again once it returns, matching
// the trust window's narrow, boot only scope, and that ctx.Position and
// ctx.Session.CommandLevel both land back at base, not left wherever
// replaying "enable" moved them.
func TestLoadStartupConfigAppliesSavedConfigurationAndResetsPosition(t *testing.T) {
	levels := startupConfigLevelsForLoad(t)
	state := &product.State{}
	ctx := &command.AppContext{
		State: state,
		// DaemonClient wraps the exact same *product.State pointer State
		// itself holds, the same pattern main.go's real startup wiring
		// uses, see the productState/daemonClient paragraph above ctx's
		// construction there, so replaying "hostname myrouter" below,
		// which now reaches ctx.DaemonClient.MutateProductState rather
		// than State directly, see example/cmd/product/cmd_hostname.go, mutates
		// this identical object rather than panicking against a nil
		// DaemonClient.
		DaemonClient: daemon.NewStandaloneClient(daemon.NewState(state, nil, nil, nil, nil)),
		Session:      &auth.Session{CommandLevel: "base"},
		Levels:       levels,
		Logger:       log.New(io.Discard, "", 0),
		Position:     command.NewCommandLevelStack("base", "", levels.ByName["base"].Tree),
	}
	path := filepath.Join(t.TempDir(), "startup-config")
	if err := os.WriteFile(path, []byte("enable\nhostname myrouter\n"), 0640); err != nil {
		t.Fatalf("failed to seed startup-config file: %v", err)
	}

	if err := command.LoadStartupConfig(ctx, path); err != nil {
		t.Fatalf("command.LoadStartupConfig returned unexpected error: %v", err)
	}

	if state.Hostname != "myrouter" {
		t.Errorf("replayed Hostname = %q, want %q", state.Hostname, "myrouter")
	}
	if ctx.ReplayingStartupConfig {
		t.Error("expected ReplayingStartupConfig to be false again once command.LoadStartupConfig returns")
	}
	if ctx.Position.Current().Name != "base" {
		t.Errorf("expected ctx.Position to be reset back to base, got %q", ctx.Position.Current().Name)
	}
	if ctx.Session.CommandLevel != "base" {
		t.Errorf("expected ctx.Session.CommandLevel to be reset back to base, got %q", ctx.Session.CommandLevel)
	}
}

// ----------------------------------------------------------------------
//
// warnPlaintextUserSecrets, sortedUserNames, warnPlaintextLevelSecrets
//
// ----------------------------------------------------------------------

// captureLogOutput - This function returns a *log.Logger with the "warn"
// level enabled, writing into an in-memory buffer, plus a function to read
// back everything written so far. gologme/log gates every level behind an
// explicit EnableLevel call, unlike the standard library's log package, so
// a freshly constructed logger with no level enabled would silently
// swallow every Warnln call.
func captureLogOutput() (*log.Logger, func() string) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logger.EnableLevel("warn")
	return logger, buf.String
}

// TestWarnPlaintextUserSecretsWarnsOnlyForPlaintextUsers - This test
// verifies that warnPlaintextUserSecrets logs a warning naming a user
// whose PasswordHash is stored in the plaintext "$0$..." form, and says
// nothing at all for a user with a real bcrypt hash.
func TestWarnPlaintextUserSecretsWarnsOnlyForPlaintextUsers(t *testing.T) {
	logger, output := captureLogOutput()
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	users := auth.Users{
		"alice": {Username: "alice", PasswordHash: "$0$plaintext"},
		"bob":   {Username: "bob", PasswordHash: hash},
	}

	warnPlaintextUserSecrets(logger, users)

	got := output()
	if !strings.Contains(got, "alice") {
		t.Errorf("expected a warning naming \"alice\", got %q", got)
	}
	if strings.Contains(got, "bob") {
		t.Errorf("expected no warning for \"bob\", who has a real bcrypt hash, got %q", got)
	}
}

// TestWarnPlaintextUserSecretsSilentWhenNonePlaintext - This test verifies
// that warnPlaintextUserSecrets logs nothing at all when every user has a
// real bcrypt hash.
func TestWarnPlaintextUserSecretsSilentWhenNonePlaintext(t *testing.T) {
	logger, output := captureLogOutput()
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	users := auth.Users{"alice": {Username: "alice", PasswordHash: hash}}

	warnPlaintextUserSecrets(logger, users)

	if got := output(); got != "" {
		t.Errorf("expected no warning output, got %q", got)
	}
}

// TestSortedUserNamesReturnsNamesInSortedOrder - This test verifies that
// sortedUserNames returns every username sorted alphabetically, regardless
// of Go's randomized map iteration order, so warnPlaintextUserSecrets's
// output is stable between runs.
func TestSortedUserNamesReturnsNamesInSortedOrder(t *testing.T) {
	users := auth.Users{
		"charlie": {Username: "charlie"},
		"alice":   {Username: "alice"},
		"bob":     {Username: "bob"},
	}
	got := sortedUserNames(users)
	want := []string{"alice", "bob", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("sortedUserNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedUserNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSortedUserNamesEmptyForEmptyUsers - This test verifies that
// sortedUserNames returns an empty, non-nil slice for an empty user
// database, rather than nil, so a caller ranging over it never needs its
// own nil check.
func TestSortedUserNamesEmptyForEmptyUsers(t *testing.T) {
	got := sortedUserNames(auth.Users{})
	if len(got) != 0 {
		t.Errorf("sortedUserNames(empty) = %v, want an empty slice", got)
	}
}

// TestWarnPlaintextLevelSecretsWarnsOnlyForPlaintextLevels - This test
// verifies the Tree Structure counterpart to
// TestWarnPlaintextUserSecretsWarnsOnlyForPlaintextUsers: a Command Level
// whose PasswordHash is stored in plaintext form is warned about by name,
// and a level with a real bcrypt hash, or no PasswordHash at all, is not.
func TestWarnPlaintextLevelSecretsWarnsOnlyForPlaintextLevels(t *testing.T) {
	logger, output := captureLogOutput()
	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	levels := &command.TreeStructure{Order: []*command.CommandLevel{
		{Name: "exec", PasswordHash: "$0$plaintext"},
		{Name: "diagnostic", PasswordHash: hash},
		{Name: "config"},
	}}

	warnPlaintextLevelSecrets(logger, levels)

	got := output()
	if !strings.Contains(got, "exec") {
		t.Errorf("expected a warning naming \"exec\", got %q", got)
	}
	if strings.Contains(got, "diagnostic") {
		t.Errorf("expected no warning for \"diagnostic\", which has a real bcrypt hash, got %q", got)
	}
	if strings.Contains(got, "config") {
		t.Errorf("expected no warning for \"config\", which has no PasswordHash at all, got %q", got)
	}
}

// ----------------------------------------------------------------------
//
// firstBadToken
//
// ----------------------------------------------------------------------

// TestFirstBadTokenReturnsFirstEntry - This test verifies that
// firstBadToken returns only the first entry of a multi-element Args
// slice, "fan" not "fan extra junk", matching how a real device points at
// the first word it could not place.
func TestFirstBadTokenReturnsFirstEntry(t *testing.T) {
	got := firstBadToken([]string{"fan", "extra", "junk"})
	if got != "fan" {
		t.Errorf("firstBadToken([\"fan\", \"extra\", \"junk\"]) = %q, want %q", got, "fan")
	}
}

// TestFirstBadTokenEmptyForEmptyArgs - This test verifies that
// firstBadToken returns an empty string, rather than panicking, for an
// empty Args slice.
func TestFirstBadTokenEmptyForEmptyArgs(t *testing.T) {
	if got := firstBadToken(nil); got != "" {
		t.Errorf("firstBadToken(nil) = %q, want empty", got)
	}
	if got := firstBadToken([]string{}); got != "" {
		t.Errorf("firstBadToken([]string{}) = %q, want empty", got)
	}
}

// ----------------------------------------------------------------------
//
// printOutputHeader
//
// ----------------------------------------------------------------------

// captureStdout - This function runs fn with os.Stdout temporarily
// redirected to an in-memory pipe, and returns everything fn printed,
// restoring the real os.Stdout afterward. printOutputHeader prints
// straight to os.Stdout rather than through an injectable writer, the same
// as several handlers in cmd/core and cmd/product, see either package's
// captureStdout helper for the same reasoning.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return clitest.CaptureStdout(t, fn)
}

// TestPrintOutputHeaderIncludesVersion - This test verifies that
// printOutputHeader's output includes the current Version, and the Build
// string only when one is set, matching the "--version" and "--help"
// flags' own use of this function in processCommandLineFlags.
func TestPrintOutputHeaderIncludesVersion(t *testing.T) {
	origBuild := Build
	Build = ""
	defer func() { Build = origBuild }()

	out := captureStdout(t, printOutputHeader)
	if !strings.Contains(out, "Router CLI") {
		t.Errorf("printOutputHeader output = %q, expected it to contain %q", out, "Router CLI")
	}
	if !strings.Contains(out, Version) {
		t.Errorf("printOutputHeader output = %q, expected it to contain the Version %q", out, Version)
	}
}

// TestPrintOutputHeaderIncludesBuildWhenSet - This test verifies that a
// non-empty Build string is included in the header, the branch skipped by
// TestPrintOutputHeaderIncludesVersion.
func TestPrintOutputHeaderIncludesBuildWhenSet(t *testing.T) {
	origBuild := Build
	Build = "test-build-123"
	defer func() { Build = origBuild }()

	out := captureStdout(t, printOutputHeader)
	if !strings.Contains(out, "test-build-123") {
		t.Errorf("printOutputHeader output = %q, expected it to contain the Build string %q", out, "test-build-123")
	}
}

// ----------------------------------------------------------------------
//
// printBanner
//
// ----------------------------------------------------------------------

// TestPrintBannerPrintsNonEmptyTextWithATrailingNewline - This test
// verifies that printBanner, given a non-empty string, prints it verbatim,
// with the one trailing newline fmt.Println always adds and nothing else,
// matching its own doc comment: "no added formatting beyond its own
// trailing newline."
func TestPrintBannerPrintsNonEmptyTextWithATrailingNewline(t *testing.T) {
	out := captureStdout(t, func() { printBanner("Unauthorized access is prohibited.") })
	if out != "Unauthorized access is prohibited.\n" {
		t.Errorf("printBanner output = %q, want %q", out, "Unauthorized access is prohibited.\n")
	}
}

// TestPrintBannerPrintsNothingForEmptyText - This test verifies that
// printBanner prints nothing at all, not even a blank line, when text is
// empty, the default, unset state every banner starts in, matching its own
// doc comment's claim that neither call site needs its own separate empty
// check. A stray blank line here would show up as an unwanted empty line
// at every login and every "enable" prompt on a deployment that never
// configured a banner.
func TestPrintBannerPrintsNothingForEmptyText(t *testing.T) {
	out := captureStdout(t, func() { printBanner("") })
	if out != "" {
		t.Errorf("printBanner(\"\") printed %q, want no output at all", out)
	}
}

// ----------------------------------------------------------------------
//
// filterModeFromConfig
//
// ----------------------------------------------------------------------

// TestFilterModeFromConfigMapsRegexToFilterModeRegex - This test verifies
// that the one recognized non-default value, "regex", maps to
// paging.FilterModeRegex, the exact string
// config.SystemConfig.FilterMatchMode's validation accepts alongside
// "substring".
func TestFilterModeFromConfigMapsRegexToFilterModeRegex(t *testing.T) {
	if got := filterModeFromConfig("regex"); got != paging.FilterModeRegex {
		t.Errorf("filterModeFromConfig(\"regex\") = %v, want paging.FilterModeRegex", got)
	}
}

// TestFilterModeFromConfigMapsEverythingElseToFilterModeSubstring - This
// test verifies that "substring", the empty string, and an arbitrary
// unrecognized value all map to paging.FilterModeSubstring, the documented
// default this function falls back to for anything that is not exactly
// "regex". config.LoadSystemConfig's validation is what keeps a real,
// loaded configuration from ever reaching this function with anything
// other than "substring" or "regex", not this function itself, so the
// fallback is checked against more than just "substring" here to confirm
// it is an unconditional default, not a second exact match.
func TestFilterModeFromConfigMapsEverythingElseToFilterModeSubstring(t *testing.T) {
	for _, mode := range []string{"substring", "", "bogus"} {
		if got := filterModeFromConfig(mode); got != paging.FilterModeSubstring {
			t.Errorf("filterModeFromConfig(%q) = %v, want paging.FilterModeSubstring", mode, got)
		}
	}
}

// ----------------------------------------------------------------------
//
// recordingAuditor
//
// ----------------------------------------------------------------------

// recordingAuditor - This type is a minimal command.Auditor test double,
// used by logSessionEnd's tests below and by main_pty_test.go's runLoop
// tests, for asserting on which entries were written without standing up a
// real *auditlog.AuditLog backed by a file on disk every time. would
// controls what WouldLog reports, mimicking a real AuditLog's enabled
// flag. RunLoop snapshots it both before and after running a command. Log
// and ForceLog both record every call they receive, ForceLog
// unconditionally, Log only records the command text, mirroring the one
// distinction between the two that matters for the tests that use this
// type, ForceLog is the one logSessionEnd, and a command's audit entry
// once WouldLog was true either before or after it ran, always writes.
type recordingAuditor struct {
	would   bool
	entries []string
}

func (r *recordingAuditor) Log(username, command string, success bool) {
	r.entries = append(r.entries, command)
}

func (r *recordingAuditor) WouldLog() bool { return r.would }

func (r *recordingAuditor) ForceLog(username, command string, success bool) {
	r.entries = append(r.entries, command)
}

// hasEntry - This method reports whether any recorded entry equals want
// exactly, used instead of a plain slice comparison so a test asserting on
// one particular entry does not need to know, or care about, every other
// entry recordingAuditor may have collected along the way.
func (r *recordingAuditor) hasEntry(want string) bool {
	for _, e := range r.entries {
		if e == want {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------
//
// logSessionEnd
//
// ----------------------------------------------------------------------

// TestLogSessionEndWritesForceLogWithSessionUsername - This test verifies
// that logSessionEnd writes exactly one "SESSION END" entry, through
// ForceLog rather than Log, using ctx.Session.Username.
func TestLogSessionEndWritesForceLogWithSessionUsername(t *testing.T) {
	audit := &recordingAuditor{}
	ctx := &command.AppContext{Audit: audit, Session: &auth.Session{Username: "alice"}}

	logSessionEnd(ctx)

	if !audit.hasEntry("SESSION END") {
		t.Errorf("expected a \"SESSION END\" entry, got %v", audit.entries)
	}
}

// TestLogSessionEndUsesEmptyUsernameWhenSessionNil - This test verifies
// that logSessionEnd does not panic when ctx.Session is nil, the state
// main.go leaves ctx in in should this function ever be reached before a
// session was constructed, and still writes its SESSION END entry, with an
// empty username rather than one read off a nil pointer.
func TestLogSessionEndUsesEmptyUsernameWhenSessionNil(t *testing.T) {
	audit := &recordingAuditor{}
	ctx := &command.AppContext{Audit: audit}

	logSessionEnd(ctx)

	if !audit.hasEntry("SESSION END") {
		t.Errorf("expected a \"SESSION END\" entry even with a nil Session, got %v", audit.entries)
	}
}

// ----------------------------------------------------------------------
// passesPreExecutionGates
// ----------------------------------------------------------------------

// gateTestContext - This function builds the minimal AppContext
// passesPreExecutionGates needs: a recording auditor so a refusal can be
// asserted, and a nil Translator, which resolves every message to its
// bracketed key. These tests assert on which gate refused, never on the
// wording, so no catalog is needed.
func gateTestContext(roles []string) (*command.AppContext, *recordingAuditor) {
	audit := &recordingAuditor{would: true}
	ctx := &command.AppContext{
		Logger:  log.New(io.Discard, "", 0),
		Session: &auth.Session{Username: "operator", Authenticated: true},
		Audit:   audit,
		Users: auth.Users{
			"operator": &auth.User{Username: "operator", Roles: roles},
		},
		AuthRequired: true,
		Roles: &command.RoleSet{ByName: map[string]*command.Role{
			"netops": {Name: "netops"},
			"admin":  {Name: "admin"},
		}},
	}
	return ctx, audit
}

// TestPreExecutionGatesRefusesAWrongRoleAndAuditsIt - This test verifies
// that a command carrying AllowedRoles refuses a session holding none of
// them, and records the refusal in the audit log.
//
// The audit entry matters as much as the refusal. A denied command that
// leaves no trace is worse than one that is simply allowed.
func TestPreExecutionGatesRefusesAWrongRoleAndAuditsIt(t *testing.T) {
	ctx, audit := gateTestContext([]string{"netops"})
	cmd := &command.Command{
		AllowedRoles: []string{"admin"},
		RunFunc:      func(*command.AppContext, []string) error { return nil },
	}
	res := command.ResolveResult{Command: cmd, FullName: []string{"reboot"}}

	var ok bool
	captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "reboot", "operator")
	})

	if ok {
		t.Error("a session holding only netops must not pass an admin-only command")
	}
	if len(audit.entries) == 0 {
		t.Error("a role refusal must be audited")
	}
}

// TestPreExecutionGatesAllowsAMatchingRole - This test verifies the other
// side of the same gate, so the refusal above is not passing for some
// unrelated reason.
func TestPreExecutionGatesAllowsAMatchingRole(t *testing.T) {
	ctx, _ := gateTestContext([]string{"admin"})
	cmd := &command.Command{
		AllowedRoles: []string{"admin"},
		RunFunc:      func(*command.AppContext, []string) error { return nil },
	}
	res := command.ResolveResult{Command: cmd, FullName: []string{"reboot"}}

	var ok bool
	captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "reboot", "operator")
	})

	if !ok {
		t.Error("a session holding admin must pass an admin-only command")
	}
}

// TestPreExecutionGatesRefusesALockedOutSessionWithoutPrompting - This test
// verifies that a password gated command whose rate limiter is already
// locked out is refused before any prompt is issued.
//
// Both refusal paths return false, so the return value alone proves nothing:
// with no terminal attached, a gate that prompted first would fail the read
// and refuse anyway. The two paths are told apart by which message they
// print. A lockout prints auth.too_many_attempts; a failed or wrong password
// prints commandlevel.access_denied.
//
// ctx.Translator is nil, so each key renders as its own bracketed name,
// which is what this asserts on.
func TestPreExecutionGatesRefusesALockedOutSessionWithoutPrompting(t *testing.T) {
	ctx, audit := gateTestContext(nil)

	limiter := auth.NewRateLimiter(1, time.Minute, time.Hour)
	limiter.RecordFailure()

	cmd := &command.Command{
		PasswordHash:        "$2a$10$abcdefghijklmnopqrstuv",
		PasswordRateLimiter: limiter,
		RunFunc:             func(*command.AppContext, []string) error { return nil },
	}
	res := command.ResolveResult{Command: cmd, FullName: []string{"gated"}}

	var ok bool
	out := captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "gated", "operator")
	})

	if ok {
		t.Fatal("a locked out session must be refused")
	}
	if !strings.Contains(out, "auth.too_many_attempts") {
		t.Errorf("expected the lockout message, got %q; the limiter was not checked before prompting", out)
	}
	if len(audit.entries) == 0 {
		t.Error("a lockout refusal must be audited")
	}
}

// TestPreExecutionGatesChecksThePasswordBeforeValidatingArguments - This
// test verifies the ordering the gate documents: the password is checked
// before ValidateArgs, so a session that has not supplied the password
// never learns how many arguments the command expects.
//
// The command below is both password gated and given a wrong argument
// count. If arguments were validated first, the refusal would name the
// argument requirement and leak the command's shape. Asserting that the
// printed output does not mention arguments is what makes this a test of
// the ordering rather than of either gate alone.
func TestPreExecutionGatesChecksThePasswordBeforeValidatingArguments(t *testing.T) {
	ctx, _ := gateTestContext(nil)

	two := 2
	cmd := &command.Command{
		PasswordHash:        "$2a$10$abcdefghijklmnopqrstuv",
		PasswordRateLimiter: auth.NewRateLimiter(1, time.Minute, time.Hour),
		MinArgs:             &two,
		MaxArgs:             &two,
		RunFunc:             func(*command.AppContext, []string) error { return nil },
	}
	// No arguments supplied, so ValidateArgs would refuse if it ran first.
	res := command.ResolveResult{Command: cmd, FullName: []string{"gated"}}

	var ok bool
	out := captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "gated", "operator")
	})

	if ok {
		t.Fatal("a password gated command must not pass with no password supplied")
	}
	if strings.Contains(strings.ToLower(out), "argument") {
		t.Errorf("the refusal leaked the argument requirement before the password was checked: %q", out)
	}
}

// TestPreExecutionGatesRefusesAWrongArgumentCount - This test verifies that
// the gate refuses a command whose argument count does not satisfy MinArgs
// and MaxArgs, and audits the refusal.
//
// command.ValidateArgs has its own tests. This one covers the gate calling
// it at all: without this, the whole call could be removed and every command
// would run with whatever arguments it was given.
func TestPreExecutionGatesRefusesAWrongArgumentCount(t *testing.T) {
	ctx, audit := gateTestContext(nil)

	one := 1
	cmd := &command.Command{
		MinArgs: &one,
		MaxArgs: &one,
		RunFunc: func(*command.AppContext, []string) error { return nil },
	}
	// Two arguments supplied where exactly one is allowed.
	res := command.ResolveResult{
		Command:  cmd,
		FullName: []string{"hostname"},
		Args:     []string{"one", "two"},
	}

	var ok bool
	captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "hostname one two", "operator")
	})

	if ok {
		t.Error("two arguments must not pass a command allowing exactly one")
	}
	if len(audit.entries) == 0 {
		t.Error("an argument refusal must be audited")
	}
}

// TestPreExecutionGatesSkipsArgumentValidationWhenNegated - This test
// verifies the documented exception: a negated command is not argument
// checked, because "no X" often takes a different argument shape than "X".
//
// Cisco's "no description" takes none while "description <text>" requires
// one. Forcing one MinArgs to describe both would make the negated form
// reject the arguments it needs, or the positive form accept ones it should
// not.
func TestPreExecutionGatesSkipsArgumentValidationWhenNegated(t *testing.T) {
	ctx, _ := gateTestContext(nil)

	one := 1
	cmd := &command.Command{
		MinArgs:   &one,
		MaxArgs:   &one,
		Negatable: true,
		RunFunc:   func(*command.AppContext, []string) error { return nil },
	}
	// No arguments, which would fail MinArgs if the check ran.
	res := command.ResolveResult{
		Command:  cmd,
		FullName: []string{"description"},
		Negated:  true,
	}

	var ok bool
	captureStdout(t, func() {
		ok = passesPreExecutionGates(ctx, res, nil, "no description", "operator")
	})

	if !ok {
		t.Error("a negated command must not be argument checked")
	}
}
