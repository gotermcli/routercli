// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package completer

import (
	"io"
	"strings"
	"testing"

	"github.com/gotermcli/routercli/command"

	"github.com/chzyer/readline"
	"github.com/gologme/log"
)

func intPtr(n int) *int { return &n }

// TestNoopCompleterDoReturnsNothing - This test verifies that
// NoopCompleter.Do always returns a nil candidate list and a zero length,
// satisfying readline's AutoComplete interface while doing none of the
// actual completion work itself, which TreeListener.OnChange handles
// instead.
func TestNoopCompleterDoReturnsNothing(t *testing.T) {
	newLine, length := NoopCompleter{}.Do([]rune("show"), 4)
	if newLine != nil {
		t.Errorf("Do() newLine = %v, want nil", newLine)
	}
	if length != 0 {
		t.Errorf("Do() length = %d, want 0", length)
	}
}

// TestNewConstructsTreeListenerWithGivenFields - This test verifies that
// New stores every argument it is given onto the returned TreeListener,
// with tapCount starting at zero, rather than leaving any of them unset.
func TestNewConstructsTreeListenerWithGivenFields(t *testing.T) {
	position := command.NewCommandLevelStack("exec", "", testTree())
	logger := testLogger()

	l := New(position, nil, logger, nil, command.DefaultListOptions())

	if l.position != position {
		t.Error("expected New to store the given CommandLevelStack")
	}
	if l.logger != logger {
		t.Error("expected New to store the given logger")
	}
	if l.tapCount != 0 {
		t.Errorf("tapCount = %d, want 0 on a freshly constructed TreeListener", l.tapCount)
	}
}

// TestSetPromptUpdatesCurrentPrompt - This test verifies that SetPrompt
// stores the prompt string it is given, for OnChange to use when rewriting
// the buffer.
func TestSetPromptUpdatesCurrentPrompt(t *testing.T) {
	l := &TreeListener{}
	l.SetPrompt("exec# ")
	if l.currentPrompt != "exec# " {
		t.Errorf("currentPrompt = %q, want %q", l.currentPrompt, "exec# ")
	}
}

// TestSetTerminalWidthStoresAllThreeArguments - This test verifies that
// SetTerminalWidth records fd, override, and fallback exactly as given,
// onto terminalFD, terminalWidthOverride, and defaultTerminalWidth
// respectively, the same fd, override, and fallback parameter shape
// paging.EffectiveTerminalWidth itself takes, see this method's doc
// comment. New leaves terminalFD at its own -1 default until main.go calls
// this method once, right alongside SetPrompt; every other test in this
// package relies on that -1 default staying in place because it never
// calls SetTerminalWidth at all, so this test is the only one confirming
// the method takes effect when it is called.
func TestSetTerminalWidthStoresAllThreeArguments(t *testing.T) {
	l := &TreeListener{}
	override := intPtr(120)

	l.SetTerminalWidth(3, override, 80)

	if l.terminalFD != 3 {
		t.Errorf("terminalFD = %d, want 3", l.terminalFD)
	}
	if l.terminalWidthOverride != override {
		t.Errorf("terminalWidthOverride = %v, want the exact pointer passed in (%v)", l.terminalWidthOverride, override)
	}
	if l.defaultTerminalWidth != 80 {
		t.Errorf("defaultTerminalWidth = %d, want 80", l.defaultTerminalWidth)
	}
}

// TestSetTerminalWidthNilOverrideIsStoredAsNil - This test verifies that a
// nil override, meaning no configured override width at all, the ordinary
// case for a deployment that never sets one, is stored as nil rather than
// replaced with some other zero value, so paging.EffectiveTerminalWidth's
// nil check still behaves correctly afterward.
func TestSetTerminalWidthNilOverrideIsStoredAsNil(t *testing.T) {
	l := &TreeListener{}
	l.SetTerminalWidth(-1, nil, 80)
	if l.terminalWidthOverride != nil {
		t.Errorf("terminalWidthOverride = %v, want nil", l.terminalWidthOverride)
	}
}

// testLogger - This function returns a *log.Logger that discards output.
// OnChange calls Debugln unconditionally, so any test constructing a
// TreeListener directly, bypassing New, needs a non-nil logger or it
// panics, exactly like a nil l.instance does on the Ambiguous branch's
// print. See TestOnChangeAddsTrailingSpaceAfterUniqueCompletion.
func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// testTree - This function returns a tree shaped like the real root tree,
// where "enable" and "end" both exist at the same level and share the
// prefix "en", so typing "en" and pressing Tab is ambiguous between them,
// matching the shape the ambiguity tests in this file need.
func testTree() map[string]*command.Command {
	return map[string]*command.Command{
		"enable": {Desc: "Elevate this session"},
		"end":    {Desc: "Return to the top-level"},
		"exit":   {Desc: "Exit"},
		"show": {
			Subcommands: map[string]*command.Command{
				"version":        {Desc: "Show version"},
				"running-config": {Desc: "Show running config"},
			},
		},
		"configure": {
			Subcommands: map[string]*command.Command{
				"terminal": {Desc: "Enter config mode"},
			},
		},
		"terminal": {
			Subcommands: map[string]*command.Command{
				"length": {
					Desc:    "Set terminal length",
					RunFunc: func(*command.AppContext, []string) error { return nil },
					MinArgs: intPtr(1),
					MaxArgs: intPtr(1),
					ArgHelp: "<2-1000>  Enter a number for the 'length' command/parameter.",
				},
			},
		},
	}
}

// TestAmbiguousRewriteBufferPreservesPartiallyTypedAmbiguousToken - This
// test verifies that typing "en", ambiguous between "enable" and "end",
// leaves the rewritten buffer with the typed token preserved, not wiped
// back to empty. The rewritten buffer must be built from more than the
// already resolved prefix, res.FullName, since res.FullName is empty when
// the ambiguity falls on the very first token. There is no resolved prefix
// yet, only the ambiguous token itself, and it must not be dropped.
func TestAmbiguousRewriteBufferPreservesPartiallyTypedAmbiguousToken(t *testing.T) {
	tree := testTree()
	tokens := []string{"en"}
	res := command.Resolve(tree, tokens)

	if len(res.Ambiguous) != 2 {
		t.Fatalf("test setup problem: expected \"en\" to be ambiguous between 2 candidates, got %v", res.Ambiguous)
	}

	got := ambiguousRewriteBuffer(tokens, res, "")
	want := "en"
	if got != want {
		t.Errorf("ambiguousRewriteBuffer(%v) = %q, want %q (must preserve the typed token, not wipe it)", tokens, got, want)
	}
}

// TestAmbiguousRewriteBufferListsBothCandidates - This test verifies that
// Resolve itself, not ambiguousRewriteBuffer, is what reports both
// "enable" and "end" as candidates for the ambiguous prefix "en",
// confirming the test fixture exercises a genuine two-way ambiguity.
func TestAmbiguousRewriteBufferListsBothCandidates(t *testing.T) {
	tree := testTree()
	res := command.Resolve(tree, []string{"en"})

	want := map[string]bool{"enable": true, "end": true}
	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected 2 candidates, got %v", res.Ambiguous)
	}
	for _, c := range res.Ambiguous {
		if !want[c] {
			t.Errorf("unexpected candidate %q", c)
		}
	}
}

// TestAmbiguousRewriteBufferOnTrailingSpace - This test verifies the
// companion case to the "en" ambiguity: "show ", a trailing space and an
// empty last token, is ambiguous among all of show's children, and
// res.FullName is non-empty here, "show". The empty ambiguous token must
// still combine correctly with the trailing space in the rewritten buffer.
func TestAmbiguousRewriteBufferOnTrailingSpace(t *testing.T) {
	tree := testTree()
	tokens := []string{"show", ""} // what the completer feeds in for "show <TAB>"
	res := command.Resolve(tree, tokens)

	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected show's two children as candidates, got %v", res.Ambiguous)
	}

	got := ambiguousRewriteBuffer(tokens, res, "")
	want := "show "
	if got != want {
		t.Errorf("ambiguousRewriteBuffer(%v) = %q, want %q", tokens, got, want)
	}
}

// TestAmbiguousRewriteBufferExpandsResolvedPrefix - This test verifies
// that when part of the line does resolve unambiguously before hitting an
// ambiguous token, that part still gets expanded in the rewritten buffer.
// An ambiguous token does not mean nothing ever gets rewritten, only that
// the ambiguous part itself is left untouched. This covers the case where
// res.FullName is non-empty and there is still an ambiguous token after
// it.
func TestAmbiguousRewriteBufferExpandsResolvedPrefix(t *testing.T) {
	tree := map[string]*command.Command{
		"configure": {
			Subcommands: map[string]*command.Command{
				"aardvark": {Desc: "first"},
				"aardwolf": {Desc: "second"},
			},
		},
	}
	tokens := []string{"conf", "aar"} // "conf" uniquely expands to "configure"; "aar" is ambiguous
	res := command.Resolve(tree, tokens)

	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected 2 ambiguous candidates under configure, got %v", res.Ambiguous)
	}
	if len(res.FullName) != 1 || res.FullName[0] != "configure" {
		t.Fatalf("expected \"conf\" to have resolved to \"configure\", got %v", res.FullName)
	}

	got := ambiguousRewriteBuffer(tokens, res, "")
	want := "configure aar"
	if got != want {
		t.Errorf("ambiguousRewriteBuffer(%v) = %q, want %q (resolved prefix expanded, ambiguous token preserved)", tokens, got, want)
	}
}

// TestAmbiguousRewriteBufferHandlesNegatedIndexing - This test verifies
// the case where the line starts with "no". Resolve strips it before
// walking the tree, so res.AmbigAt indexes into tokens[1:], not the
// original tokens slice. Getting this index translation wrong would either
// panic with an index out of range or silently grab the wrong token, so
// this test exists specifically to pin that plus-one adjustment down.
func TestAmbiguousRewriteBufferHandlesNegatedIndexing(t *testing.T) {
	tree := map[string]*command.Command{
		"shutdown":   {Negatable: true},
		"showthings": {Negatable: true}, // shares a prefix with "shutdown" for a real ambiguity
	}
	tokens := []string{"no", "sh"}
	res := command.Resolve(tree, tokens)

	if !res.Negated {
		t.Fatal("expected Negated to be true for a \"no\"-prefixed line")
	}
	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected 2 ambiguous candidates, got %v", res.Ambiguous)
	}

	got := ambiguousRewriteBuffer(tokens, res, "no ")
	want := "no sh"
	if got != want {
		t.Errorf("ambiguousRewriteBuffer(%v) = %q, want %q", tokens, got, want)
	}
}

// TestAmbiguousRewriteBufferEmptyLineListsTopLevel - This test verifies
// that Tab pressed at a completely bare prompt produces an unchanged,
// empty rewritten buffer, since nothing was typed to preserve.
func TestAmbiguousRewriteBufferEmptyLineListsTopLevel(t *testing.T) {
	tree := testTree()
	tokens := []string{""} // what the completer feeds in for Tab at a bare prompt
	res := command.Resolve(tree, tokens)

	if len(res.Ambiguous) == 0 {
		t.Fatal("expected an empty line to list every top-level command as candidates")
	}

	got := ambiguousRewriteBuffer(tokens, res, "")
	want := ""
	if got != want {
		t.Errorf("ambiguousRewriteBuffer(%v) = %q, want %q (nothing typed, nothing to preserve)", tokens, got, want)
	}
}

// ----------------------------------------------------------------------
//
// Empty token ambiguity does not need the double Tab confirmation
//
// ----------------------------------------------------------------------

// TestAmbiguousTokenIsEmptyTrailingSpace - This test verifies the "show
// <TAB>" case, nothing typed for the next token yet: the ambiguous token
// itself is "", so ambiguousTokenIsEmpty must report true. This is what
// lets OnChange skip the double Tab confirmation and list "show"'s
// children on the very first Tab.
func TestAmbiguousTokenIsEmptyTrailingSpace(t *testing.T) {
	tree := testTree()
	tokens := []string{"show", ""}
	res := command.Resolve(tree, tokens)
	if len(res.Ambiguous) == 0 {
		t.Fatalf("test setup problem: expected show's children to be ambiguous, got %v", res.Ambiguous)
	}
	if !ambiguousTokenIsEmpty(tokens, res) {
		t.Error("expected ambiguousTokenIsEmpty to be true for a trailing space Tab (nothing typed yet)")
	}
}

// TestAmbiguousTokenIsEmptyPartialWord - This test verifies the
// contrasting case: "en", ambiguous between "enable" and "end", is a
// partial word, not an empty slot. ambiguousTokenIsEmpty must report false
// here so OnChange keeps requiring the double Tab confirmation for it.
func TestAmbiguousTokenIsEmptyPartialWord(t *testing.T) {
	tree := testTree()
	tokens := []string{"en"}
	res := command.Resolve(tree, tokens)
	if len(res.Ambiguous) == 0 {
		t.Fatalf("test setup problem: expected \"en\" to be ambiguous, got %v", res.Ambiguous)
	}
	if ambiguousTokenIsEmpty(tokens, res) {
		t.Error("expected ambiguousTokenIsEmpty to be false for a partially typed ambiguous token")
	}
}

// TestAmbiguousTokenIsEmptyBarePrompt - This test verifies Tab at a
// completely empty line, also an empty token, so it should also skip the
// double Tab confirmation.
func TestAmbiguousTokenIsEmptyBarePrompt(t *testing.T) {
	tree := testTree()
	tokens := []string{""}
	res := command.Resolve(tree, tokens)
	if !ambiguousTokenIsEmpty(tokens, res) {
		t.Error("expected ambiguousTokenIsEmpty to be true at a bare prompt")
	}
}

// TestAmbiguousTokenIsEmptyRespectsNegatedIndexing - This test verifies
// the same case as TestAmbiguousRewriteBufferHandlesNegatedIndexing. The
// same plus-one adjustment needs to be right here too, or this would
// either panic or silently check the wrong token for a "no " prefixed
// line.
func TestAmbiguousTokenIsEmptyRespectsNegatedIndexing(t *testing.T) {
	tree := map[string]*command.Command{
		"shutdown":   {Negatable: true},
		"showthings": {Negatable: true},
	}
	tokens := []string{"no", "sh"}
	res := command.Resolve(tree, tokens)
	if !res.Negated {
		t.Fatal("test setup problem: expected Negated true")
	}
	if ambiguousTokenIsEmpty(tokens, res) {
		t.Error("expected ambiguousTokenIsEmpty to be false, \"sh\" is a partial word, not empty")
	}
}

// ----------------------------------------------------------------------
//
// Unique completion always leaves a trailing space, so a second Tab
// press immediately after works with no further keystrokes
//
// ----------------------------------------------------------------------

// TestOnChangeAddsTrailingSpaceAfterUniqueCompletion - This test verifies that typing
// "sh" and pressing Tab completes to "show" with a trailing space, so a
// second Tab immediately after has something further to offer. Without
// a trailing space in the rewritten buffer, a second Tab would see an
// unchanged buffer and do nothing, forcing the user to type a literal
// space themselves first. This exercises the real OnChange method
// end-to-end, not just the pure helper, since the resolved line assembly
// this covers lives directly in OnChange.
//
// A real readline.Instance is not available in this package's tests.
// New needs one to print candidate lists on a double Tab, but this test
// does not require printing anything, since "sh" resolves unambiguously
// to "show", the Ambiguous branch is never reached, and a nil
// *readline.Instance is never dereferenced on that path. That is
// exactly why this is safe to construct directly rather than through
// New.
func TestOnChangeAddsTrailingSpaceAfterUniqueCompletion(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", testTree()), logger: testLogger()}

	line := []rune("sh")
	newLine, newPos, ok := l.OnChange(line, len(line), readline.CharTab)
	if !ok {
		t.Fatal("expected OnChange to rewrite the buffer for \"sh\" + Tab")
	}
	got := string(newLine)
	want := "show "
	if got != want {
		t.Errorf("OnChange(%q) = %q, want %q (must include the trailing space)", "sh", got, want)
	}
	if newPos != len([]rune(want)) {
		t.Errorf("OnChange(%q) cursor = %d, want %d (cursor should sit after the space)", "sh", newPos, len([]rune(want)))
	}

	// The critical second half of the regression was that pressing Tab
	// again immediately, on the buffer OnChange itself just produced, used
	// to report nothing changed, forcing the user to type a literal space
	// first. That decision now hinges on ambiguousTokenIsEmpty, tested
	// directly above in TestAmbiguousTokenIsEmptyTrailingSpace: "show "
	// resolves to an empty final token, so the list is shown on the very
	// first Tab, with no confirmation tap needed. Exercising that same
	// path through OnChange end-to-end would need a real
	// *readline.Instance to print through, l.instance.Stdout() panics on
	// the nil TreeListener built above, and chzyer/readline's Instance
	// requires actual raw-mode terminal control to construct, which is not
	// available in this test environment. TestAmbiguousTokenIsEmpty*
	// covers the decision; this test covers the trailing space fix that
	// decision depends on.
}

// TestOnChangeArgHelpHintDetection - This test verifies the Command-level
// conditions OnChange checks before showing an ArgHelp hint: a leaf
// command, RunFunc set with no Subcommands, with MinArgs set and a
// trailing space with nothing typed for the argument yet. This does not
// try to capture stdout, that needs a real readline.Instance, it confirms
// Resolve hands OnChange exactly the shape it needs to make that decision,
// that is, that "terminal length " resolves to the leaf command with a
// single empty trailing arg, rather than misfiring on the container
// "terminal" itself.
func TestOnChangeArgHelpHintDetection(t *testing.T) {
	tree := testTree()
	tokens := []string{"terminal", "length", ""} // "terminal length <TAB>"
	res := command.Resolve(tree, tokens)

	if res.Command == nil || res.Command.RunFunc == nil {
		t.Fatalf("expected \"terminal length\" to resolve to a runnable leaf node, got %+v", res)
	}
	if len(res.Command.Subcommands) != 0 {
		t.Fatalf("expected the leaf node to have no children, got %v", res.Command.Subcommands)
	}
	// Resolve dumps every remaining token into res.Args once it reaches a
	// childless node, so for "terminal length ", a trailing space appended
	// as its own empty token exactly like OnChange does, that is res.Args
	// == [""], not an empty slice. This is the actual signal OnChange's
	// ArgHelp hint condition checks for, see the comment on that condition
	// in completer.go.
	if len(res.Args) != 1 || res.Args[0] != "" {
		t.Fatalf("expected a single empty trailing arg token, got %v", res.Args)
	}
	if res.Command.MinArgs == nil {
		t.Fatal("expected MinArgs to be set on the \"length\" node (test setup)")
	}
	hint := res.Command.ResolvedArgHelp(nil)
	if !strings.Contains(hint, "<2-1000>") {
		t.Errorf("expected the configured ArgHelp text, got %q", hint)
	}
}

// TestOnChangeNonTabKeyResetsDoubleTapStateAndDeclinesToRewrite - This
// test verifies the branch that runs for any keypress other than Tab or
// "?": it must report ok=false, leaving readline's handling of that key
// untouched, and it must reset tapCount and lastAmbiguousInput, so a Tab
// press right after an unrelated key is never misread as the second half
// of a double Tab sequence on whatever was ambiguous before.
func TestOnChangeNonTabKeyResetsDoubleTapStateAndDeclinesToRewrite(t *testing.T) {
	l := &TreeListener{
		position:           command.NewCommandLevelStack("exec", "", testTree()),
		logger:             testLogger(),
		tapCount:           2,
		lastAmbiguousInput: "en",
	}

	line := []rune("x")
	newLine, newPos, ok := l.OnChange(line, len(line), 'x')
	if ok {
		t.Error("expected OnChange to report ok=false for an ordinary, non-Tab, non-'?' key")
	}
	if newLine != nil || newPos != 0 {
		t.Errorf("OnChange('x') = (%v, %d), want (nil, 0)", newLine, newPos)
	}
	if l.tapCount != 0 {
		t.Errorf("tapCount = %d, want 0 after a non-Tab key", l.tapCount)
	}
	if l.lastAmbiguousInput != "" {
		t.Errorf("lastAmbiguousInput = %q, want empty after a non-Tab key", l.lastAmbiguousInput)
	}
}

// TestOnChangeAmbiguousPartialWordTracksTapCountWithoutPrinting - This
// test verifies OnChange's dispatch into the Ambiguous branch for a
// partial, ambiguous word on the first Tab press: "en", ambiguous between
// "enable" and "end", must leave tapCount at 1 and record
// lastAmbiguousInput, without ever touching l.instance, since
// ambiguousTokenIsEmpty is false and tapCount has not yet reached 2, so
// the candidate list print is skipped. Nothing is left to expand either,
// the rewritten buffer equals what was already typed, so ok must be false.
// This is the OnChange-level counterpart to
// TestAmbiguousRewriteBufferPreservesPartiallyTypedAmbiguousToken, which
// only tests the pure helper directly.
func TestOnChangeAmbiguousPartialWordTracksTapCountWithoutPrinting(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", testTree()), logger: testLogger()}

	line := []rune("en")
	newLine, newPos, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, nothing to expand for a still-ambiguous partial word")
	}
	if newLine != nil || newPos != 0 {
		t.Errorf("OnChange(\"en\") = (%v, %d), want (nil, 0)", newLine, newPos)
	}
	if l.tapCount != 1 {
		t.Errorf("tapCount = %d, want 1 after the first Tab on an ambiguous word", l.tapCount)
	}
	if l.lastAmbiguousInput != "en" {
		t.Errorf("lastAmbiguousInput = %q, want %q", l.lastAmbiguousInput, "en")
	}
}

// TestOnChangeNegatedUniqueCompletionAddsNoPrefix - This test verifies
// that a "no "-prefixed line resolving uniquely is rewritten with the "no
// " prefix preserved and the real command name expanded after it,
// exercising OnChange's noPrefix and resolvedLine assembly for a negated
// line, not just Resolve()'s Negated flag in isolation.
func TestOnChangeNegatedUniqueCompletionAddsNoPrefix(t *testing.T) {
	tree := testTree()
	tree["logging"] = &command.Command{Desc: "Configure logging", Negatable: true}
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", tree), logger: testLogger()}

	line := []rune("no logging")
	newLine, newPos, ok := l.OnChange(line, len(line), readline.CharTab)
	if !ok {
		t.Fatal("expected OnChange to rewrite the buffer for \"no logging\" + Tab")
	}
	got := string(newLine)
	want := "no logging "
	if got != want {
		t.Errorf("OnChange(%q) = %q, want %q", "no logging", got, want)
	}
	if newPos != len([]rune(want)) {
		t.Errorf("OnChange(%q) cursor = %d, want %d", "no logging", newPos, len([]rune(want)))
	}
}

// ----------------------------------------------------------------------
//
// "?" help
//
// ----------------------------------------------------------------------

// TestHelpTokensAndRestoredBufferStripsQuestionMark - This test verifies
// that "show ?", with readline having already inserted "?" at the cursor,
// is turned into the token path ["show", ""] and a restored buffer with
// the "?" stripped back out.
func TestHelpTokensAndRestoredBufferStripsQuestionMark(t *testing.T) {
	// line is "show ?" with pos at the very end (6); '?' sits at index 5
	// (pos-1).
	line := []rune("show ?")
	tokens, restored, restoredPos := helpTokensAndRestoredBuffer(line, len(line))

	if got, want := string(restored), "show "; got != want {
		t.Errorf("restored buffer = %q, want %q (the '?' must be stripped back out)", got, want)
	}
	if restoredPos != len([]rune("show ")) {
		t.Errorf("restoredPos = %d, want %d (cursor should sit right after \"show \")", restoredPos, len([]rune("show ")))
	}
	if len(tokens) != 2 || tokens[0] != "show" || tokens[1] != "" {
		t.Errorf("tokens = %v, want [\"show\" \"\"] (trailing space means \"what comes next\")", tokens)
	}
}

// TestHelpTokensAndRestoredBufferBarePrompt - This test verifies just "?"
// at a bare prompt, resolving to a single empty token and an empty
// restored buffer.
func TestHelpTokensAndRestoredBufferBarePrompt(t *testing.T) {
	// pos is 1, '?' is the only rune.
	line := []rune("?")
	tokens, restored, restoredPos := helpTokensAndRestoredBuffer(line, len(line))

	if len(restored) != 0 {
		t.Errorf("restored buffer = %q, want empty", string(restored))
	}
	if restoredPos != 0 {
		t.Errorf("restoredPos = %d, want 0", restoredPos)
	}
	if len(tokens) != 1 || tokens[0] != "" {
		t.Errorf("tokens = %v, want a single empty token", tokens)
	}
}

// TestHelpTokensAndRestoredBufferPartialWord - This test verifies "sh?",
// with no space before the "?" and mid-word, resolving to the single token
// "sh".
func TestHelpTokensAndRestoredBufferPartialWord(t *testing.T) {
	line := []rune("sh?")
	tokens, restored, restoredPos := helpTokensAndRestoredBuffer(line, len(line))

	if got, want := string(restored), "sh"; got != want {
		t.Errorf("restored buffer = %q, want %q", got, want)
	}
	if restoredPos != len([]rune("sh")) {
		t.Errorf("restoredPos = %d, want %d", restoredPos, len([]rune("sh")))
	}
	if len(tokens) != 1 || tokens[0] != "sh" {
		t.Errorf("tokens = %v, want [\"sh\"]", tokens)
	}
}

// TestHelpTokensAndRestoredBufferMidLineInsertion - This test verifies
// "show ?running-config", "?" pressed with "running-config" still sitting
// after the cursor. The restored buffer must keep that trailing text
// untouched.
func TestHelpTokensAndRestoredBufferMidLineInsertion(t *testing.T) {
	line := []rune("show ?running-config")
	pos := len([]rune("show ?")) // cursor right after the '?'
	tokens, restored, restoredPos := helpTokensAndRestoredBuffer(line, pos)

	if got, want := string(restored), "show running-config"; got != want {
		t.Errorf("restored buffer = %q, want %q (text after the cursor must be preserved)", got, want)
	}
	if restoredPos != len([]rune("show ")) {
		t.Errorf("restoredPos = %d, want %d", restoredPos, len([]rune("show ")))
	}
	if len(tokens) != 2 || tokens[0] != "show" || tokens[1] != "" {
		t.Errorf("tokens = %v, want [\"show\" \"\"]", tokens)
	}
}

// ----------------------------------------------------------------------
//
// "help <command>" completes and shows "?" for its own argument
// against the current tree, rather than being left alone as a leaf
// command's free-form argument with nothing to complete
//
// ----------------------------------------------------------------------

// helpTestTree - This function returns testTree()'s commands plus a
// "help" entry, reachable by its own literal name the same way it is in
// the real shipped tree, example/var/tree/level_common.yaml, and a top level "set"
// command sharing the "s" prefix with "show", for a genuine two way
// ambiguity nested behind "help" too. This is what unwrapHelpTokens, and
// the OnChange and handleHelp tests built on it below, resolve against.
func helpTestTree() map[string]*command.Command {
	tree := testTree()
	tree["help"] = &command.Command{Desc: "Display available commands", ArgHelp: "[<word...>]  A command name."}
	tree["set"] = &command.Command{Desc: "Set something"}
	return tree
}

// TestUnwrapHelpTokensLeavesOrdinaryTokensAlone - This test verifies that
// a token path not starting with "help" at all passes through unchanged,
// with an empty prefix, confirming this unwrap is entirely inert for every
// ordinary command.
func TestUnwrapHelpTokensLeavesOrdinaryTokensAlone(t *testing.T) {
	tree := helpTestTree()
	tokens, prefix := unwrapHelpTokens(tree, []string{"show"})

	if len(tokens) != 1 || tokens[0] != "show" {
		t.Errorf("tokens = %v, want [\"show\"] unchanged", tokens)
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want empty", prefix)
	}
}

// TestUnwrapHelpTokensStopsWithNoArgsYet - This test verifies that "help"
// typed alone, nothing after it yet, is left as is: there is nothing to
// unwrap since res.Args is empty, matching plain "help" with no command
// name at all.
func TestUnwrapHelpTokensStopsWithNoArgsYet(t *testing.T) {
	tree := helpTestTree()
	tokens, prefix := unwrapHelpTokens(tree, []string{"help"})

	if len(tokens) != 1 || tokens[0] != "help" {
		t.Errorf("tokens = %v, want [\"help\"] unchanged", tokens)
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want empty", prefix)
	}
}

// TestUnwrapHelpTokensUnwrapsOneLevel - This test verifies the core case:
// "help con" resolves its leftover argument, "con", against the same tree
// "help" itself came from, unwrapping to just ["con"] with prefix "help ",
// rather than leaving ["con"] buried as "help"'s unresolved free-form
// argument.
func TestUnwrapHelpTokensUnwrapsOneLevel(t *testing.T) {
	tree := helpTestTree()
	tokens, prefix := unwrapHelpTokens(tree, []string{"help", "con"})

	if len(tokens) != 1 || tokens[0] != "con" {
		t.Errorf("tokens = %v, want [\"con\"]", tokens)
	}
	if prefix != "help " {
		t.Errorf("prefix = %q, want %q", prefix, "help ")
	}
}

// TestUnwrapHelpTokensStopsOnAmbiguousNestedResolution - This test
// verifies that once the nested tokens themselves resolve ambiguously, "s"
// here matching both "show" and "set", the unwrap stops and hands that
// ambiguous token back unresolved, along with the "help " prefix already
// earned, rather than trying to resolve past an ambiguity.
func TestUnwrapHelpTokensStopsOnAmbiguousNestedResolution(t *testing.T) {
	tree := helpTestTree()
	tokens, prefix := unwrapHelpTokens(tree, []string{"help", "s"})

	if len(tokens) != 1 || tokens[0] != "s" {
		t.Errorf("tokens = %v, want [\"s\"]", tokens)
	}
	if prefix != "help " {
		t.Errorf("prefix = %q, want %q", prefix, "help ")
	}
}

// TestUnwrapHelpTokensHandlesRepeatedHelp - This test verifies that "help
// help con", an odd but not meaningless line, unwraps twice, accumulating
// "help help " as the prefix, confirming the loop keeps going for as long
// as each successive resolution keeps landing back on the literal "help"
// command, not just once.
func TestUnwrapHelpTokensHandlesRepeatedHelp(t *testing.T) {
	tree := helpTestTree()
	tokens, prefix := unwrapHelpTokens(tree, []string{"help", "help", "con"})

	if len(tokens) != 1 || tokens[0] != "con" {
		t.Errorf("tokens = %v, want [\"con\"]", tokens)
	}
	if prefix != "help help " {
		t.Errorf("prefix = %q, want %q", prefix, "help help ")
	}
}

// TestIsHelpCommandMatchesOnlyTheRealHelpEntry - This test verifies
// isHelpCommand reports true only for the exact *Command stored under
// "help" in tree, false for any other command, and false, rather than
// panicking, against a tree with no "help" entry at all, the shape a
// Command Level built with skip_common: true would have.
func TestIsHelpCommandMatchesOnlyTheRealHelpEntry(t *testing.T) {
	tree := helpTestTree()

	if !isHelpCommand(tree, tree["help"]) {
		t.Error("expected isHelpCommand to be true for tree's \"help\" entry")
	}
	if isHelpCommand(tree, tree["show"]) {
		t.Error("expected isHelpCommand to be false for a different command")
	}

	noHelp := testTree()
	delete(noHelp, "help")
	if isHelpCommand(noHelp, noHelp["show"]) {
		t.Error("expected isHelpCommand to be false against a tree with no \"help\" entry")
	}
}

// TestOnChangeExpandsCommandNameAfterHelp - This test verifies that
// typing "help con" and pressing Tab expands to "help configure ", the
// same way typing "con" alone expands to "configure ".
//
// Without the unwrapping, everything after "help" is a leaf command's
// free-form argument with nothing to complete, and the buffer is left
// untouched.
// This never touches l.instance, "con" resolves uniquely, so the Ambiguous
// branch, the only one that prints, is never reached.
func TestOnChangeExpandsCommandNameAfterHelp(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", helpTestTree()), logger: testLogger()}

	line := []rune("help con")
	newLine, newPos, ok := l.OnChange(line, len(line), readline.CharTab)
	if !ok {
		t.Fatal("expected OnChange to rewrite the buffer for \"help con\" + Tab")
	}
	got := string(newLine)
	want := "help configure "
	if got != want {
		t.Errorf("OnChange(%q) = %q, want %q", "help con", got, want)
	}
	if newPos != len([]rune(want)) {
		t.Errorf("OnChange(%q) cursor = %d, want %d", "help con", newPos, len([]rune(want)))
	}
}

// TestOnChangeHelpAmbiguousPartialWordTracksTapCountWithoutPrinting - This
// test verifies the nested counterpart to
// TestOnChangeAmbiguousPartialWordTracksTapCountWithoutPrinting: "help s",
// ambiguous between "show" and "set" once unwrapped, tracks tapCount and
// lastAmbiguousInput exactly the same way a plain "s" would, keyed by the
// full typed line, "help s", and touches l.instance not at all on this
// first Tap.
func TestOnChangeHelpAmbiguousPartialWordTracksTapCountWithoutPrinting(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", helpTestTree()), logger: testLogger()}

	line := []rune("help s")
	newLine, newPos, ok := l.OnChange(line, len(line), readline.CharTab)
	if ok {
		t.Error("expected OnChange to report ok=false, nothing to expand for a still-ambiguous nested word")
	}
	if newLine != nil || newPos != 0 {
		t.Errorf("OnChange(\"help s\") = (%v, %d), want (nil, 0)", newLine, newPos)
	}
	if l.tapCount != 1 {
		t.Errorf("tapCount = %d, want 1 after the first Tab on an ambiguous nested word", l.tapCount)
	}
	if l.lastAmbiguousInput != "help s" {
		t.Errorf("lastAmbiguousInput = %q, want %q", l.lastAmbiguousInput, "help s")
	}
}

// TestOnChangeQuestionMarkAfterHelpWithUnknownNestedNameDoesNotPanicWithoutInstance
// - This test verifies handleHelp's use of unwrapHelpTokens: "help bogus?"
// unwraps to look up help for "bogus" against the tree directly, and since
// nothing named "bogus" exists, HelpForPath returns "", so handleHelp's
// Fprint call is skipped, the same reasoning
// TestOnChangeQuestionMarkDoesNotPanicWithoutInstance below already relies
// on, keeping this safe to run without a real *readline.Instance.
func TestOnChangeQuestionMarkAfterHelpWithUnknownNestedNameDoesNotPanicWithoutInstance(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", helpTestTree()), logger: testLogger()}

	line := []rune("help bogus?")
	newLine, newPos, ok := l.OnChange(line, len(line), '?')
	if !ok {
		t.Fatal("expected OnChange to report ok=true for '?' (it always restores the buffer)")
	}
	if got, want := string(newLine), "help bogus"; got != want {
		t.Errorf("OnChange('?') buffer = %q, want %q", got, want)
	}
	if newPos != len([]rune("help bogus")) {
		t.Errorf("OnChange('?') cursor = %d, want %d", newPos, len([]rune("help bogus")))
	}
}

// TestOnChangeQuestionMarkDoesNotPanicWithoutInstance - This test verifies
// the same reasoning as
// TestOnChangeAddsTrailingSpaceAfterUniqueCompletion, see that test's doc
// comment: this only exercises a path that never needs to print. An empty
// tree has no help text to show, so HelpForPath returns "" and
// handleHelp's Fprint call is skipped entirely, which is specifically what
// makes it safe to construct a TreeListener without a real
// *readline.Instance.
func TestOnChangeQuestionMarkDoesNotPanicWithoutInstance(t *testing.T) {
	l := &TreeListener{position: command.NewCommandLevelStack("exec", "", map[string]*command.Command{}), logger: testLogger()}

	line := []rune("bogus ?")
	newLine, newPos, ok := l.OnChange(line, len(line), '?')
	if !ok {
		t.Fatal("expected OnChange to report ok=true for '?' (it always restores the buffer)")
	}
	if got, want := string(newLine), "bogus "; got != want {
		t.Errorf("OnChange('?') buffer = %q, want %q", got, want)
	}
	if newPos != len([]rune("bogus ")) {
		t.Errorf("OnChange('?') cursor = %d, want %d", newPos, len([]rune("bogus ")))
	}
}
