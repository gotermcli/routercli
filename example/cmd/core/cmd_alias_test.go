// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"reflect"
	"testing"

	"github.com/gotermcli/routercli/command"
)

// newAliasTestContext - This function builds on newTestContext with a
// minimal two level command.TreeStructure, "exec" and "config", exec's
// Tree carrying one real command, "show", so a collision check has
// something real to reject against. ctx.Position is seeded to
// currentLevel, matching the way a real session's Command Level stack
// tells the "alias" handler which level it is standing in. The handler
// reads that rather than a typed argument. This is built by hand rather
// than through command.LoadTreeStructure, since these tests only care
// about CommandLevel.Aliases and CommandLevel.Tree, not manifest loading
// itself, already covered in package command's tests.
func newAliasTestContext(currentLevel string) *command.AppContext {
	ctx := newTestContext()
	ctx.Levels = &command.TreeStructure{
		ByName: map[string]*command.CommandLevel{
			"exec": {
				Name: "exec",
				Tree: map[string]*command.Command{
					"show": {},
				},
			},
			"config": {
				Name: "config",
				Tree: map[string]*command.Command{
					"hostname": {},
				},
			},
		},
	}
	level := ctx.Levels.ByName[currentLevel]
	ctx.Position = command.NewCommandLevelStack(level.Name, "", level.Tree)
	// rewireDaemonClient shares this exact TreeStructure with
	// ctx.DaemonClient's Store, so cmd_alias.go's
	// ctx.DaemonClient.MutateLevels call sees, and modifies, the same
	// ctx.Levels a test asserts against afterward. See
	// rewireDaemonClient's doc comment in testhelpers_test.go.
	rewireDaemonClient(ctx)
	return ctx
}

// TestAliasHandlerDefinesAlias - This test verifies that "alias sh show
// version", typed while standing in the exec level, records "sh" against
// the exec level's Aliases map, expanding to exactly the words that
// followed the alias name.
func TestAliasHandlerDefinesAlias(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show", "version"}); err != nil {
		t.Fatalf("alias handler returned unexpected error: %v", err)
	}

	got := ctx.Levels.ByName["exec"].Aliases["sh"]
	want := []string{"show", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases[\"sh\"] = %v, want %v", got, want)
	}
}

// TestAliasHandlerRejectsUnresolvedCurrentLevel - This test verifies that
// a session standing at a Command Level not present in ctx.Levels.ByName,
// which should never happen in a real, fully loaded deployment, is refused
// with an error rather than a nil map panic. This is the defensive
// counterpart to the old, now removed, "unknown level" check that used to
// apply to a typed argument; there is nothing left for a session to type
// wrong, only this one defensive case remains.
func TestAliasHandlerRejectsUnresolvedCurrentLevel(t *testing.T) {
	ctx := newAliasTestContext("exec")
	ctx.Position = command.NewCommandLevelStack("nonexistent", "", map[string]*command.Command{})
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show"}); err == nil {
		t.Fatal("expected an error when the current Command Level does not resolve, got nil")
	}
}

// TestAliasHandlerRejectsCollisionWithRealCommand - This test verifies
// that an alias name matching a real, already reachable command at the
// current Command Level, "show" here, is refused, so an alias can never
// silently shadow a real command.
func TestAliasHandlerRejectsCollisionWithRealCommand(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"show", "show", "version"}); err == nil {
		t.Fatal("expected an error for an alias name colliding with a real command, got nil")
	}
	if _, defined := ctx.Levels.ByName["exec"].Aliases["show"]; defined {
		t.Error("a rejected alias must not be recorded")
	}
}

// TestAliasHandlerRejectsReservedWordNo - This test verifies that "no"
// itself, the reserved negation word command.Resolve always strips before
// resolving anything, can never be defined as an alias name, since it
// could never be reached, every leading "no" is always treated as negation
// first.
func TestAliasHandlerRejectsReservedWordNo(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"no", "show"}); err == nil {
		t.Fatal("expected an error defining an alias named \"no\", got nil")
	}
}

// TestAliasHandlerRejectsRedefiningAnExistingAlias - This test verifies
// that defining an alias name a second time, without removing it first, is
// refused, a deliberate security measure so a change to an already trusted
// alias can never happen silently in one step. This is unlike real Cisco's
// "alias exec" convention, which does allow a bare redefinition.
func TestAliasHandlerRejectsRedefiningAnExistingAlias(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show", "version"}); err != nil {
		t.Fatalf("first alias definition returned unexpected error: %v", err)
	}
	if err := cmd.RunFunc(ctx, []string{"sh", "show", "running-config"}); err == nil {
		t.Fatal("expected an error redefining an already defined alias without removing it first, got nil")
	}

	got := ctx.Levels.ByName["exec"].Aliases["sh"]
	want := []string{"show", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases[\"sh\"] after a refused redefinition = %v, want %v (unchanged)", got, want)
	}
}

// TestAliasHandlerAllowsRedefiningAfterRemoval - This test verifies that
// "no alias" followed by "alias" again does let a session change what an
// alias expands to, the required two step path in place of the single step
// redefinition TestAliasHandlerRejectsRedefiningAnExistingAlias confirms
// is refused.
func TestAliasHandlerAllowsRedefiningAfterRemoval(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show", "version"}); err != nil {
		t.Fatalf("first alias definition returned unexpected error: %v", err)
	}
	ctx.Negated = true
	if err := cmd.RunFunc(ctx, []string{"sh"}); err != nil {
		t.Fatalf("removing the alias returned unexpected error: %v", err)
	}
	ctx.Negated = false
	if err := cmd.RunFunc(ctx, []string{"sh", "show", "running-config"}); err != nil {
		t.Fatalf("redefining the alias after removal returned unexpected error: %v", err)
	}

	got := ctx.Levels.ByName["exec"].Aliases["sh"]
	want := []string{"show", "running-config"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases[\"sh\"] after redefinition = %v, want %v", got, want)
	}
}

// TestAliasHandlerAllowsRedefiningDuringTrustedReplay - This test verifies
// that ctx.ReplayingStartupConfig being true waives the already-defined
// check, so command.LoadStartupConfig can restate an alias a live session
// already has, "reload" running that same replay against the same
// in-memory ctx.Levels rather than a freshly loaded one, without being
// refused as a collision with itself.
func TestAliasHandlerAllowsRedefiningDuringTrustedReplay(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show", "version"}); err != nil {
		t.Fatalf("first alias definition returned unexpected error: %v", err)
	}
	ctx.ReplayingStartupConfig = true
	if err := cmd.RunFunc(ctx, []string{"sh", "show", "version"}); err != nil {
		t.Fatalf("restating the same alias during a trusted replay returned unexpected error: %v", err)
	}
}

// TestAliasHandlerDoesNotAffectAnotherLevel - This test verifies that
// defining an alias while standing in one level never touches another
// level's Aliases map, the direct handler level counterpart to
// command.TestExpandAliasIsScopedPerLevel.
func TestAliasHandlerDoesNotAffectAnotherLevel(t *testing.T) {
	ctx := newAliasTestContext("exec")
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh", "show"}); err != nil {
		t.Fatalf("alias handler returned unexpected error: %v", err)
	}
	if len(ctx.Levels.ByName["config"].Aliases) != 0 {
		t.Errorf("config level's Aliases = %v, want empty", ctx.Levels.ByName["config"].Aliases)
	}
}

// TestAliasHandlerNegatedRemovesAlias - This test verifies that "no alias
// sh", typed while standing in the exec level, deletes a previously
// defined alias from that level's Aliases map.
func TestAliasHandlerNegatedRemovesAlias(t *testing.T) {
	ctx := newAliasTestContext("exec")
	ctx.Levels.ByName["exec"].Aliases = map[string][]string{"sh": {"show"}}
	ctx.Negated = true
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh"}); err != nil {
		t.Fatalf("negated alias handler returned unexpected error: %v", err)
	}
	if _, defined := ctx.Levels.ByName["exec"].Aliases["sh"]; defined {
		t.Error("expected \"sh\" to be removed from exec's Aliases map")
	}
}

// TestAliasHandlerNegatedRejectsUndefinedAlias - This test verifies that
// removing an alias name that was never defined in the first place is
// refused with an error, rather than silently succeeding as a no-op.
func TestAliasHandlerNegatedRejectsUndefinedAlias(t *testing.T) {
	ctx := newAliasTestContext("exec")
	ctx.Negated = true
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh"}); err == nil {
		t.Fatal("expected an error removing an alias that was never defined, got nil")
	}
}

// TestAliasHandlerNegatedRejectsUnresolvedCurrentLevel - This test
// verifies that "no alias" also refuses a session standing at a Command
// Level not present in ctx.Levels.ByName, the negated path's counterpart
// to TestAliasHandlerRejectsUnresolvedCurrentLevel.
func TestAliasHandlerNegatedRejectsUnresolvedCurrentLevel(t *testing.T) {
	ctx := newAliasTestContext("exec")
	ctx.Position = command.NewCommandLevelStack("nonexistent", "", map[string]*command.Command{})
	ctx.Negated = true
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, []string{"sh"}); err == nil {
		t.Fatal("expected an error when the current Command Level does not resolve, got nil")
	}
}

// TestAliasHandlerNegatedRequiresOneArg - This test verifies that the
// negated path validates its own minimum argument count by hand, since
// command.ValidateArgs is never called for a negated command, see that
// function's doc comment, rather than panicking on a missing args[0] when
// nothing at all was typed after "no alias".
func TestAliasHandlerNegatedRequiresOneArg(t *testing.T) {
	ctx := newAliasTestContext("exec")
	ctx.Negated = true
	cmd := loadTestCommand(t, "alias")

	if err := cmd.RunFunc(ctx, nil); err == nil {
		t.Error("expected an error for \"no alias\" with no arguments, got nil")
	}
}
