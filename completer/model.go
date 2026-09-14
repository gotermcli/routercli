// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package completer

import (
	"github.com/chzyer/readline"
	"github.com/gologme/log"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/i18n"
)

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// NoopCompleter - This type satisfies readline's requirement that
// AutoComplete be non-nil, since Tab otherwise just bells if AutoComplete
// is nil. It does none of the actual completion work itself. See
// TreeListener below for why.
type NoopCompleter struct{}

// TreeListener - Abbreviation expansion has to rewrite tokens already on
// the line, turning "sh run" into "show running-config". Readline's
// AutoComplete contract can only insert a suffix at the cursor, so it
// cannot do that.
//
// This implements readline.Listener instead. That fires on every keypress,
// after readline has handled the key, and lets this return a full
// replacement buffer.
//
// position holds a *command.CommandLevelStack rather than a fixed tree,
// and every OnChange reads Current().Tree fresh. That is what makes
// completion Command Level aware: entering config mode changes what Tab
// offers, because this reads the same stack the dispatch loop mutates.
//
// lastAmbiguousInput and tapCount track the double Tab sequence, which is
// why one listener instance serves one session.
type TreeListener struct {
	position   *command.CommandLevelStack
	instance   *readline.Instance
	logger     *log.Logger
	translator *i18n.Translator

	// listOptions controls how OnChange and handleHelp order a candidate
	// list of more than one command name. New's doc comment covers where
	// this comes from.
	listOptions command.ListOptions

	currentPrompt      string
	lastAmbiguousInput string
	tapCount           int

	// terminalFD, terminalWidthOverride, and defaultTerminalWidth are the
	// three inputs to paging.EffectiveTerminalWidth, which handleHelp uses
	// to size its output.
	//
	// They are held here rather than resolved once at construction, so a
	// mid session resize is picked up the next time "?" is pressed.
	//
	// terminalFD defaults to -1, which term.GetSize always fails against.
	// A listener that never had SetTerminalWidth called on it, every test
	// in this package included, therefore falls back to
	// defaultTerminalWidth, left at zero, which is read as the ordinary 80
	// column default.
	terminalFD            int
	terminalWidthOverride *int
	defaultTerminalWidth  int
}

// ----------------------------------------------------------------------
// Initialization Functions
// ----------------------------------------------------------------------

// New - This function constructs a TreeListener bound to a
// CommandLevelStack.
//
// rl is needed to print the candidate list in the right place. logger is
// for debug tracing. translator resolves a Command's ArgHelpKey and may
// be nil, in which case the literal ArgHelp field is used.
//
// listOptions controls the order a candidate list prints in. main.go
// passes the same value it puts on AppContext.ListOptions, so the
// interactive Tab and "?" paths here and the non-interactive fallback in
// runLoop always agree on ordering.
func New(position *command.CommandLevelStack, instance *readline.Instance, logger *log.Logger, translator *i18n.Translator, listOptions command.ListOptions) *TreeListener {
	return &TreeListener{position: position, instance: instance, logger: logger, translator: translator, listOptions: listOptions, tapCount: 0, terminalFD: -1}
}

// SetTerminalWidth - This method records where to read the live terminal
// width from, matching EffectiveTerminalWidth's fd, override, and
// fallback parameters, so handleHelp can wrap a long "?" listing the same
// way the "help" command wraps its output.
//
// main.go calls this once, alongside SetPrompt. Leaving it uncalled keeps
// New's -1 default, which falls back to the fallback width. Every test in
// this package relies on that.
func (l *TreeListener) SetTerminalWidth(fd int, override *int, fallback int) {
	l.terminalFD = fd
	l.terminalWidthOverride = override
	l.defaultTerminalWidth = fallback
}
