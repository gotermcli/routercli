// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/tokenize"
)

func init() {
	command.Register("show.running-config", func(ctx *command.AppContext, args []string) error {
		ctx.Logger.Debugln("DEBUG: generating running-config output")
		state := ctx.State.(*State)
		for _, line := range runningConfigLines(ctx, state) {
			fmt.Println(line)
		}
		return nil
	})

	// "show" has several siblings sharing no forced prefix, so
	// "show <TAB>" has candidates including interface, running-config, and
	// startup-config. None should ever be auto-picked. The unit test
	// TestResolveMultipleSubtreeOptionsNeverAutoPicksOne locks that down.
	command.Register("show.interface", func(ctx *command.AppContext, args []string) error {
		state := ctx.State.(*State)
		if len(state.Interfaces) == 0 {
			fmt.Println(ctx.Translator.T("show.interface.text"))
			return nil
		}
		for _, name := range sortedInterfaceNames(state) {
			iface := state.Interfaces[name]
			status := ctx.Translator.T("show.interface.up")
			if iface.Shutdown {
				status = ctx.Translator.T("show.interface.admin_down")
			}
			fmt.Printf("%s: %s\n", name, status)
			if iface.Description != "" {
				fmt.Println("  " + ctx.Translator.T("show.interface.description_label", iface.Description))
			}
		}
		return nil
	})

	command.Register("show.startup-config", func(ctx *command.AppContext, args []string) error {
		data, err := os.ReadFile(ctx.StartupConfigFile)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println(ctx.Translator.T("show.startup_config.text"))
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("show.startup_config.read_failed", err))
		}
		text := string(data)
		if !currentLevelShowsSecrets(ctx) {
			text = redactPasswordManagerHashLines(text)
		}
		fmt.Print(text)
		return nil
	})
}

// runningConfigLines - The output of "show running-config" is meant to be
// pasted back into a fresh session and reproduce the same state. That only
// works if the order is right, not just the content.
//
// This builds that output as ordered lines, one command per line, covering
// the hostname, per interface state, and the top level description. Values
// go through tokenize.QuoteIfNeeded so they survive the round trip, and an
// unset value is omitted, the same as a real router.
//
// It is also exactly what "write memory" writes, so there is never a
// second, slightly different version of the same text on disk.
//
// The paste is assumed to start cold at the base level, which is what the
// boot time replay does too. Each alias is therefore wrapped in whatever
// reaches its own Command Level:
//
//   - base aliases first, unwrapped, since that is where a paste starts.
//   - user aliases next, wrapped in "user" and "end".
//   - everything else is exec rooted: "set description", exec aliases,
//     admin and diagnostic aliases in their own enter and exit blocks, and
//     the config mode output wrapped in "configure terminal" and "end".
//
// The exec rooted content needs "enable" first, so this prepends that line
// itself whenever any of it follows.
func runningConfigLines(ctx *command.AppContext, state *State) []string {
	lines := []string{"! (example running-config)"}

	if baseLevel, ok := ctx.Levels.ByName["base"]; ok {
		lines = append(lines, aliasLinesForLevel(baseLevel)...)
	}
	lines = append(lines, wrappedLevelAliasLines(ctx, "user")...)

	var execRootedLines []string

	if state.Description != "" {
		execRootedLines = append(execRootedLines, "set description "+tokenize.QuoteIfNeeded(state.Description))
	}
	if execLevel, ok := ctx.Levels.ByName["exec"]; ok {
		execRootedLines = append(execRootedLines, aliasLinesForLevel(execLevel)...)
	}
	execRootedLines = append(execRootedLines, wrappedLevelAliasLines(ctx, "admin")...)
	execRootedLines = append(execRootedLines, wrappedLevelAliasLines(ctx, "diagnostic")...)

	configLines := configModeLines(ctx, state)
	if len(configLines) > 0 {
		enter, ok := configEnterWords(ctx)
		if ok {
			execRootedLines = append(execRootedLines, strings.Join(enter, " "))
		}
		execRootedLines = append(execRootedLines, configLines...)
		if ok {
			execRootedLines = append(execRootedLines, "end")
		}
	}

	if len(execRootedLines) > 0 {
		if enter, ok := execEnterWords(ctx); ok {
			lines = append(lines, strings.Join(enter, " "))
		}
		lines = append(lines, execRootedLines...)
	}

	lines = append(lines, "!")
	return lines
}

// levelPasswordLines - This function renders the "password manager hash"
// line for every Command Level that has one, in tree order.
//
// Whether a real hash or a "<HIDDEN>" placeholder is written is decided
// once, by which Command Level the session is in right now, not per
// rendered level. The point of admin is that a session which already
// proved a live credential there sees everything, rather than a selective
// peek at one level at a time.
//
// A VendorRestricted level is skipped entirely rather than redacted. Its
// state goes to vendorConfigLines instead.
func levelPasswordLines(ctx *command.AppContext) []string {
	var lines []string
	show := currentLevelShowsSecrets(ctx)

	for _, level := range ctx.Levels.Order {
		switch {
		case level.VendorRestricted:
			// Never rendered here, in any form, regardless of show; see
			// vendorConfigLines.
			continue
		case level.VendorDefinedPasswordHash != "":
			// A level with VendorDefinedPasswordHash set but not
			// VendorRestricted is refused at startup, so this is
			// unreachable through any validly loaded tree.
			//
			// "<HIDDEN>" here is defense in depth for a CommandLevel built
			// directly in Go, bypassing that check. Rendering must never be
			// the path that leaks a secret a stricter check elsewhere
			// assumed was unreachable.
			//
			// "<HIDDEN>" is not a recognized hash, so pasting this line
			// back in is refused rather than corrupting the real secret.
			lines = append(lines, "password manager hash <HIDDEN>")
		case level.PasswordHash != "" && show:
			lines = append(lines, "password manager hash "+level.PasswordHash)
		case level.PasswordHash != "":
			lines = append(lines, "password manager hash <HIDDEN>")
		}
	}

	return lines
}

// interfaceBlockLines - This function renders one "interface <name>" block
// per configured interface, sorted.
//
// An interface with no description and no shutdown was never configured,
// so no empty block is printed, the same "nothing configured, nothing
// shown" convention the rest of this file follows.
//
// Each block ends with "exit" except the last, which needs it only when
// something else still follows. hasTrailingBlock is what says so.
func interfaceBlockLines(ctx *command.AppContext, state *State, hasTrailingBlock bool) []string {
	var configuredNames []string
	for _, name := range sortedInterfaceNames(state) {
		iface := state.Interfaces[name]
		if iface.Description == "" && !iface.Shutdown {
			continue
		}
		configuredNames = append(configuredNames, name)
	}

	var lines []string
	for i, name := range configuredNames {
		iface := state.Interfaces[name]
		if enter, ok := interfaceEnterWords(ctx, name); ok {
			lines = append(lines, strings.Join(enter, " "))
		} else {
			lines = append(lines, "interface "+name)
		}
		if iface.Description != "" {
			lines = append(lines, " description "+tokenize.QuoteIfNeeded(iface.Description))
		}
		if iface.Shutdown {
			lines = append(lines, " shutdown")
		}
		if i != len(configuredNames)-1 || hasTrailingBlock {
			lines = append(lines, "exit")
		}
	}

	return lines
}

// lineModeLines - This function renders the "line" block, which comes last,
// matching where Cisco and HP put "line vty" and "line console" in
// running-config. A field left nil is omitted.
//
// Every config-line alias renders inside this same block rather than one of
// its own. Unlike config-if, config-line is a single deployment wide
// instance, so there is no reason to enter and leave it twice.
func lineModeLines(ctx *command.AppContext, state *State, configLineLevel *command.CommandLevel) []string {
	var lines []string

	if enter, ok := lineEnterWords(ctx); ok {
		lines = append(lines, strings.Join(enter, " "))
	} else {
		lines = append(lines, "line")
	}
	if state.Line.Length != nil {
		lines = append(lines, " length "+strconv.Itoa(*state.Line.Length))
	}
	if state.Line.Width != nil {
		lines = append(lines, " width "+strconv.Itoa(*state.Line.Width))
	}
	if state.Line.Paging != nil {
		if *state.Line.Paging {
			lines = append(lines, " paging")
		} else {
			lines = append(lines, " no paging")
		}
	}
	for _, line := range aliasLinesForLevel(configLineLevel) {
		lines = append(lines, " "+line)
	}

	return lines
}

// configModeLines - "show running-config" has to reproduce state that can
// only be set from inside config mode. Those lines are grouped together so
// the output can be pasted back in as one block.
//
// This returns the hostname, each Command Level's "password manager"
// secret, every runtime defined alias, one block per configured interface,
// and last a "line" block for the deployment wide terminal defaults. It
// does not decide how a caller reaches config mode.
//
// "terminal length" and "terminal width" are not reproduced. Those are
// session scoped, and Cisco and HP do not persist them either. The "line"
// values are a different thing: the deployment default, which they do
// persist.
//
// Every interface block except the last is followed by "exit", because
// "interface" is only accepted from config mode itself, not from inside a
// previous interface. The last does not need it; the trailing "end" pops
// back from any depth.
//
// A "password manager hash" line renders the real hash only when the
// session is somewhere with ShowSecrets set, admin being the one shipped
// level that has it. Everywhere else it renders "<HIDDEN>". A
// VendorRestricted level renders neither; that goes to vendorConfigLines.
//
// One limitation remains. Every level's password line is emitted in one
// flat list after "configure terminal", rather than each inside a sequence
// that reenters its own level. Since "password manager hash" sets the
// secret for whichever level is current, pasting this back affects only
// the level the session is sitting in. Aliases do not have this problem,
// because each is already wrapped in the words that reach its own level.
func configModeLines(ctx *command.AppContext, state *State) []string {
	var lines []string

	if state.Hostname != "" {
		lines = append(lines, "hostname "+tokenize.QuoteIfNeeded(state.Hostname))
	}
	if state.BannerMOTD != "" {
		lines = append(lines, "banner motd "+tokenize.QuoteIfNeeded(state.BannerMOTD))
	}
	if state.BannerLogin != "" {
		lines = append(lines, "banner login "+tokenize.QuoteIfNeeded(state.BannerLogin))
	}
	// show is decided once, by which Command Level the session is in right
	// now, not per rendered level. See this function's doc comment for the
	// reasoning, and for why a VendorRestricted level is skipped rather
	// than redacted.
	lines = append(lines, levelPasswordLines(ctx)...)
	// Every runtime defined command alias belonging to config itself, see
	// command.CommandLevel.Aliases and example/cmd/core/cmd_alias.go, renders here
	// as one "alias <name> <word...>" line per alias, with no wrapper of
	// its own needed, since this whole function already runs inside a
	// "configure terminal" paste. config-if and config-line aliases are
	// handled separately, further down, since each needs its own short
	// enter, exit block first.
	if configLevel, ok := ctx.Levels.ByName["config"]; ok {
		lines = append(lines, aliasLinesForLevel(configLevel)...)
	}

	// hasConfigIfAliasBlock and hasLineBlock are computed before the
	// interface loop because the last interface block needs to know whether
	// anything follows it.
	//
	// That block omits its trailing "exit" only when nothing does. With
	// either of these coming next, it has to leave config-if first, the same
	// as every interior block.
	configIfLevel := ctx.Levels.ByName["config-if"]
	hasConfigIfAliasBlock := configIfLevel != nil && len(configIfLevel.Aliases) > 0
	configLineLevel := ctx.Levels.ByName["config-line"]
	hasLineBlock := state.Line.Length != nil || state.Line.Width != nil || state.Line.Paging != nil || (configLineLevel != nil && len(configLineLevel.Aliases) > 0)
	hasTrailingBlock := hasConfigIfAliasBlock || hasLineBlock

	lines = append(lines, interfaceBlockLines(ctx, state, hasTrailingBlock)...)
	// An alias defined inside config-if belongs to the whole config-if
	// level, not to one interface, the same way a level password does.
	//
	// Replaying it therefore only needs some interface name to walk
	// through, any name at all, so this uses a name reserved for the
	// purpose rather than one a session actually configured.
	//
	// Entering and leaving config-if this way never touches
	// state.Interfaces, so it never creates a visible interface as a side
	// effect.
	if hasConfigIfAliasBlock {
		if enter, ok := interfaceEnterWords(ctx, configIfAliasReplayInterfaceName); ok {
			lines = append(lines, "! the interface named below exists only to replay config-if command aliases; it carries no configuration of its own")
			lines = append(lines, strings.Join(enter, " "))
			lines = append(lines, aliasLinesForLevel(configIfLevel)...)
			if hasLineBlock {
				lines = append(lines, "exit")
			}
		}
	}

	// The "line" block renders last, matching where Cisco and HP put "line
	// vty" and "line console" in running-config.
	//
	// A field left nil is omitted, the same convention every other optional
	// value here follows. Config-line aliases render inside this block too,
	// since config-line is a single deployment wide instance.
	//
	// hasLineBlock above is false, and this is skipped entirely, only when
	// all three fields are nil and there are no aliases either.
	if hasLineBlock {
		lines = append(lines, lineModeLines(ctx, state, configLineLevel)...)
	}
	return lines
}

// configIfAliasReplayInterfaceName - This constant names the interface
// configModeLines walks through solely to replay a config-if scoped
// command alias back in, see that function's doc comment. It is
// distinctive, so it never collides with a name a real deployment would
// give one of its own interfaces, and it never appears in state.Interfaces
// at all, since merely entering and leaving config-if touches no interface
// state, see cmd_interface.go.
const configIfAliasReplayInterfaceName = "alias-replay-placeholder"

// aliasLinesForLevel - This function returns one "alias <name> <word...>"
// line per entry in level's Aliases map, sorted by alias name, since a Go
// map has no ordering of its own.
//
// It returns nil when level is nil, which happens whenever a deployment's
// tree structure never declared the level being asked about.
func aliasLinesForLevel(level *command.CommandLevel) []string {
	if level == nil || len(level.Aliases) == 0 {
		return nil
	}
	names := make([]string, 0, len(level.Aliases))
	for name := range level.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		words := make([]string, 0, len(level.Aliases[name]))
		for _, w := range level.Aliases[name] {
			words = append(words, tokenize.QuoteIfNeeded(w))
		}
		lines = append(lines, "alias "+name+" "+strings.Join(words, " "))
	}
	return lines
}

// wrappedLevelAliasLines - This function returns everything needed to
// replay a level's aliases back in: the words that enter the level, one
// alias line each, and the words that leave it.
//
// It works the same however the level is reached. levelExitWords picks up
// whichever exit the level declares, or falls back to "end".
//
// It returns nil when the level does not exist, has no aliases, or when
// either the enter or exit words cannot be discovered. Leaving the wrapper
// out entirely is safer than guessing at it.
func wrappedLevelAliasLines(ctx *command.AppContext, levelName string) []string {
	if ctx.Levels == nil {
		return nil
	}
	level, ok := ctx.Levels.ByName[levelName]
	if !ok || len(level.Aliases) == 0 {
		return nil
	}
	enter, ok := levelEnterWords(ctx, levelName)
	if !ok {
		return nil
	}
	exit, ok := levelExitWords(ctx, levelName)
	if !ok {
		return nil
	}

	lines := []string{strings.Join(enter, " ")}
	lines = append(lines, aliasLinesForLevel(level)...)
	lines = append(lines, strings.Join(exit, " "))
	return lines
}

// currentLevelShowsSecrets - This function reports whether the session's
// current Command Level has ShowSecrets set. admin is the one shipped
// level that does.
//
// Both "show running-config" and "show startup-config" call it, so an
// ordinary secret redacts the same way whichever command a session runs.
//
// It reads the property off the current level rather than comparing
// against a hardcoded "admin", so a deployment renaming that level never
// needs to touch this.
//
// It returns false whenever the level cannot be resolved at all.
func currentLevelShowsSecrets(ctx *command.AppContext) bool {
	if ctx.Levels == nil || ctx.Session == nil {
		return false
	}
	level, ok := ctx.Levels.ByName[ctx.Session.CommandLevel]
	if !ok {
		return false
	}
	return level.ShowSecrets
}

// redactPasswordManagerHashLines - This function replaces every
// "password manager hash" line's hash with "<HIDDEN>", leaving indentation
// and every other line alone.
//
// "show startup-config" needs it because it reads the saved file directly
// rather than rebuilding from state. "write memory" always writes the real
// hash to that file, so the redaction has to happen on the way back out to
// a session that has not proven a credential.
//
// It never touches a VendorRestricted level, since that content never
// reaches the startup-config file at all.
func redactPasswordManagerHashLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "password manager hash ") {
			indent := line[:len(line)-len(trimmed)]
			lines[i] = indent + "password manager hash <HIDDEN>"
		}
	}
	return strings.Join(lines, "\n")
}

// vendorConfigLines - This function builds the content of
// VendorConfigFile: one "password manager hash <hash>" line, unredacted,
// for every Command Level that is both VendorRestricted and carries a
// VendorDefinedPasswordHash.
//
// The lines are wrapped in whatever a session must type to reach config
// mode, the same wrapping runningConfigLines uses, since every level the
// shipped tree marks VendorRestricted lives under config. A level with no
// VendorDefinedPasswordHash contributes nothing, and this returns nil when
// no level qualifies.
//
// This shares configModeLines' limitation: the line is not positioned
// inside a paste sequence that reenters that specific level first.
//
// VendorConfigFile is a deployment's durable record of a vendor's baked in
// secret, readable and restorable by whoever manages that side of the
// deployment, never a file an ordinary end user is handed. It appears in
// neither show command, and neither "erase startup-config" nor
// "restore-factory-defaults" removes it.
func vendorConfigLines(ctx *command.AppContext) []string {
	var restricted []string
	for _, level := range ctx.Levels.Order {
		if !level.VendorRestricted || level.VendorDefinedPasswordHash == "" {
			continue
		}
		restricted = append(restricted, "password manager hash "+level.VendorDefinedPasswordHash)
	}
	if len(restricted) == 0 {
		return nil
	}

	lines := []string{"! (vendor-config)"}
	enter, ok := execEnterWords(ctx)
	if ok {
		lines = append(lines, strings.Join(enter, " "))
	}
	configEnter, configOk := configEnterWords(ctx)
	if configOk {
		lines = append(lines, strings.Join(configEnter, " "))
	}
	lines = append(lines, restricted...)
	if configOk {
		lines = append(lines, "end")
	}
	lines = append(lines, "!")
	return lines
}

// configEnterWords - This function returns the words that move a session
// from exec into config mode, discovered from the exec tree rather than
// repeated here as a second literal that could drift.
//
// It returns false when there is no fully loaded "config" entry with a
// resolvable parent, which happens in a few unit tests that only care
// about the values inside the block. The caller then leaves the wrapper
// lines out rather than guessing.
func configEnterWords(ctx *command.AppContext) ([]string, bool) {
	return levelEnterWords(ctx, "config")
}

// execEnterWords - This function is configEnterWords for entering exec
// from base.
//
// runningConfigLines prepends it whenever any exec rooted content follows.
// That one line is what makes the whole script replayable from base,
// including the boot time replay, rather than assuming whoever pastes it
// has already elevated.
func execEnterWords(ctx *command.AppContext) ([]string, bool) {
	return levelEnterWords(ctx, "exec")
}

// interfaceEnterWords - This function does the same thing as
// configEnterWords, for entering config interface mode for one specific
// interface, appending name as the argument cmd_interface.go's "interface"
// command expects, since that value comes from this state, not from
// anything command.CommandLevel itself knows about.
func interfaceEnterWords(ctx *command.AppContext, name string) ([]string, bool) {
	words, ok := levelEnterWords(ctx, "config-if")
	if !ok {
		return nil, false
	}
	return append(words, name), true
}

// lineEnterWords - This function does the same thing as configEnterWords,
// for entering "line" mode. Unlike interfaceEnterWords, "line" itself
// takes no argument, there is only ever one, deployment wide set of line
// defaults today, not a named collection the way interfaces are, so this
// needs nothing appended beyond what levelEnterWords already returns.
func lineEnterWords(ctx *command.AppContext) ([]string, bool) {
	return levelEnterWords(ctx, "config-line")
}

// levelEnterWords - This function looks up levelName in ctx.Levels, then
// its own Parent, and returns the literal words, found in the parent's
// tree through command.LiteralCommandPath, that a session sitting in that
// parent level types to reach levelName. See configEnterWords,
// interfaceEnterWords, lineEnterWords, and wrappedLevelAliasLines above
// for this project's callers.
func levelEnterWords(ctx *command.AppContext, levelName string) ([]string, bool) {
	if ctx.Levels == nil {
		return nil, false
	}
	level, ok := ctx.Levels.ByName[levelName]
	if !ok {
		return nil, false
	}
	parent, ok := ctx.Levels.ByName[level.Parent]
	if !ok {
		return nil, false
	}
	return command.LiteralCommandPath(parent.Tree, level.EnterCommand)
}

// levelExitWords - This function looks up levelName in ctx.Levels and
// returns the literal words that leave it again.
//
// When levelName declares its own ExitCommand, the words are found inside
// levelName's tree rather than its parent's, because that is where
// EnterCommandLevel and ExitCommandLevel expect it to be registered.
//
// A level with no ExitCommand, config and the modes nested under it, is
// left through the shared "end" command instead. That is always correct
// for one of those, since each is reached by pushing a frame on top of
// exec rather than by swapping the root frame.
//
// The one caller is wrappedLevelAliasLines, and only ever for a level
// reached through EnterCommandLevel in the first place.
func levelExitWords(ctx *command.AppContext, levelName string) ([]string, bool) {
	if ctx.Levels == nil {
		return nil, false
	}
	level, ok := ctx.Levels.ByName[levelName]
	if !ok {
		return nil, false
	}
	if level.ExitCommand == "" {
		return []string{"end"}, true
	}
	return command.LiteralCommandPath(level.Tree, level.ExitCommand)
}

func sortedInterfaceNames(state *State) []string {
	names := make([]string, 0, len(state.Interfaces))
	for name := range state.Interfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
