// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotermcli/routercli/auth"
)

// ----------------------------------------------------------------------
//
// TestHelperProcess and the subprocess helpers built on it
//
// ----------------------------------------------------------------------

// TestHelperProcess - This is not a real test. It never runs any assertion
// of its own, and returns immediately, doing nothing at all, unless
// GO_WANT_HELPER_PROCESS=1 is set in its environment. The tests below
// launch this same compiled test binary as a real subprocess with that
// variable set, and with "-test.run=^TestHelperProcess$" so nothing else
// in this package's test suite also runs inside it, so that the several
// os.Exit calls scattered through main and processCommandLineFlags,
// config.LoadSystemConfig failing, --check-config, --version, --help,
// --hashpassword, terminate this disposable child process instead of the
// real "go test" run asserting on it. This is the same technique Go's
// standard library tests os/exec with, see exec_test.go's
// TestHelperProcess, for exactly the same reason, a function that
// legitimately calls os.Exit cannot otherwise be exercised from inside the
// test binary that is supposed to keep running afterward to report the
// result.
//
// The flag package's "--" terminator means everything on the command line
// after it is left untouched in os.Args rather than being parsed as a
// testing flag, see the flag package's documentation for "--", so this
// replaces os.Args wholesale with whatever followed "--" before calling
// the real, unmodified main(), the same argv shape a real invocation of
// the compiled routercli binary would have seen.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:] // drop the "--" separator itself
	}
	os.Args = append([]string{"routercli"}, args...)
	main()
	// Reached only if main() itself returns without ever calling os.Exit,
	// which does not currently happen along any path this file's tests
	// drive it through. Kept so a future change to main() that adds a path
	// with no os.Exit of its own does not leave this helper process
	// hanging around instead of exiting cleanly.
	os.Exit(0)
}

// subprocessResult - This type bundles what a helper process did, its exit
// code and both output streams, so runMainAsSubprocess and
// runMainAsSubprocessWithStdin can share one return shape.
type subprocessResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// runMainAsSubprocessWithStdin - This function launches this same test
// binary as a subprocess in TestHelperProcess mode, see its own doc
// comment, with args as the simulated routercli command line and stdin, if
// non-nil, connected to the child's stdin. Returns the child's captured
// stdout, stderr, and real process exit code. A child that exits nonzero
// is not itself a test failure here, most of this file's tests are
// specifically checking for a nonzero exit and a particular stderr
// message, the same way a real administrator running routercli from a real
// shell would judge a failure by what was printed and what the shell
// reported afterward, not by go test's pass or fail bookkeeping.
func runMainAsSubprocessWithStdin(t *testing.T, dir string, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmdArgs := append([]string{"-test.run=^TestHelperProcess$", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("failed to run helper subprocess: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// runMainAsSubprocess - This function is runMainAsSubprocessWithStdin with
// no stdin connected at all, for the many flag combinations below that
// never try to read anything, --version, --help, a bad --config path, and
// --check-config.
func runMainAsSubprocess(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runMainAsSubprocessWithStdin(t, dir, nil, args...)
}

// startMainAsSubprocessWithPTY - This function is
// runMainAsSubprocessWithStdin with the child's stdin connected to the
// slave end of a real pseudo terminal instead of an ordinary pipe, for
// flag combinations that call term.ReadPassword, --hashpassword below, the
// same requirement main_pty_test.go's in-process pty tests have, a plain
// pipe is not a real terminal device and golang.org/x/term's ioctl calls
// fail against one. master is returned so the caller can write to it
// before the child's read happens; writing before starting the subprocess
// would not work. A pty is needed here at all rather than a pipe. The
// child is started, not awaited, by this function; the caller writes to
// master and then calls waitPTYSubprocess to collect the result once done.
func startMainAsSubprocessWithPTY(t *testing.T, dir string, args ...string) (cmd *exec.Cmd, master *os.File, stdout, stderr *bytes.Buffer) {
	t.Helper()
	m, s := newPTY(t)
	cmdArgs := append([]string{"-test.run=^TestHelperProcess$", "--"}, args...)
	c := exec.Command(os.Args[0], cmdArgs...)
	c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	if dir != "" {
		c.Dir = dir
	}
	c.Stdin = s
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	if err := c.Start(); err != nil {
		t.Fatalf("failed to start helper subprocess: %v", err)
	}
	return c, m, &outBuf, &errBuf
}

// waitPTYSubprocessResult - This function waits for cmd, started by
// startMainAsSubprocessWithPTY, to exit, returning the same
// subprocessResult shape the other helpers in this file use.
func waitPTYSubprocessResult(t *testing.T, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) subprocessResult {
	t.Helper()
	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()
	select {
	case err := <-doneCh:
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("helper subprocess did not exit cleanly: %v", err)
			}
		}
		return subprocessResult{stdout.String(), stderr.String(), code}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("helper subprocess did not exit in time")
		return subprocessResult{}
	}
}

// ----------------------------------------------------------------------
//
// processCommandLineFlags, through main's os.Exit paths
//
// ----------------------------------------------------------------------

// TestMainVersionFlagPrintsHeaderAndExitsZero - This test verifies that
// --version prints printOutputHeader's output and exits 0, without ever
// needing a real configuration file at all, matching
// processCommandLineFlags's early return for this flag, before
// config.LoadSystemConfig is ever reached.
func TestMainVersionFlagPrintsHeaderAndExitsZero(t *testing.T) {
	stdout, _, code := runMainAsSubprocess(t, "", "--version")
	if code != 0 {
		t.Errorf("exit code = %d, want 0, stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "Router CLI") {
		t.Errorf("expected --version output to contain \"Router CLI\", got %q", stdout)
	}
}

// TestMainHelpFlagPrintsUsageAndExitsZero - This test verifies that --help
// prints usage information and exits 0, the other flag
// processCommandLineFlags handles before ever touching a configuration
// file.
func TestMainHelpFlagPrintsUsageAndExitsZero(t *testing.T) {
	stdout, _, code := runMainAsSubprocess(t, "", "--help")
	if code != 0 {
		t.Errorf("exit code = %d, want 0, stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "Router CLI") {
		t.Errorf("expected --help output to contain the header, got %q", stdout)
	}
}

// TestMainMalformedConfigFileExitsOne - This test verifies main's first
// config.LoadSystemConfig error branch, exit code 1 with a message on
// stderr, using an unparseable YAML file. A --config path that does not
// exist is, by config.LoadSystemConfig's deliberate design, not an error
// at all, see its own os.ErrNotExist handling, it falls back to
// DefaultSystemConfig silently, so exercising this error branch needs a
// file that exists but fails to parse instead.
func TestMainMalformedConfigFileExitsOne(t *testing.T) {
	badConfig := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(badConfig, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatalf("failed to write a broken config fixture: %v", err)
	}

	_, stderr, code := runMainAsSubprocess(t, "", "--config", badConfig)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for an unparseable --config file, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "failed to load configuration") {
		t.Errorf("expected stderr to explain the configuration failed to load, got %q", stderr)
	}
}

// TestMainCheckConfigSucceedsAgainstTheShippedConfig - This test verifies
// that --check-config, run against this project's shipped
// example/etc/routercli.yaml and var/tree files, reports success and exits 0,
// without ever starting the interactive loop at all. This relies on being
// run from this project's repository root, the same assumption this
// project's manual --check-config smoke testing already makes, and is
// skipped if that shipped configuration is not present.
func TestMainCheckConfigSucceedsAgainstTheShippedConfig(t *testing.T) {
	if _, err := os.Stat("etc/routercli.yaml"); err != nil {
		t.Skipf("etc/routercli.yaml not found relative to the test working directory, skipping: %v", err)
	}
	stdout, stderr, code := runMainAsSubprocess(t, "", "--check-config")
	if code != 0 {
		t.Errorf("exit code = %d, want 0, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "configuration OK") {
		t.Errorf("expected stdout to report the configuration is OK, got %q", stdout)
	}
}

// ----------------------------------------------------------------------
//
// --hashpassword
//
// ----------------------------------------------------------------------

// TestMainHashPasswordFlagPrintsAVerifiableHash - This test verifies
// --hashpassword end to end as a real subprocess, a real pseudo terminal
// supplying the password, matching
// TestRunHashPasswordUtilityPrintsAVerifiableHash's in-process version of
// the same check but exercising the actual --hashpassword command line
// flag and os.Exit(0) at the end, rather than calling
// runHashPasswordUtility directly.
func TestMainHashPasswordFlagPrintsAVerifiableHash(t *testing.T) {
	cmd, master, stdout, stderr := startMainAsSubprocessWithPTY(t, "", "--hashpassword")
	sendLine(t, master, "s3cret")
	res := waitPTYSubprocessResult(t, cmd, stdout, stderr)

	if res.exitCode != 0 {
		t.Errorf("exit code = %d, want 0, stdout=%q", res.exitCode, res.stdout)
	}
	hash := ""
	for _, line := range splitLines(res.stdout) {
		if line != "" {
			hash = line
		}
	}
	if hash == "" {
		t.Fatalf("--hashpassword printed no hash at all, stdout was %q", res.stdout)
	}
	// auth.HashPassword's format is "$" + cryptIDBcrypt + "$" + the real
	// bcrypt encoding, cryptIDBcrypt being "6" today, so the printed line
	// correctly starts "$6$$2a$..." rather than a bare "$2a$..." bcrypt
	// string on its own. Checking against auth.VerifyPassword directly,
	// the same way TestRunHashPasswordUtilityPrintsAVerifiableHash already
	// does, avoids this test needing to know that internal format at all.
	if !auth.VerifyPassword(hash, "s3cret") {
		t.Errorf("printed hash %q does not verify against the password that was typed", hash)
	}
}
