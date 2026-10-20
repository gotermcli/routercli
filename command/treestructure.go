// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gotermcli/routercli/auth"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------
// Public Methods - Tree Structure
// ----------------------------------------------------------------------

// Base - This method returns the one level with IsBase set. Every Session
// starts here, see Session.CommandLevel in package auth, and it seeds the
// root CommandLevelStack frame in main.go. This panics if called before a
// successful LoadTreeStructure, or on a zero value TreeStructure, since
// that is a programming error, LoadTreeStructure guarantees exactly one
// base level exists on any successful return, not a runtime condition.
func (t *TreeStructure) Base() *CommandLevel {
	for _, e := range t.Order {
		if e.IsBase {
			return e
		}
	}
	panic("command: TreeStructure has no base level, this should have been caught by LoadTreeStructure validation")
}

// ----------------------------------------------------------------------
// Public Functions - Tree Structure
// ----------------------------------------------------------------------

// parseTreeManifest - This function reads and decodes the Command Level
// manifest at manifestPath into one *CommandLevel per named tree, with
// each level's Name filled in from its key. An unknown property name is an
// error, the same way config.LoadSystemConfig and auth.LoadUsers treat an
// unknown key in their own files, as is a manifest holding more than one
// YAML document or defining no trees at all.
//
// This does no cross level checking of its own; see verifyLevelHierarchy
// for that.
func parseTreeManifest(manifestPath string) (map[string]*CommandLevel, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading tree structure manifest %s: %w", manifestPath, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var parsed treeStructureFile
	if err := dec.Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing tree structure manifest %s: %w", manifestPath, err)
	}

	var extra treeStructureFile
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("tree structure manifest %s contains multiple YAML documents", manifestPath)
		}
		return nil, fmt.Errorf("parsing tree structure manifest %s: %w", manifestPath, err)
	}

	if len(parsed.Trees) == 0 {
		return nil, fmt.Errorf("tree structure manifest %s defines no trees at all", manifestPath)
	}

	levels := make(map[string]*CommandLevel, len(parsed.Trees))
	// Each tree_file is written relative to the manifest that names it, so
	// one set of tree files works whatever directory the binary was started
	// from. An absolute path is left alone.
	manifestDir := filepath.Dir(manifestPath)

	for name, cl := range parsed.Trees {
		if cl.TreeFile != "" && !filepath.IsAbs(cl.TreeFile) {
			cl.TreeFile = filepath.Join(manifestDir, cl.TreeFile)
		}
		level := cl
		level.Name = name
		levels[name] = &level
	}
	return levels, nil
}

// verifyLevelHierarchy - This function checks everything about the set of
// levels as a whole, as opposed to any one level's contents. An empty or
// malformed hierarchy should fail loudly at startup rather than produce a
// CLI that silently has no way to reach half its commands, so each of the
// following is a hard error: zero levels or more than one level setting
// is_base, since a fresh session needs exactly one unambiguous starting
// point; a level whose parent names another level that does not exist in
// this manifest; and a cycle in the parent chain.
func verifyLevelHierarchy(levels map[string]*CommandLevel, manifestPath string) error {
	baseCount := 0
	for _, l := range levels {
		if l.IsBase {
			baseCount++
		}
	}
	if baseCount == 0 {
		return fmt.Errorf("tree structure manifest %s has no base level (no tree sets is_base: true)", manifestPath)
	}
	if baseCount > 1 {
		return fmt.Errorf("tree structure manifest %s has more than one base level (more than one tree sets is_base: true)", manifestPath)
	}

	for name, l := range levels {
		if l.Parent == "" {
			continue
		}
		if _, ok := levels[l.Parent]; !ok {
			return fmt.Errorf("command level %q has parent %q, which is not defined in this manifest", name, l.Parent)
		}
	}

	// Cycle detection: walk each level's Parent chain up to len(levels)
	// steps. If it has not terminated by then, something loops.
	for name, l := range levels {
		cur := l
		steps := 0
		for cur.Parent != "" {
			cur = levels[cur.Parent]
			steps++
			if steps > len(levels) {
				return fmt.Errorf("command level %q's parent chain does not terminate, check for a cycle", name)
			}
		}
	}

	return nil
}

// LoadTreeStructure - A deployment declares its Command Levels in a
// manifest and its commands in per level tree files. Something has to turn
// those into the assembled structure the rest of the program uses.
//
// This reads the manifest, resolves each level's effective tree, its own
// tree merged with its parent chain when InheritParent is set, then merged
// once with the common tree unless SkipCommonMerge, and returns the
// result. Privilege levels and nested modes are handled identically.
//
// A broken manifest MUST fail at startup rather than produce a CLI that
// silently cannot reach half its commands. These are hard errors: zero or
// more than one level setting is_base, a parent naming a level that does
// not exist, a cycle in the parent chain, an unknown property, and more
// than one YAML document.
//
// This validates only what loading requires. Whether each declared
// EnterCommand and ExitCommand is registered is VerifyCommandLevels' job.
func LoadTreeStructure(manifestPath, commonPath string) (*TreeStructure, error) {
	levels, err := parseTreeManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if err := verifyLevelHierarchy(levels, manifestPath); err != nil {
		return nil, err
	}

	commonTree, err := LoadTree(commonPath)
	if err != nil {
		return nil, fmt.Errorf("loading common tree %s: %w", commonPath, err)
	}
	markCommonCommands(commonTree)

	// levelSwitchNames collects every EnterCommand and ExitCommand
	// declared anywhere in this manifest, "admin" and "disable" for
	// instance, this project's shipped tree. See
	// filterLevelSwitchCommands's doc comment for why these are stripped
	// back out again whenever InheritParent carries a tree forward into a
	// further descendant, rather than left to accumulate down the whole
	// chain the way an ordinary command such as "show" correctly does.
	levelSwitchNames := make(map[string]bool, len(levels)*2)
	for _, l := range levels {
		if l.EnterCommand != "" {
			levelSwitchNames[l.EnterCommand] = true
		}
		if l.ExitCommand != "" {
			levelSwitchNames[l.ExitCommand] = true
		}
	}

	// Build the order, base first, then anything whose parent is already
	// resolved, in whatever order that ends up being, since only the base
	// level itself has no parent to wait on, along with each level's raw,
	// pre-common-merge, tree. InheritParent needs the parent's already
	// accumulated raw tree, not just the parent's TreeFile, so this has to
	// proceed top-down rather than in arbitrary map iteration order.
	rawTrees := make(map[string]map[string]*Command, len(levels))
	var order []*CommandLevel
	remaining := make(map[string]*CommandLevel, len(levels))
	for name, l := range levels {
		remaining[name] = l
	}
	for len(remaining) > 0 {
		progressed := false
		for name, l := range remaining {
			if l.Parent != "" {
				if _, ready := rawTrees[l.Parent]; !ready {
					continue // parent not resolved yet
				}
			}
			ownRaw, err := LoadTree(l.TreeFile)
			if err != nil {
				return nil, fmt.Errorf("loading Command Level %q's file %s: %w", name, l.TreeFile, err)
			}
			effectiveRaw := ownRaw
			if l.InheritParent && l.Parent != "" {
				inherited := filterLevelSwitchCommands(rawTrees[l.Parent], levelSwitchNames)
				merged, err := MergeTrees(inherited, ownRaw)
				if err != nil {
					return nil, fmt.Errorf("merging Command Level %q with parent %q: %w", name, l.Parent, err)
				}
				effectiveRaw = merged
			}
			rawTrees[name] = effectiveRaw
			finalTree := effectiveRaw
			if !l.SkipCommonMerge {
				merged, err := MergeTrees(effectiveRaw, commonTree)
				if err != nil {
					return nil, fmt.Errorf("merging Command Level %q with the common tree: %w", name, err)
				}
				finalTree = merged
			}
			l.Tree = finalTree
			order = append(order, l)
			delete(remaining, name)
			progressed = true
		}
		if !progressed {
			// The cycle detection pass above already covers this case, so
			// this should be unreachable. It is kept as a hard stop rather
			// than an infinite loop in case that pass is ever weakened
			// without this loop being revisited too.
			return nil, fmt.Errorf("tree structure manifest %s: could not resolve build order (unexpected cycle)", manifestPath)
		}
	}

	return &TreeStructure{ByName: levels, Order: order}, nil
}

// ----------------------------------------------------------------------
// Private Functions - Tree Structure
// ----------------------------------------------------------------------

// markCommonCommands - This function stamps IsCommonCommand true on every
// command in tree, recursively through Subcommands. It is called exactly
// once per program run, LoadTreeStructure's call right after
// LoadTree(commonPath) returns and before commonTree is merged anywhere.
// That single call is enough for the whole program: the very same *Command
// values loaded there are the ones MergeTrees then copies its pointers
// from into every Command Level's Tree, so marking them once here is
// marking them everywhere they end up. See Command.IsCommonCommand's doc
// comment for what a caller does with this, ListOptions.MergeCommon's
// ordering.
func markCommonCommands(tree map[string]*Command) {
	for _, cmd := range tree {
		cmd.IsCommonCommand = true
		if len(cmd.Subcommands) > 0 {
			markCommonCommands(cmd.Subcommands)
		}
	}
}

// filterLevelSwitchCommands - InheritParent carries a parent level's
// commands forward so they stay available further down, which is right for
// an ordinary command such as "show".
//
// It is wrong for a command that switches Command Levels. Those move a
// session between two exact levels, so inheriting one further just offers
// a command that is refused every time it is typed, with a "you must be in
// %s mode first" the session had no way to predict.
//
// This returns a copy of tree with those commands left out, checked
// recursively through Subcommands. "admin" and "configure terminal" are
// examples in the shipped tree.
//
// Neither tree nor any Command in it is mutated. A Command needing a
// smaller Subcommands map is copied first, since the same pointer is
// probably still reachable from the level that owns it.
//
// A container left with neither Run nor Subcommands is dropped, rather
// than surviving as a stub that resolves but runs nothing.
//
// This runs only at an InheritParent merge, against the parent's resolved
// tree. A level always keeps its own declared commands.
func filterLevelSwitchCommands(tree map[string]*Command, switchNames map[string]bool) map[string]*Command {
	if len(switchNames) == 0 {
		return tree
	}
	out := make(map[string]*Command, len(tree))
	for name, cmd := range tree {
		if cmd.Run != "" && switchNames[cmd.Run] {
			continue
		}
		effective := cmd
		if len(cmd.Subcommands) > 0 {
			filteredSub := filterLevelSwitchCommands(cmd.Subcommands, switchNames)
			if len(filteredSub) != len(cmd.Subcommands) {
				if cmd.Run == "" && len(filteredSub) == 0 {
					continue
				}
				copyCmd := *cmd
				copyCmd.Subcommands = filteredSub
				effective = &copyCmd
			}
		}
		out[name] = effective
	}
	return out
}

// RequireCurrentCommandLevel - "You must be here to go there" needs one
// enforcement point, or two kinds of Command Level would check it two
// different ways.
//
// This returns an error unless the frame on top of the stack is exactly
// parent. A root swap level reaches it through EnterCommandLevel; a nested
// mode such as config calls it directly. target is used only in the error
// message.
//
// It checks the top of the stack rather than Session.CommandLevel because
// that field only ever names a root swap level. It could not express "you
// must be inside config to enter config-if", since config never sets it.
func RequireCurrentCommandLevel(ctx *AppContext, target, parent string) error {
	if ctx.Position.Current().Name != parent {
		return fmt.Errorf("%s", ctx.Translator.T("commandlevel.wrong_level", target, parent))
	}
	return nil
}

// withinReauthGracePeriod - This function reports whether level's
// LastAuthenticatedAt is recent enough, against ReauthGracePeriod, for
// EnterCommandLevel to skip prompting again.
//
// It is false when ReauthGracePeriod is zero, the default, or when
// LastAuthenticatedAt is still unset, meaning no prompt has succeeded for
// this level in this run.
//
// Only a real, live password check inside EnterCommandLevel ever sets
// LastAuthenticatedAt. Nothing that merely sets what PasswordHash equals
// does, since treating the two as the same thing would be a real pass the
// hash vulnerability.
func withinReauthGracePeriod(ctx *AppContext, level *CommandLevel) bool {
	if ctx.ReauthGracePeriod <= 0 {
		return false
	}
	if level.LastAuthenticatedAt.IsZero() {
		return false
	}
	return time.Since(level.LastAuthenticatedAt) < ctx.ReauthGracePeriod
}

// withinAuthBypassTrust - This function reports whether a level marked
// GrantAuthBypass was authenticated recently enough, against
// AuthBypassTrustWindow, for EnterCommandLevel to let a session into any
// level without prompting.
//
// It is false when the window is zero, when no level carries
// GrantAuthBypass, or when none that does has been entered with a real
// password in this run.
//
// It is exactly as narrow as withinReauthGracePeriod in what it accepts as
// proof. Only a real password check inside EnterCommandLevel sets
// LastAuthenticatedAt on any level.
//
// What it adds is scope, not weaker proof. One real check, at a level
// built to require one, waives every other level's prompt for a bounded
// window, so a saved configuration can be replayed without a prompt at
// each gated level.
func withinAuthBypassTrust(ctx *AppContext) bool {
	if ctx.AuthBypassTrustWindow <= 0 {
		return false
	}
	if ctx.Levels == nil {
		return false
	}
	for _, l := range ctx.Levels.Order {
		if !l.GrantAuthBypass {
			continue
		}
		if l.LastAuthenticatedAt.IsZero() {
			continue
		}
		if time.Since(l.LastAuthenticatedAt) < ctx.AuthBypassTrustWindow {
			return true
		}
	}
	return false
}

// EnterCommandLevel - Every privilege level entry command performs the
// same checks. Repeating them in each cmd_*.go file would let them drift.
//
// This does that shared work: the current level check, the password check
// or an honored grace period, updating the session fields, and swapping
// the root stack frame.
//
// It prints, logs, and audits nothing. What to tell the user is the
// calling file's decision, since different levels want different feedback.
//
// The return values distinguish four outcomes:
//
//   - true, nil: the session moved into level.
//   - false, nil: the session was already there. A no-op, not a failure.
//   - false, error, rate limited: a lockout is in effect. Checked before
//     prompting, so a locked out session is not invited to try again.
//   - false, error, refused: wrong level, or a wrong or missing password.
//
// This is only for levels reached by swapping the root frame. A nested
// mode such as config can have more than one active at once, which a root
// swap cannot express, so those call RequireCurrentCommandLevel and push
// their own frame.
func EnterCommandLevel(ctx *AppContext, level, parent *CommandLevel) (entered bool, err error) {
	if ctx.Session.CommandLevel == level.Name {
		return false, nil
	}
	if err := RequireCurrentCommandLevel(ctx, level.Name, level.Parent); err != nil {
		return false, err
	}
	// The role gate is checked before the password gate below, and before
	// ever prompting, the same "do not invite a session to try at all"
	// reasoning the rate limit check inside the default case below already
	// follows. ReplayingStartupConfig waives this the same way it waives
	// the password gate right below it: nobody has typed anything at all
	// yet during boot time replay, no Session even exists, so there is no
	// logged in user's roles to check in the first place. See
	// AppContext.ReplayingStartupConfig's doc comment. AllowedRoles and
	// PasswordHash are independent gates; both are enforced when both are
	// set on the same level.
	if !ctx.ReplayingStartupConfig && len(level.AllowedRoles) > 0 && !Authorized(ctx, level.AllowedRoles) {
		return false, fmt.Errorf("%s", ctx.Translator.T("commandlevel.access_denied"))
	}
	switch effectiveHash := level.EffectivePasswordHash(); {
	case ctx.ReplayingStartupConfig:
		// Trusted boot time replay. Checked first, before effectiveHash is
		// even looked at, since this waves a session through regardless of
		// whether level has a password configured at all. Deliberately
		// does NOT set level.LastAuthenticatedAt: nobody typed a
		// credential here, real or otherwise, the trust comes from this
		// code running as this operating system process at all, a
		// different kind of proof than anything withinReauthGracePeriod or
		// withinAuthBypassTrust ever accept, and letting it masquerade as
		// one of those would let a session that later reenters this same
		// level, for real, skip a prompt it never earned.
	case effectiveHash == "":
		// No password at all, nothing to check.
	case withinReauthGracePeriod(ctx, level):
		// Within the grace period from a real, earlier prompt this same
		// run of the program already answered; see withinReauthGracePeriod
		// below. Sliding the window forward again here, the same way
		// sudo's cached authentication does, means a session that keeps
		// stepping back into this level regularly never has to re-answer
		// as long as it never goes stale, rather than the window only ever
		// counting down from the first prompt.
		level.LastAuthenticatedAt = time.Now()
	case withinAuthBypassTrust(ctx):
		// Granted through admin's recent, real, live credential check, see
		// withinAuthBypassTrust below, not through anything at this level
		// itself. Deliberately does NOT set level.LastAuthenticatedAt:
		// nobody proved this level's credential, only admin's, and letting
		// this branch mark this level as if it had been would falsely let
		// a later, ordinary reentry skip the prompt through
		// withinReauthGracePeriod once AuthBypassTrustWindow itself has long
		// since expired.
	default:
		// Checked before ever prompting. See this function's doc comment
		// on the four return value cases for why a locked out session is
		// not invited to try again.
		if ok, retryAfter := level.RateLimiter.Allow(); !ok {
			return false, fmt.Errorf("%s", ctx.Translator.T("auth.too_many_attempts", auth.RoundForDisplay(retryAfter)))
		}
		password, perr := auth.PromptSecret(os.Stdout, int(os.Stdin.Fd()), ctx.Translator)
		if perr != nil || !auth.VerifyPassword(effectiveHash, password) {
			level.RateLimiter.RecordFailure()
			return false, fmt.Errorf("%s", ctx.Translator.T("commandlevel.access_denied"))
		}
		level.RateLimiter.RecordSuccess()
		level.LastAuthenticatedAt = time.Now()
	}
	ctx.Session.CommandLevel = level.Name
	ctx.Session.CommandLevelEnteredAt = time.Now()
	ctx.Position.SetRootTree(level.Name, level.PromptSuffix, level.Tree)
	ctx.Logger.Debugln("DEBUG: session entered Command Level", level.Name, "for user", ctx.Session.Username)
	return true, nil
}

// ExitCommandLevel - This function is the mirror of EnterCommandLevel. It
// moves a session back from level to parent. It follows the same no
// printing, no logging beyond Debugln policy, and returns the same shape,
// exited instead of entered, for the same reason. exited is false with a
// nil error when the session was not at level, a no-op rather than a
// failure, for example "disable" run twice in a row, or from somewhere it
// does not apply.
func ExitCommandLevel(ctx *AppContext, level, parent *CommandLevel) (exited bool, err error) {
	if ctx.Session.CommandLevel != level.Name {
		return false, nil
	}
	ctx.Session.CommandLevel = parent.Name
	ctx.Position.SetRootTree(parent.Name, parent.PromptSuffix, parent.Tree)
	ctx.Logger.Debugln("DEBUG: session left Command Level", level.Name, "back to", parent.Name, "for user", ctx.Session.Username)
	return true, nil
}
