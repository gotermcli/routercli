// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gotermcli/routercli/internal/clitest"
)

// ----------------------------------------------------------------------
//
// pty test helpers
//
// ----------------------------------------------------------------------

// newPTY - This function is a thin alias for clitest.NewPTY, kept so this
// package's tests read the same way they always have. The real
// implementation is shared with every other package that needs a real
// terminal; see internal/clitest.
func newPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	return clitest.NewPTY(t)
}

// syncBuffer - This type collects everything a background goroutine reads
// off a pty's master side into an ordinary bytes.Buffer, guarded by a
// mutex, since that read loop runs concurrently with the test goroutine
// calling Display and polling String() to see what has arrived so far.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// readFromMaster - This function starts a background goroutine copying
// everything written to master into a *syncBuffer, returning it so a test
// can poll String() as Display's output arrives.
func readFromMaster(master *os.File) *syncBuffer {
	out := &syncBuffer{}
	go func() { io.Copy(out, master) }()
	return out
}

// waitForSubstring - This function polls got until it contains want,
// failing the test if that never happens within timeout. Display writes to
// the pty asynchronously from this test's goroutine, so a plain single
// check right after sending a keypress can race ahead of Display's next
// write.
func waitForSubstring(t *testing.T, got func() string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(got(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q, got so far:\n%s", want, got())
}

// ----------------------------------------------------------------------
//
// EffectivePageLines
//
// ----------------------------------------------------------------------

// TestEffectivePageLinesReturnsOverrideExactly - This test verifies that a
// non-nil override, "terminal length" already typed this session, is
// returned exactly as given, with no further adjustment, including the
// real Cisco "never pause" value, zero.
func TestEffectivePageLinesReturnsOverrideExactly(t *testing.T) {
	for _, want := range []int{0, 1, 40, 512} {
		override := want
		got := EffectivePageLines(int(os.Stdin.Fd()), &override, 24)
		if got != want {
			t.Errorf("EffectivePageLines with override %d = %d, want %d unchanged", want, got, want)
		}
	}
}

// TestEffectivePageLinesFallsBackWhenFDIsNotATerminal - This test verifies
// that with no override and fd not a real terminal, an ordinary os.Pipe
// here, fallback is returned rather than whatever term.GetSize would
// otherwise report for a non-terminal descriptor.
func TestEffectivePageLinesFallsBackWhenFDIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open a pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	got := EffectivePageLines(int(r.Fd()), nil, 24)
	if got != 24 {
		t.Errorf("EffectivePageLines = %d, want the fallback 24", got)
	}
}

// TestEffectivePageLinesUsesRealTerminalHeightMinusOne - This test
// verifies that with no override and fd a real pty, the detected height
// minus one, reserved for the "--More--" prompt itself, is returned, not
// the raw height term.GetSize reports.
func TestEffectivePageLinesUsesRealTerminalHeightMinusOne(t *testing.T) {
	master, slave := newPTY(t)
	if err := pty.Setsize(master, &pty.Winsize{Rows: 30, Cols: 80}); err != nil {
		t.Fatalf("failed to set the pty's size: %v", err)
	}

	got := EffectivePageLines(int(slave.Fd()), nil, 24)
	if got != 29 {
		t.Errorf("EffectivePageLines = %d, want 29 (30 rows minus one for the prompt)", got)
	}
}

// ----------------------------------------------------------------------
//
// EffectiveTerminalWidth
//
// ----------------------------------------------------------------------

// TestEffectiveTerminalWidthReturnsOverrideExactly - This test verifies
// that a non-nil override, "terminal width" already typed this session, is
// returned exactly as given, with no further adjustment, mirroring
// TestEffectivePageLinesReturnsOverrideExactly above for its sibling
// function.
func TestEffectiveTerminalWidthReturnsOverrideExactly(t *testing.T) {
	for _, want := range []int{0, 1, 80, 512} {
		override := want
		got := EffectiveTerminalWidth(int(os.Stdin.Fd()), &override, 0)
		if got != want {
			t.Errorf("EffectiveTerminalWidth with override %d = %d, want %d unchanged", want, got, want)
		}
	}
}

// TestEffectiveTerminalWidthReturnsFallbackWhenFDIsNotATerminal - This
// test verifies that with no override and fd not a real terminal, an
// ordinary os.Pipe here, fallback is returned exactly as given, the same
// "cannot be determined" convention EffectivePageLines already falls back
// for, mirroring TestEffectivePageLinesFallsBackWhenFDIsNotATerminal above
// for its sibling function.
func TestEffectiveTerminalWidthReturnsFallbackWhenFDIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open a pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	got := EffectiveTerminalWidth(int(r.Fd()), nil, 132)
	if got != 132 {
		t.Errorf("EffectiveTerminalWidth = %d, want 132 (fallback)", got)
	}
}

// TestEffectiveTerminalWidthUsesRealTerminalWidth - This test verifies
// that with no override and fd a real pty, the detected width is returned
// as is, with no adjustment of the kind EffectivePageLines applies to
// height for its own "--More--" prompt.
func TestEffectiveTerminalWidthUsesRealTerminalWidth(t *testing.T) {
	master, slave := newPTY(t)
	if err := pty.Setsize(master, &pty.Winsize{Rows: 30, Cols: 100}); err != nil {
		t.Fatalf("failed to set the pty's size: %v", err)
	}

	got := EffectiveTerminalWidth(int(slave.Fd()), nil, 0)
	if got != 100 {
		t.Errorf("EffectiveTerminalWidth = %d, want 100", got)
	}
}

// TestEffectiveTerminalWidthReflectsAResizeWithNoStaleness - This test
// verifies the core claim behind EffectiveTerminalWidth reads term.GetSize
// fresh on every call, so a resize between two calls,
// github.com/creack/pty's Setsize here standing in for a real
// SIGWINCH-driving terminal emulator resize, is reflected immediately on
// the very next call, with no caching of an earlier, now stale value
// anywhere in this function.
func TestEffectiveTerminalWidthReflectsAResizeWithNoStaleness(t *testing.T) {
	master, slave := newPTY(t)
	if err := pty.Setsize(master, &pty.Winsize{Rows: 30, Cols: 80}); err != nil {
		t.Fatalf("failed to set the pty's initial size: %v", err)
	}
	if got := EffectiveTerminalWidth(int(slave.Fd()), nil, 0); got != 80 {
		t.Fatalf("EffectiveTerminalWidth before resize = %d, want 80", got)
	}

	if err := pty.Setsize(master, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatalf("failed to resize the pty: %v", err)
	}
	if got := EffectiveTerminalWidth(int(slave.Fd()), nil, 0); got != 120 {
		t.Errorf("EffectiveTerminalWidth after resize = %d, want 120", got)
	}
}

// ----------------------------------------------------------------------
//
// Display - the skip-pausing paths
//
// ----------------------------------------------------------------------

// TestDisplayWritesEverythingWhenPagingDisabled - This test verifies that
// pagingEnabled false, config.SystemConfig.PagingEnabled's deployment wide
// switch turned off, writes every line straight through with no "--More--"
// prompt at all, even against a real terminal with a small pageLines.
func TestDisplayWritesEverythingWhenPagingDisabled(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"a", "b", "c", "d", "e"}

	if err := Display(slave, int(slave.Fd()), nil, lines, 2, false); err != nil {
		t.Fatalf("Display returned unexpected error: %v", err)
	}
	waitForSubstring(t, out.String, "e\r\n", time.Second)
	if strings.Contains(out.String(), "More") {
		t.Errorf("expected no \"--More--\" prompt with paging disabled, got:\n%s", out.String())
	}
}

// TestDisplayWritesEverythingWhenPageLinesIsZeroOrLess - This test
// verifies "terminal length 0", the real Cisco convention for "never
// pause," honored here as pageLines <= 0, writes every line straight
// through with no pause, the same as paging being disabled entirely.
func TestDisplayWritesEverythingWhenPageLinesIsZeroOrLess(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"a", "b", "c"}

	if err := Display(slave, int(slave.Fd()), nil, lines, 0, true); err != nil {
		t.Fatalf("Display returned unexpected error: %v", err)
	}
	waitForSubstring(t, out.String, "c\r\n", time.Second)
	if strings.Contains(out.String(), "More") {
		t.Errorf("expected no \"--More--\" prompt with pageLines 0, got:\n%s", out.String())
	}
}

// TestDisplayWritesEverythingWhenFDIsNotATerminal - This test verifies
// that a non-terminal fd, an ordinary os.Pipe here, skips pausing
// entirely, since there is no keyboard behind it to ever read a keypress
// from; pausing there would hang the session forever.
func TestDisplayWritesEverythingWhenFDIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open a pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	var buf bytes.Buffer
	lines := []string{"a", "b", "c", "d"}
	if err := Display(&buf, int(r.Fd()), nil, lines, 1, true); err != nil {
		t.Fatalf("Display returned unexpected error: %v", err)
	}
	want := "a\nb\nc\nd\n"
	if buf.String() != want {
		t.Errorf("Display output = %q, want %q", buf.String(), want)
	}
}

// TestDisplayWritesEverythingWhenOutputFitsOnOnePage - This test verifies
// that len(lines) <= pageLines skips the prompt entirely with no special
// flag needed, matching how a real device shows no "--More--" at all for
// output that already fits on one screen.
func TestDisplayWritesEverythingWhenOutputFitsOnOnePage(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"a", "b"}

	if err := Display(slave, int(slave.Fd()), nil, lines, 5, true); err != nil {
		t.Fatalf("Display returned unexpected error: %v", err)
	}
	waitForSubstring(t, out.String, "b\r\n", time.Second)
	if strings.Contains(out.String(), "More") {
		t.Errorf("expected no \"--More--\" prompt when output already fits on one page, got:\n%s", out.String())
	}
}

// ----------------------------------------------------------------------
//
// Display - the interactive pausing path
//
// ----------------------------------------------------------------------

// runDisplay - This function calls Display on its own goroutine and
// returns a channel carrying its error result. A pty backed call must run
// this way, not inline, since Display blocks reading a keypress from slave
// once a page is full, and the test itself is what writes that keypress to
// master right after starting this goroutine.
func runDisplay(stdout io.Writer, fd int, lines []string, pageLines int) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- Display(stdout, fd, nil, lines, pageLines, true)
	}()
	return ch
}

// TestDisplayPausesEveryPageLinesAndShowsMorePrompt - This test verifies
// the ordinary paged case: with pageLines 2 and 5 lines, Display shows
// exactly the first two lines, then a "--More--" prompt, then, once a
// keypress arrives, the rest.
func TestDisplayPausesEveryPageLinesAndShowsMorePrompt(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"1", "2", "3", "4", "5"}

	doneCh := runDisplay(slave, int(slave.Fd()), lines, 2)

	waitForSubstring(t, out.String, "[[pager.more_prompt]]", 2*time.Second)
	if strings.Contains(out.String(), "3\r\n") {
		t.Errorf("line 3 was written before the first \"--More--\" prompt was answered, got:\n%s", out.String())
	}

	// pageLines is 2 against 5 lines, so this pages as [1,2], [3,4], [5],
	// one prompt between each: a space after the first prompt only reaches
	// the second page, not the end, so a second space is needed to reach
	// the third and final page.
	if _, err := master.Write([]byte(" ")); err != nil {
		t.Fatalf("failed to send a space keypress: %v", err)
	}
	waitForSubstring(t, out.String, "4\r\n", 2*time.Second)

	if _, err := master.Write([]byte(" ")); err != nil {
		t.Fatalf("failed to send a second space keypress: %v", err)
	}
	waitForSubstring(t, out.String, "5\r\n", 2*time.Second)

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Display returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Display did not return after the last page was shown")
	}
}

// TestDisplayQuitStopsImmediately - This test verifies that "q" discards
// whatever page is left and Display returns right away, rather than
// continuing to page through the remainder.
func TestDisplayQuitStopsImmediately(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"1", "2", "3", "4", "5", "6"}

	doneCh := runDisplay(slave, int(slave.Fd()), lines, 2)

	waitForSubstring(t, out.String, "[[pager.more_prompt]]", 2*time.Second)
	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatalf("failed to send a quit keypress: %v", err)
	}

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Display returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Display did not return after a quit keypress")
	}

	if strings.Contains(out.String(), "3\r\n") {
		t.Errorf("expected everything after the quit keypress to be discarded, got:\n%s", out.String())
	}
}

// TestDisplayCtrlCQuitsTheSameAsQ - This test verifies that Ctrl-C, byte
// 0x03, quits immediately, exactly the same as "q" or "Q", matching
// readKeypress's documented mapping.
func TestDisplayCtrlCQuitsTheSameAsQ(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"1", "2", "3", "4"}

	doneCh := runDisplay(slave, int(slave.Fd()), lines, 2)

	waitForSubstring(t, out.String, "[[pager.more_prompt]]", 2*time.Second)
	if _, err := master.Write([]byte{0x03}); err != nil {
		t.Fatalf("failed to send a Ctrl-C keypress: %v", err)
	}

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Display returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Display did not return after a Ctrl-C keypress")
	}

	if strings.Contains(out.String(), "3\r\n") {
		t.Errorf("expected everything after Ctrl-C to be discarded, got:\n%s", out.String())
	}
}

// TestDisplayEnterShowsExactlyOneLineThenPausesAgain - This test verifies
// that Enter, or Return, advances by exactly one line rather than a full
// page, then pauses again with another "--More--" prompt, matching real
// Cisco and HP pager behavior for the Enter key.
func TestDisplayEnterShowsExactlyOneLineThenPausesAgain(t *testing.T) {
	master, slave := newPTY(t)
	out := readFromMaster(master)
	lines := []string{"1", "2", "3", "4", "5"}

	doneCh := runDisplay(slave, int(slave.Fd()), lines, 2)

	waitForSubstring(t, out.String, "[[pager.more_prompt]]", 2*time.Second)
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatalf("failed to send an Enter keypress: %v", err)
	}
	waitForSubstring(t, out.String, "3\r\n", 2*time.Second)

	// Enter only ever reveals one more line, "3", not the rest of a full
	// page, so a second "--More--" prompt must appear again before "4" is
	// shown.
	firstPromptCount := strings.Count(out.String(), "[[pager.more_prompt]]")
	if strings.Contains(out.String(), "4\r\n") {
		t.Errorf("expected line 4 to still be withheld behind a second prompt, got:\n%s", out.String())
	}

	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatalf("failed to send a quit keypress: %v", err)
	}
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Display returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Display did not return after a quit keypress")
	}
	if firstPromptCount < 1 {
		t.Errorf("expected at least one \"--More--\" prompt before Enter was sent, got:\n%s", out.String())
	}
}
