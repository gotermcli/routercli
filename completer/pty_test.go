// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package completer

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotermcli/routercli/command"

	"github.com/chzyer/readline"
	"github.com/creack/pty"
)

// ----------------------------------------------------------------------
//
// pty test helpers
//
// ----------------------------------------------------------------------

// newPTY - This function opens a real pseudo terminal pair,
// github.com/creack/pty under the hood, and registers a t.Cleanup to close
// both ends once the test finishes. Every test in this file needs one of
// these rather than a plain os.Pipe or bytes.Buffer, since chzyer/readline
// puts the terminal into raw mode and only a genuine character device
// answers the ioctl calls that requires, the same reasoning
// main_pty_test.go's newPTY documents, in the main package, cannot be
// reused directly here since it lives in a different package. master is
// read by the test, playing the part of a person watching their own
// terminal, slave is handed to readline as both its Stdin and Stdout.
func newPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, s, err := pty.Open()
	if err != nil {
		t.Fatalf("failed to open a pseudo terminal: %v", err)
	}
	t.Cleanup(func() {
		m.Close()
		s.Close()
	})
	return m, s
}

// syncBuffer - This type collects everything a background goroutine reads
// off a pty's master side into an ordinary bytes.Buffer, guarded by a
// mutex since the read loop below runs concurrently with the test
// goroutine polling String(). This exists because, unlike the blocking
// prompt reads main_pty_test.go's helpers wait on with a single channel
// receive, the three prints this file tests, the ambiguous candidate list,
// the argument help hint, and handleHelp's help text, are side effects of
// a synchronous call the test itself makes directly, not something a
// caller blocks waiting to read back. Polling this buffer for the expected
// text is what lets the test notice output arriving on the pty
// asynchronously, since readline's wrapWriter, see chzyer/readline's
// Operation.Stdout, writes through to the pty's real file descriptor on
// its own, nothing in this package's call to OnChange or handleHelp waits
// for that write to be observed.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startReading - This function starts a background goroutine copying
// everything read from master into the returned *syncBuffer, until master
// is closed, which newPTY's t.Cleanup already arranges, at which point
// Read returns an error and the goroutine exits on its own. A test never
// needs to stop this goroutine itself.
func startReading(master *os.File) *syncBuffer {
	buf := &syncBuffer{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return buf
}

// waitForContains - This function polls buf every 20 milliseconds until
// its accumulated content contains want or timeout elapses, failing the
// test in the latter case. A fixed sleep in each test instead of this
// would either flake under load or waste real wall clock time padding out
// every run, this returns as soon as the expected text shows up on the
// pty.
func waitForContains(t *testing.T, buf *syncBuffer, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := buf.String()
		if bytes.Contains([]byte(got), []byte(want)) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pty output to contain %q, got %q", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// newPTYListener - This function builds a real *TreeListener backed by a
// pty pair, rather than the nil or field-literal construction most of this
// package's other tests use, so l.instance.Stdout() is a genuine, writable
// target instead of a nil pointer dereference. This mirrors
// main_pty_test.go's newPTYReadline, trimmed to what this package's tests
// need directly, a *TreeListener rather than the *command.AppContext-level
// wiring runLoop's tests build.
//
// Unlike newPTYReadline, this never calls rl.Close() itself. Every test in
// this file calls OnChange or handleHelp directly, never rl.Readline(), so
// the background goroutine chzyer/readline's NewTerminal starts, see
// terminal.go's ioloop, never gets scheduled far enough to run its own
// wg.Add(1) before this function returns. Calling Close() immediately
// after construction races that goroutine's first Add against Close's
// Wait, a real concurrency bug in the library itself when Close comes
// before ioloop has started, not something introduced here. Leaving the
// readline.Instance itself unclosed avoids that race entirely, and leaks
// nothing that matters: newPTY's t.Cleanup below closes master and slave
// first, LIFO, which is what makes ioloop's blocked read return an error
// and the goroutine exit on its own, Close() was only ever needed to
// unblock that same read a different way.
func newPTYListener(t *testing.T, tree map[string]*command.Command) (l *TreeListener, master *os.File) {
	t.Helper()
	master, slave := newPTY(t)
	rl, err := readline.NewEx(&readline.Config{
		Stdin:           slave,
		Stdout:          slave,
		AutoComplete:    NoopCompleter{},
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		t.Fatalf("failed to construct a pty backed readline.Instance: %v", err)
	}

	position := command.NewCommandLevelStack("exec", "", tree)
	l = New(position, rl, testLogger(), nil, command.DefaultListOptions())
	return l, master
}

// ----------------------------------------------------------------------
//
// OnChange ambiguous candidate list
//
// ----------------------------------------------------------------------

// TestOnChangePrintsAmbiguousCandidateListOnTrailingSpace - This test
// verifies the empty-token half of OnChange's Ambiguous branch end to end
// through a real pty: "show " followed by Tab lists show's two children
// immediately, no second Tap needed, see ambiguousTokenIsEmpty's doc
// comment, and that list reaches the terminal through l.instance.Stdout().
// Package-level tests already cover the decision of whether to print, see
// TestAmbiguousTokenIsEmptyTrailingSpace, this is the one test in this
// package confirming the print itself, the line
// TestOnChangeAddsTrailingSpaceAfterUniqueCompletion's doc comment says
// needs a real readline.Instance unavailable to the rest of this package,
// happens.
func TestOnChangePrintsAmbiguousCandidateListOnTrailingSpace(t *testing.T) {
	l, master := newPTYListener(t, testTree())
	buf := startReading(master)

	line := []rune("show ")
	_, _, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, show has no unique unambiguous expansion to offer")
	}

	got := waitForContains(t, buf, "version", 2*time.Second)
	if !bytes.Contains([]byte(got), []byte("running-config")) {
		t.Errorf("expected both of show's children in the printed list, got %q", got)
	}
}

// TestOnChangePrintsAmbiguousCandidateListOnSecondTap - This test verifies
// the other half of the same branch, a partial ambiguous word, "en", needs
// a second consecutive Tab on the same input before the candidate list
// prints, matching real Cisco and HP double Tab behavior. The first Tab is
// confirmed silent, package level tests already cover that decision
// directly, see
// TestOnChangeAmbiguousPartialWordTracksTapCountWithoutPrinting, this
// test's first call exists only to advance tapCount to 1 so the second
// call is the second tap, not to re-assert silence itself.
func TestOnChangePrintsAmbiguousCandidateListOnSecondTap(t *testing.T) {
	l, master := newPTYListener(t, testTree())
	buf := startReading(master)

	line := []rune("en")
	l.OnChange(line, len(line), readline.CharTab)
	if l.tapCount != 1 {
		t.Fatalf("test setup problem: tapCount = %d after the first Tab, want 1", l.tapCount)
	}

	_, _, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, \"en\" itself has nothing further to expand")
	}
	if l.tapCount != 2 {
		t.Errorf("tapCount = %d after the second Tab, want 2", l.tapCount)
	}

	got := waitForContains(t, buf, "enable", 2*time.Second)
	if !bytes.Contains([]byte(got), []byte("end")) {
		t.Errorf("expected both ambiguous candidates in the printed list, got %q", got)
	}
}

// ----------------------------------------------------------------------
//
// OnChange argument help hint
//
// ----------------------------------------------------------------------

// TestOnChangePrintsArgHelpHintAfterLeafCommand - This test verifies the
// ArgHelp hint half of OnChange end to end through a real pty: "terminal
// length " followed by Tab, a fully resolved leaf command with MinArgs set
// and nothing typed yet for its argument, prints the configured ArgHelp
// text. TestOnChangeArgHelpHintDetection already pins down the
// Resolve-level conditions that decide whether to print this hint at all,
// this test is the one confirming the print itself reaches
// l.instance.Stdout(), which that test's doc comment says it does not
// attempt.
func TestOnChangePrintsArgHelpHintAfterLeafCommand(t *testing.T) {
	l, master := newPTYListener(t, testTree())
	buf := startReading(master)

	line := []rune("terminal length ")
	_, _, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, the line is already fully resolved with nothing left to rewrite")
	}

	got := waitForContains(t, buf, "<2-1000>", 2*time.Second)
	if got == "" {
		t.Error("expected the configured ArgHelp text to be printed")
	}
}

// ----------------------------------------------------------------------
//
// handleHelp
//
// ----------------------------------------------------------------------

// TestHandleHelpPrintsHelpTextAtBarePrompt - This test verifies handleHelp
// end to end through a real pty: "?" at a bare prompt prints every top
// level command's description, the HelpForPath(tree, [""]...) result
// completer_test.go's TestOnChangeQuestionMarkDoesNotPanicWithoutInstance
// avoids reaching, by using an empty tree specifically so HelpForPath
// returns "" and the print is skipped, see that test's doc comment. This
// test uses testTree(), which is not empty, so the print happens and this
// confirms it reaches l.instance.Stdout() correctly.
func TestHandleHelpPrintsHelpTextAtBarePrompt(t *testing.T) {
	l, master := newPTYListener(t, testTree())
	buf := startReading(master)

	line := []rune("?")
	newLine, newPos, ok := l.OnChange(line, len(line), '?')
	if !ok {
		t.Fatal("expected OnChange to report ok=true for '?', it always restores the buffer")
	}
	if string(newLine) != "" || newPos != 0 {
		t.Errorf("OnChange('?') = (%q, %d), want (\"\", 0) at a bare prompt", string(newLine), newPos)
	}

	got := waitForContains(t, buf, "Elevate this session", 2*time.Second)
	if !bytes.Contains([]byte(got), []byte("Return to the top-level")) {
		t.Errorf("expected help text for the whole top level tree, got %q", got)
	}
}

// TestHandleHelpPrintsHelpTextForContainerWithTrailingSpace - This test
// verifies the "show ?" case, help for a specific container's children,
// rather than the bare prompt's full top level listing above, still
// reaches the real terminal correctly.
func TestHandleHelpPrintsHelpTextForContainerWithTrailingSpace(t *testing.T) {
	l, master := newPTYListener(t, testTree())
	buf := startReading(master)

	line := []rune("show ?")
	newLine, newPos, ok := l.OnChange(line, len(line), '?')
	if !ok {
		t.Fatal("expected OnChange to report ok=true for '?'")
	}
	want := "show "
	if string(newLine) != want || newPos != len([]rune(want)) {
		t.Errorf("OnChange(\"show ?\") = (%q, %d), want (%q, %d)", string(newLine), newPos, want, len([]rune(want)))
	}

	got := waitForContains(t, buf, "Show version", 2*time.Second)
	if !bytes.Contains([]byte(got), []byte("Show running config")) {
		t.Errorf("expected help text scoped to show's children, got %q", got)
	}
}

// ----------------------------------------------------------------------
//
// OnChange "<cr>"
//
// ----------------------------------------------------------------------

// crTestTree - This function gives the tests below a tree shaped like
// example/var/tree/level_user.yaml's "totp" branch: "enable" is itself a complete,
// runnable command, but also has one subcommand, "qr", below it. "exit" is
// a plain, argumentless, runnable leaf with no subcommands at all. Neither
// testTree()'s "exit" nor "enable" has a RunFunc, since the tests that
// already use testTree() do not need one, so this is a separate, dedicated
// tree rather than a change to that shared one.
func crTestTree() map[string]*command.Command {
	return map[string]*command.Command{
		"exit": {Desc: "Exit", RunFunc: func(*command.AppContext, []string) error { return nil }},
		"totp": {
			Subcommands: map[string]*command.Command{
				"enable": {
					Desc:    "Enable TOTP",
					RunFunc: func(*command.AppContext, []string) error { return nil },
					Subcommands: map[string]*command.Command{
						"qr": {Desc: "Show a QR code", RunFunc: func(*command.AppContext, []string) error { return nil }},
					},
				},
				"disable": {Desc: "Disable TOTP", RunFunc: func(*command.AppContext, []string) error { return nil }},
			},
		},
	}
}

// TestOnChangePrintsCRForFullyResolvedRunnableLeaf - This test verifies
// that "exit " followed by Tab prints "<cr>". That is a fully resolved,
// argumentless, runnable leaf, so pressing Enter is a complete command and
// the listing MUST say so.
func TestOnChangePrintsCRForFullyResolvedRunnableLeaf(t *testing.T) {
	l, master := newPTYListener(t, crTestTree())
	buf := startReading(master)

	line := []rune("exit ")
	_, _, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, \"exit\" has nothing left to rewrite")
	}

	got := waitForContains(t, buf, "<cr>", 2*time.Second)
	if got == "" {
		t.Error("expected \"<cr>\" to be printed for a fully resolved, argumentless leaf")
	}
}

// TestOnChangePrintsAmbiguousCandidateListWithCRForRunnableContainer -This
// test verifies the ambiguous half of "<cr>" support end to end through a
// real pty: "totp enable " followed by Tab must show both "qr", the sole
// remaining subcommand, and "<cr>", since "enable" is already a complete
// command on its own, matching real Cisco and HP. This is the pty-backed
// companion to
// TestResolveRunnableContainerWithSoleSubcommandStaysAmbiguousInsteadOfAutoDescending
// in package command, which covers the underlying Resolve() decision. This
// test confirms OnChange prints both
// pieces to the terminal, immediately, no second Tap needed, since nothing
// is typed yet for this position.
func TestOnChangePrintsAmbiguousCandidateListWithCRForRunnableContainer(t *testing.T) {
	l, master := newPTYListener(t, crTestTree())
	buf := startReading(master)

	line := []rune("totp enable ")
	_, _, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, \"totp enable\" is ambiguous against \"qr\"")
	}

	got := waitForContains(t, buf, "<cr>", 2*time.Second)
	if !bytes.Contains([]byte(got), []byte("qr")) {
		t.Errorf("expected both \"qr\" and \"<cr>\" in the printed list, got %q", got)
	}
	if strings.Index(got, "qr") > strings.Index(got, "<cr>") {
		t.Errorf("expected \"qr\" before \"<cr>\", got %q", got)
	}
}

// TestOnChangeHelpWithUnknownArgumentKeepsOneSpace - This test verifies
// that Tab after "help" and a word matching nothing leaves exactly one
// space between them.
//
// The unwrapping path builds the rewritten line from a prefix, "help ",
// plus whatever resolved after it. When nothing resolves, the resolved
// part is empty, and adding a separator unconditionally produced a second
// space, so "help foo" came back as "help  foo".
func TestOnChangeHelpWithUnknownArgumentKeepsOneSpace(t *testing.T) {
	tree := map[string]*command.Command{
		"help": {Desc: "help", RunFunc: func(*command.AppContext, []string) error { return nil }},
		"show": {Desc: "show", Subcommands: map[string]*command.Command{
			"version": {Desc: "version", RunFunc: func(*command.AppContext, []string) error { return nil }},
		}},
	}
	l, _ := newPTYListener(t, tree)

	line := []rune("help foo")
	newLine, _, ok := l.OnChange(line, len(line), '\t')
	if !ok {
		return
	}
	if got := string(newLine); strings.Contains(got, "help  ") {
		t.Errorf("Tab on %q produced %q, which doubles the space after \"help\"", string(line), got)
	}
}
