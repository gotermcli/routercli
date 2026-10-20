// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"fmt"
	"io"
	"os"

	"github.com/gotermcli/routercli/i18n"

	"golang.org/x/term"
)

// EffectivePageLines - This function returns how many lines Display shows
// before pausing.
//
// override is AppContext.PageLines, nil until "terminal length" is typed.
// With no override, the live terminal height behind fd is used, minus one
// line reserved for the "--More--" prompt, so a full page plus the prompt
// never exceeds one screen.
//
// fallback is used only when fd is not a real terminal, or its size cannot
// be read. That is the same case Display treats as "print everything".
//
// A non-nil override is returned exactly as given, including zero, which
// is the Cisco convention for never pausing. Someone who asked for a
// specific page size gets that many lines, not that many minus one for a
// prompt they did not ask to reserve room for.
func EffectivePageLines(fd int, override *int, fallback int) int {
	if override != nil {
		return *override
	}
	if _, h, err := term.GetSize(fd); err == nil && h > 1 {
		return h - 1
	}
	return fallback
}

// EffectiveTerminalWidth - This function returns the current terminal
// width, in the same override, live, fallback shape EffectivePageLines
// uses for height.
//
// override is AppContext.TerminalWidth, nil until "terminal width" is
// typed. With no override the width is read fresh from fd on every call,
// so a mid session resize is already reflected.
//
// fallback is used only when fd is not a real terminal, such as piped
// stdin, or its width cannot be read.
func EffectiveTerminalWidth(fd int, override *int, fallback int) int {
	if override != nil {
		return *override
	}
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		return w
	}
	return fallback
}

// Display - This function writes lines to stdout, pausing with a
// translated "--More--" prompt every pageLines lines, the way a Cisco or
// HP pager behaves.
//
// Space shows the next page. Enter shows one more line, then pauses again.
// "q", "Q", or Ctrl-C stops and discards the rest. Any other key acts like
// space, matching real devices where almost any key advances.
//
// Pausing is skipped entirely in three cases: pagingEnabled is false,
// pageLines is zero or less, or fd is not a real terminal. That last one
// matters because there is no keyboard to read from, and pausing would
// hang the session forever.
//
// Output that already fits in pageLines shows no prompt either, with no
// flag needed, since a real device shows none for a single screen.
func Display(stdout io.Writer, fd int, translator *i18n.Translator, lines []string, pageLines int, pagingEnabled bool) error {
	if !pagingEnabled || pageLines <= 0 || !term.IsTerminal(fd) || len(lines) <= pageLines {
		writeAll(stdout, lines)
		return nil
	}

	prompt := translator.T("pager.more_prompt")
	step := pageLines
	i := 0
	for i < len(lines) {
		end := i + step
		if end > len(lines) {
			end = len(lines)
		}
		writeAll(stdout, lines[i:end])
		i = end
		if i >= len(lines) {
			return nil
		}

		fmt.Fprint(stdout, prompt)
		action, err := readKeypress(fd)
		clearPrompt(stdout)
		if err != nil {
			// No further keypress could be read, for example the terminal
			// went away mid-pause. Print whatever is left rather than
			// leaving the session stuck at a prompt nothing will ever
			// answer.
			writeAll(stdout, lines[i:])
			return nil
		}

		switch action {
		case actionQuit:
			return nil
		case actionLine:
			step = 1
		default:
			step = pageLines
		}
	}
	return nil
}

// keyAction - This type is what one raw keypress read by readKeypress
// resolves to, one of actionPage, actionLine, or actionQuit.
type keyAction int

const (
	actionPage keyAction = iota
	actionLine
	actionQuit
)

// writeAll - This function prints every entry of lines to w, one per line.
// It is the plain, no pause fallback Display itself uses, and the shared
// building block its own paused loop prints each page through as well.
func writeAll(w io.Writer, lines []string) {
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

// clearPrompt - This function erases the prompt Display just printed,
// before the next page is written over it.
//
// "\r" returns the cursor to the start of the line and the ANSI "\x1b[K"
// sequence clears from there to the end, so the real prompt length does
// not matter.
func clearPrompt(w io.Writer) {
	fmt.Fprint(w, "\r\x1b[K")
}

// readKeypress - This function reads one raw byte from fd, putting the
// terminal into raw mode for just that read and restoring it afterward on
// every path.
//
// The mode is set explicitly rather than assumed from whatever readline
// left the terminal in between dispatches, so correctness here does not
// depend on that holding.
//
// os.NewFile wraps fd without duplicating it, so this reads the real
// descriptor. The wrapper is never closed: Close on a file built this way
// closes the descriptor it wraps, which would take stdin out from under
// the rest of the session.
func readKeypress(fd int) (keyAction, error) {
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return actionPage, err
	}
	defer term.Restore(fd, oldState)

	f := os.NewFile(uintptr(fd), "")
	buf := make([]byte, 1)
	if _, err := f.Read(buf); err != nil {
		return actionPage, err
	}

	switch buf[0] {
	case ' ':
		return actionPage, nil
	case '\r', '\n':
		return actionLine, nil
	case 'q', 'Q', 0x03:
		return actionQuit, nil
	default:
		return actionPage, nil
	}
}
