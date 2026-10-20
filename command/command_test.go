// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"reflect"
	"testing"

	"github.com/gotermcli/routercli/i18n"
)

// testTree - This function gives Resolve() a small tree with a nested
// level and a hidden sibling, enough to exercise abbreviation, ambiguity,
// and hidden command matching without pulling in the full example tree
// from main.go.
func testTree() map[string]*Command {
	return map[string]*Command{
		"show": {
			Subcommands: map[string]*Command{
				"interface":      {},
				"running-config": {},
				"startup-config": {},
				"version":        {},
				"secret-debug":   {Hidden: true},
			},
		},
		"set": {
			Subcommands: map[string]*Command{
				"description": {},
			},
		},
		"help": {},
		"exit": {},
	}
}

// TestResolveExactMatch - This test verifies that a full, exactly typed
// command path resolves to itself with no leftover arguments.
func TestResolveExactMatch(t *testing.T) {
	res := Resolve(testTree(), []string{"show", "version"})
	want := []string{"show", "version"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
	if len(res.Args) != 0 {
		t.Errorf("Args = %v, want empty", res.Args)
	}
}

// TestResolveUniqueAbbreviation - This test verifies that "sh run"
// resolves the same as "show running-config", each token abbreviated
// independently against its own level of the tree.
func TestResolveUniqueAbbreviation(t *testing.T) {
	res := Resolve(testTree(), []string{"sh", "run"})
	want := []string{"show", "running-config"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
}

// TestResolveAmbiguous - This test verifies that a prefix matching more
// than one top-level command, here "s" against both "show" and "set", is
// reported as ambiguous with both candidates listed.
func TestResolveAmbiguous(t *testing.T) {
	res := Resolve(testTree(), []string{"s"})
	if len(res.Ambiguous) != 2 {
		t.Fatalf("expected 2 ambiguous candidates, got %v", res.Ambiguous)
	}
	want := []string{"set", "show"}
	if !reflect.DeepEqual(res.Ambiguous, want) {
		t.Errorf("Ambiguous = %v, want %v", res.Ambiguous, want)
	}
}

// TestResolveUnknownCommand - This test verifies that a token matching
// nothing in the tree falls through as an argument, with a nil Command
// rather than an error.
func TestResolveUnknownCommand(t *testing.T) {
	res := Resolve(testTree(), []string{"bogus"})
	if res.Command != nil {
		t.Errorf("expected nil Command for unknown command, got %+v", res.Command)
	}
	if !reflect.DeepEqual(res.Args, []string{"bogus"}) {
		t.Errorf("Args = %v, want [bogus] (unmatched token falls through as an argument)", res.Args)
	}
}

// TestResolveArgumentsPastLeaf - This test verifies that once a command
// has no children, everything typed after it is treated as arguments
// rather than further command matching.
func TestResolveArgumentsPastLeaf(t *testing.T) {
	res := Resolve(testTree(), []string{"show", "version", "extra", "stuff"})
	want := []string{"show", "version"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
	wantArgs := []string{"extra", "stuff"}
	if !reflect.DeepEqual(res.Args, wantArgs) {
		t.Errorf("Args = %v, want %v", res.Args, wantArgs)
	}
}

// TestResolveHiddenCommandNotAbbreviatable - This test verifies that a
// Hidden command cannot be reached through an abbreviated prefix, but
// still resolves normally when typed out in full.
func TestResolveHiddenCommandNotAbbreviatable(t *testing.T) {
	res := Resolve(testTree(), []string{"show", "secret"})
	if res.Command != nil && res.Command.Hidden {
		t.Errorf("hidden command should not be reachable via abbreviation, got %+v", res.FullName)
	}
	res2 := Resolve(testTree(), []string{"show", "secret-debug"})
	want := []string{"show", "secret-debug"}
	if !reflect.DeepEqual(res2.FullName, want) {
		t.Errorf("exact-match hidden command: FullName = %v, want %v", res2.FullName, want)
	}
}

// TestResolveHiddenSiblingDoesNotBlockUniqueVisibleAbbreviation - This test
// verifies that a Hidden command sharing a prefix with exactly one visible
// sibling does not make that sibling's abbreviation ambiguous.
//
// "test" and "testnothidden" both start with "tes", but "test" is Hidden, so
// "tes" must resolve to "testnothidden" as though "test" did not exist.
//
// TestResolveHiddenCommandNotAbbreviatable covers the other half: a Hidden
// command cannot be reached by abbreviation itself.
func TestResolveHiddenSiblingDoesNotBlockUniqueVisibleAbbreviation(t *testing.T) {
	tree := map[string]*Command{
		"test":          {Hidden: true},
		"testnothidden": {},
	}
	res := Resolve(tree, []string{"tes"})
	want := []string{"testnothidden"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v (hidden sibling must not create ambiguity)", res.FullName, want)
	}
	if len(res.Ambiguous) != 0 {
		t.Errorf("expected no ambiguity, got %v", res.Ambiguous)
	}
}

// TestResolveNoMatchUnderDescendedNodeKeepsParentCommandAsArgs - This test
// verifies that once a command with children has matched, "show" for
// instance, a following token matching none of its Subcommands does not lose
// the parent.
//
// Command must still carry "show", not nil, with the unmatched token and
// everything after it folded into Args. TestResolveUnknownCommand covers the
// same behavior at the top level, where there is no parent to preserve.
func TestResolveNoMatchUnderDescendedNodeKeepsParentCommandAsArgs(t *testing.T) {
	tree := testTree()
	res := Resolve(tree, []string{"show", "bogus", "eth0"})
	want := []string{"show"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
	if res.Command != tree["show"] {
		t.Errorf("Command = %+v, want the already matched %q command, not nil or something else", res.Command, "show")
	}
	wantArgs := []string{"bogus", "eth0"}
	if !reflect.DeepEqual(res.Args, wantArgs) {
		t.Errorf("Args = %v, want %v", res.Args, wantArgs)
	}
}

// TestResolveEmptyTokenListsChildren - This test verifies that an empty
// trailing token, what the completer feeds in for a trailing space such as
// "show <TAB>", surfaces every non-hidden child as an ambiguous match
// rather than matching nothing.
func TestResolveEmptyTokenListsChildren(t *testing.T) {
	res := Resolve(testTree(), []string{"show", ""})
	want := []string{"interface", "running-config", "startup-config", "version"}
	if !reflect.DeepEqual(res.Ambiguous, want) {
		t.Errorf("Ambiguous = %v, want %v (secret-debug must stay excluded)", res.Ambiguous, want)
	}
}

// TestResolveMultipleSubtreeOptionsNeverAutoPicksOne - This test verifies
// that Tab on "show " never silently completes to one of several sibling
// options, here interface, running-config, startup-config, and version.
//
// There is no correct way to guess which was meant, so the only correct
// behavior is to list them and require more typing. Cisco and HP behave the
// same way: auto completing an ambiguous choice would run the wrong command
// for anyone who pressed Enter straight after Tab.
//
// The single remaining child case is different, and covered by
// TestResolveUniqueAbbreviation. With one candidate, completing it is the
// only possible answer rather than a guess.
func TestResolveMultipleSubtreeOptionsNeverAutoPicksOne(t *testing.T) {
	tree := testTree()
	res := Resolve(tree, []string{"show", ""})

	// res.Command correctly stops at the last unambiguous match, "show"
	// itself. It must not be any of the 4 sibling candidates, since
	// resolving into one of them would be exactly the silent guess this
	// test exists to rule out.
	if res.Command != tree["show"] {
		t.Errorf("res.Command should be the \"show\" command itself (last unambiguous match), got %+v", res.Command)
	}
	if len(res.FullName) != 1 || res.FullName[0] != "show" {
		t.Errorf("FullName should stop at the last unambiguous token (\"show\"), got %v", res.FullName)
	}
	if len(res.Ambiguous) != 4 {
		t.Fatalf("expected all 4 sibling options as candidates, got %v", res.Ambiguous)
	}
	want := map[string]bool{"interface": true, "running-config": true, "startup-config": true, "version": true}
	for _, c := range res.Ambiguous {
		if !want[c] {
			t.Errorf("unexpected candidate %q in Ambiguous", c)
		}
	}
}

// TestResolveUniquePrefixesAmongManySiblings - This test verifies that a
// prefix narrowing to exactly one of several siblings still resolves
// immediately, with no reason to make the user type the rest.
//
// Ambiguity is about how many candidates match what has been typed, not how
// many siblings exist in total. It uses the same four commands as the test
// above, from the opposite direction.
func TestResolveUniquePrefixesAmongManySiblings(t *testing.T) {
	cases := []struct {
		typed string
		want  string
	}{
		{"int", "interface"},        // only sibling starting with "int"
		{"run", "running-config"},   // only sibling starting with "run"
		{"start", "startup-config"}, // only sibling starting with "start"
		{"v", "version"},            // only sibling starting with "v"
	}
	for _, c := range cases {
		res := Resolve(testTree(), []string{"show", c.typed})
		want := []string{"show", c.want}
		if !reflect.DeepEqual(res.FullName, want) {
			t.Errorf("Resolve(show, %q).FullName = %v, want %v", c.typed, res.FullName, want)
		}
		if len(res.Ambiguous) != 0 {
			t.Errorf("Resolve(show, %q) should not be ambiguous, got candidates %v", c.typed, res.Ambiguous)
		}
	}
}

// TestResolveSharedPrefixAmongSiblingsIsAmbiguous - This test covers the
// shared leading prefix case. "running-config" and "startup-config" both
// contain "config", but that is not a prefix: resolution matches from the
// start of a token, never a substring inside it.
//
// Nothing in the fixture tree collides on a real leading substring, so this
// pins that "s" resolves uniquely to "startup-config".
//
// It is deliberately brittle. Adding a second "s" prefixed sibling, "show
// summary" for instance, makes this test fail, which forces a decision about
// the new ambiguity rather than letting it change quietly.
func TestResolveSharedPrefixAmongSiblingsIsAmbiguous(t *testing.T) {
	res := Resolve(testTree(), []string{"show", "s"})
	want := []string{"show", "startup-config"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("Resolve(show, \"s\").FullName = %v, want %v (only one sibling starts with \"s\" today)", res.FullName, want)
	}
}

// TestValidateArgsMinMax - This test verifies that ValidateArgs rejects
// both too few and too many arguments against MinArgs and MaxArgs, and
// accepts a count right in between.
func TestValidateArgsMinMax(t *testing.T) {
	cmd := &Command{MinArgs: intPtr(1), MaxArgs: intPtr(1)}

	if err := ValidateArgs(cmd, nil); err == nil {
		t.Error("expected error for too few arguments, got nil")
	}
	if err := ValidateArgs(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many arguments, got nil")
	}
	if err := ValidateArgs(cmd, []string{"a"}); err != nil {
		t.Errorf("expected exactly 1 argument to be accepted, got error: %v", err)
	}
}

// TestValidateArgsUnsetMeansUnconstrained - This test verifies that the
// zero value Command, which is what every leaf command has unless it opts
// in, does not silently reject arguments. A plain int with a 0 default for
// MinArgs and MaxArgs would otherwise have caused exactly that.
func TestValidateArgsUnsetMeansUnconstrained(t *testing.T) {
	cmd := &Command{}
	if err := ValidateArgs(cmd, []string{"a", "b", "c"}); err != nil {
		t.Errorf("unset MinArgs/MaxArgs should allow any argument count, got error: %v", err)
	}
}

// TestValidateArgsMaxLength - This test verifies that ValidateArgs accepts
// an argument right at MaxArgLength and rejects one past it.
func TestValidateArgsMaxLength(t *testing.T) {
	cmd := &Command{MaxArgLength: 5}
	if err := ValidateArgs(cmd, []string{"short"}); err != nil {
		t.Errorf("5-char argument should fit a 5-char limit, got error: %v", err)
	}
	if err := ValidateArgs(cmd, []string{"toolong"}); err == nil {
		t.Error("expected error for an argument past MaxArgLength, got nil")
	}
}

// TestResolveNegatedCommand - This test locks down the core "no"
// mechanism. "no shutdown" must resolve to the same Command as "shutdown"
// alone, with Negated set to true and "no" itself never appearing anywhere
// in FullName. This is what lets a single handler serve both directions,
// see registry.go's AppContext.Negated doc comment.
func TestResolveNegatedCommand(t *testing.T) {
	tree := map[string]*Command{
		"shutdown": {Negatable: true},
	}
	res := Resolve(tree, []string{"no", "shutdown"})

	if !res.Negated {
		t.Error("Negated should be true for a \"no\" prefixed command")
	}
	want := []string{"shutdown"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v (must not include \"no\")", res.FullName, want)
	}
	if res.Command != tree["shutdown"] {
		t.Errorf("Command = %+v, want the shutdown command itself", res.Command)
	}
}

// TestResolveNegatedCommandWithAbbreviation - This test verifies that the
// real command name after "no" still goes through the normal abbreviation
// matching logic, so "no shut" resolves the same as "no shutdown" when
// "shutdown" is the only match.
func TestResolveNegatedCommandWithAbbreviation(t *testing.T) {
	tree := map[string]*Command{
		"shutdown": {Negatable: true},
	}
	res := Resolve(tree, []string{"no", "shut"})
	want := []string{"shutdown"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
	if !res.Negated {
		t.Error("Negated should be true")
	}
}

// TestResolveNegatedAmbiguous - This test verifies that Negated is still
// reported true even when the command name after "no" is itself ambiguous.
func TestResolveNegatedAmbiguous(t *testing.T) {
	tree := map[string]*Command{
		"shutdown":    {Negatable: true},
		"show-secret": {},
	}
	res := Resolve(tree, []string{"no", "sh"})
	if !res.Negated {
		t.Error("Negated should be true even when the rest is ambiguous")
	}
	if len(res.Ambiguous) != 2 {
		t.Errorf("expected 2 ambiguous candidates after \"no\", got %v", res.Ambiguous)
	}
}

// TestResolveNegatedUnknownCommand - This test verifies that Negated is
// still reported true even when the command name after "no" does not match
// anything at all.
func TestResolveNegatedUnknownCommand(t *testing.T) {
	tree := map[string]*Command{"shutdown": {Negatable: true}}
	res := Resolve(tree, []string{"no", "bogus"})
	if !res.Negated {
		t.Error("Negated should still be true even for an unknown command")
	}
	if res.Command != nil {
		t.Errorf("expected no Command for an unknown negated command, got %+v", res.Command)
	}
}

// TestResolveBareNoListsChildren - This test verifies that "no <TAB>", an
// empty token after "no", lists the same candidates as a bare Tab at this
// level would. The completer relies on this to offer completions after "no
// ".
func TestResolveBareNoListsChildren(t *testing.T) {
	tree := map[string]*Command{
		"shutdown":    {Negatable: true},
		"description": {Negatable: true},
	}
	res := Resolve(tree, []string{"no", ""})
	if !res.Negated {
		t.Error("Negated should be true")
	}
	if len(res.Ambiguous) != 2 {
		t.Errorf("expected both children listed after bare \"no \", got %v", res.Ambiguous)
	}
}

// TestResolveNonNegatedUnaffected - This test verifies that a plain
// command with no "no" prefix has Negated false, guarding against a
// regression where Negated accidentally defaults to something other than
// false.
func TestResolveNonNegatedUnaffected(t *testing.T) {
	tree := map[string]*Command{"shutdown": {Negatable: true}}
	res := Resolve(tree, []string{"shutdown"})
	if res.Negated {
		t.Error("Negated should be false when the line did not start with \"no\"")
	}
}

// TestResolveChasesAliasToRealCommand - This test verifies that an exact
// match on an alias entry, the "?" key pointing at "help" the way
// help_test.go's tree shapes it, resolves to the real target command's
// Command, not the alias entry itself. FullName still carries the
// literally typed token, "?", not the alias target's name, since that is
// what the user typed and what a rewritten completion buffer should show.
func TestResolveChasesAliasToRealCommand(t *testing.T) {
	help := &Command{Desc: "Display available commands", RunFunc: func(*AppContext, []string) error { return nil }}
	tree := map[string]*Command{
		"help": help,
		"?":    {Alias: "help", Hidden: true},
	}

	res := Resolve(tree, []string{"?"})
	if res.Command != help {
		t.Errorf("expected \"?\" to resolve to the real \"help\" Command through its alias, got %+v", res.Command)
	}
	if len(res.FullName) != 1 || res.FullName[0] != "?" {
		t.Errorf("FullName = %v, want [\"?\"] (the literally typed token, not the alias target)", res.FullName)
	}
}

// TestCommandResolvedDescHelpArgHelp - This test verifies that
// ResolvedDesc, ResolvedHelp, and ResolvedArgHelp each prefer their
// translated form when a *Translator and the matching *Key field are both
// set, and fall back to the literal field, Desc, Help, or ArgHelp, when
// the translator is nil, matching how a tree with i18n never wired in at
// all is meant to work.
func TestCommandResolvedDescHelpArgHelp(t *testing.T) {
	catalog := i18n.Catalog{
		"show.desc":    "translated desc",
		"show.help":    "translated help",
		"show.arghelp": "translated arghelp",
	}
	tr := i18n.New(map[string]i18n.Catalog{"en": catalog}, "en", "en")

	n := &Command{
		Desc: "literal desc", DescKey: "show.desc",
		Help: "literal help", HelpKey: "show.help",
		ArgHelp: "literal arghelp", ArgHelpKey: "show.arghelp",
	}

	if got := n.ResolvedDesc(tr); got != "translated desc" {
		t.Errorf("ResolvedDesc with a translator = %q, want %q", got, "translated desc")
	}
	if got := n.ResolvedHelp(tr); got != "translated help" {
		t.Errorf("ResolvedHelp with a translator = %q, want %q", got, "translated help")
	}
	if got := n.ResolvedArgHelp(tr); got != "translated arghelp" {
		t.Errorf("ResolvedArgHelp with a translator = %q, want %q", got, "translated arghelp")
	}

	if got := n.ResolvedDesc(nil); got != "literal desc" {
		t.Errorf("ResolvedDesc with a nil translator = %q, want the literal Desc %q", got, "literal desc")
	}
	if got := n.ResolvedHelp(nil); got != "literal help" {
		t.Errorf("ResolvedHelp with a nil translator = %q, want the literal Help %q", got, "literal help")
	}
	if got := n.ResolvedArgHelp(nil); got != "literal arghelp" {
		t.Errorf("ResolvedArgHelp with a nil translator = %q, want the literal ArgHelp %q", got, "literal arghelp")
	}
}

// ----------------------------------------------------------------------
//
// RunnableAsIs and the "<cr>" signal
//
// ----------------------------------------------------------------------

// intPtr - This function returns a pointer to the int passed in. It exists
// so a test can write MinArgs: intPtr(1) inline instead of needing a named
// variable for every argument constraint, since Go will not take the
// address of a literal directly. A production tree definition comes from
// YAML, loader.go, where yaml.v3 already unmarshals directly into *int
// fields without needing this helper, which is why this lives in a test
// file rather than in the package itself.
func intPtr(n int) *int {
	return &n
}

// noopRun is a RunFunc stand-in for tests that only care whether a Command
// counts as runnable, never what running it does.
func noopRun(*AppContext, []string) error { return nil }

// runnableAsIsTestTree - This function mirrors the shape of
// example/var/tree/level_user.yaml's "totp" branch that motivated RunnableAsIs in
// the first place: "enable" is itself a complete, runnable command,
// exactly like real "totp enable", but it also has exactly one subcommand,
// "qr", below it, exactly like real "totp enable qr". "exit" is a plain,
// argumentless leaf with no subcommands at all, and "length" takes a
// required argument.
func runnableAsIsTestTree() map[string]*Command {
	return map[string]*Command{
		"totp": {
			Subcommands: map[string]*Command{
				"enable": {
					RunFunc: noopRun,
					Subcommands: map[string]*Command{
						"qr": {RunFunc: noopRun},
					},
				},
			},
		},
		"exit": {RunFunc: noopRun},
		"terminal": {
			Subcommands: map[string]*Command{
				"length": {RunFunc: noopRun, MinArgs: intPtr(1), ArgHelp: "<2-1000>"},
			},
		},
	}
}

// TestResolveRunnableAsIsTrueForSatisfiedLeaf - This test verifies that a
// plain, argumentless leaf command, typed in full with nothing left over,
// reports RunnableAsIs true, the ordinary "<cr>" case.
func TestResolveRunnableAsIsTrueForSatisfiedLeaf(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"exit"})
	if !res.RunnableAsIs {
		t.Error("RunnableAsIs = false, want true for a fully typed, argumentless leaf")
	}
}

// TestResolveRunnableAsIsIgnoresSyntheticTrailingEmptyToken - This test
// verifies that the synthetic "" token completer.OnChange and
// HelpForPath's caller append for "nothing typed yet here", a trailing
// space after "exit", does not itself count as an unsatisfied argument.
// Without stripping it, runnableAsIs would report "exit " as not runnable
// the instant a trailing space appears, purely because that placeholder
// landed in Args with nowhere else to go.
func TestResolveRunnableAsIsIgnoresSyntheticTrailingEmptyToken(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"exit", ""})
	if !res.RunnableAsIs {
		t.Error("RunnableAsIs = false, want true: a trailing synthetic \"\" token is not a real argument")
	}
}

// TestResolveRunnableAsIsFalseForUnsatisfiedMinArgs - This test verifies
// that a leaf command requiring an argument it has not yet been given,
// "terminal length" alone, reports RunnableAsIs false, since pressing
// Enter right now would fail MinArgs.
func TestResolveRunnableAsIsFalseForUnsatisfiedMinArgs(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"terminal", "length"})
	if res.RunnableAsIs {
		t.Error("RunnableAsIs = true, want false: MinArgs is not satisfied yet")
	}
}

// TestResolveRunnableAsIsFalseForPureContainer - This test verifies that a
// container command with no RunFunc of its own, "terminal", is never
// RunnableAsIs regardless of Args, since there is nothing to run.
func TestResolveRunnableAsIsFalseForPureContainer(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"terminal"})
	if res.RunnableAsIs {
		t.Error("RunnableAsIs = true, want false: a pure container has no RunFunc to run")
	}
}

// TestResolveRunnableContainerWithSoleSubcommandStaysAmbiguousInsteadOfAutoDescending
// - This test verifies that "totp enable " followed by Tab or "?", tokens
// ending in the synthetic empty string, does not auto-complete into "qr",
// its sole remaining subcommand.
//
// Descending would hide that "enable" is already a complete, runnable
// command. It must surface through the same Ambiguous path genuine ambiguity
// uses, so a caller lists "qr" alongside "<cr>". Cisco and HP show both
// together in exactly this situation.
func TestResolveRunnableContainerWithSoleSubcommandStaysAmbiguousInsteadOfAutoDescending(t *testing.T) {
	tree := runnableAsIsTestTree()
	res := Resolve(tree, []string{"totp", "enable", ""})

	wantFullName := []string{"totp", "enable"}
	if !reflect.DeepEqual(res.FullName, wantFullName) {
		t.Errorf("FullName = %v, want %v", res.FullName, wantFullName)
	}
	if want := []string{"qr"}; !reflect.DeepEqual(res.Ambiguous, want) {
		t.Errorf("Ambiguous = %v, want %v (must not silently auto-descend into the sole subcommand)", res.Ambiguous, want)
	}
	if res.Command != tree["totp"].Subcommands["enable"] {
		t.Error("Command does not point at the \"enable\" command")
	}
	if !res.RunnableAsIs {
		t.Error("RunnableAsIs = false, want true: \"totp enable\" is already a complete command")
	}
	if res.AmbiguousTree == nil || res.AmbiguousTree["qr"] != tree["totp"].Subcommands["enable"].Subcommands["qr"] {
		t.Error("AmbiguousTree does not point at \"enable\"'s Subcommands map")
	}
}

// TestResolveNonRunnableContainerWithSoleSubcommandStillAutoDescends -
// This
// test verifies the fix above is scoped correctly: a container with
// exactly one subcommand that is NOT itself runnable, "terminal" here, has
// only ever had one meaning for a trailing Tab, descend into that sole
// child, real Cisco and HP's ordinary abbreviation completion behavior.
// This must keep working.
func TestResolveNonRunnableContainerWithSoleSubcommandStillAutoDescends(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"terminal", ""})
	if len(res.Ambiguous) != 0 {
		t.Errorf("Ambiguous = %v, want none: \"terminal\" is not itself runnable, so its sole child should still auto-complete", res.Ambiguous)
	}
	want := []string{"terminal", "length"}
	if !reflect.DeepEqual(res.FullName, want) {
		t.Errorf("FullName = %v, want %v", res.FullName, want)
	}
}

// TestResolveRunnableAsIsPartialWordAmbiguityStillReported - This test
// verifies that a partial, ambiguous word is completely unaffected by the
// case 1 auto-descend fix above, which only ever triggers for an empty
// token. "t" matching both "totp" and "terminal" at the top level must
// still report both as Ambiguous the ordinary way.
func TestResolveRunnableAsIsPartialWordAmbiguityStillReported(t *testing.T) {
	res := Resolve(runnableAsIsTestTree(), []string{"t"})
	want := []string{"terminal", "totp"}
	if !reflect.DeepEqual(res.Ambiguous, want) {
		t.Errorf("Ambiguous = %v, want %v", res.Ambiguous, want)
	}
}
