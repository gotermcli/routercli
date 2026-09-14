// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import "testing"

// TestCommandLevelStackStartsAtRoot - This test verifies that a freshly
// constructed CommandLevelStack begins at depth 1, at the root, with
// Current reflecting the name it was seeded with.
func TestCommandLevelStackStartsAtRoot(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{})
	if !s.AtRoot() {
		t.Error("a fresh CommandLevelStack should start at the root")
	}
	if s.Depth() != 1 {
		t.Errorf("Depth() = %d, want 1", s.Depth())
	}
	if s.Current().Name != "exec" {
		t.Errorf("Current().Name = %q, want %q", s.Current().Name, "exec")
	}
}

// TestCommandLevelStackPushAndPop - This test verifies that Push adds a
// frame and moves off the root, and that Pop removes it again and returns
// to the root.
func TestCommandLevelStackPushAndPop(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{})
	s.Push(CommandLevelFrame{Name: "config", PromptSuffix: "(config)"})
	if s.AtRoot() {
		t.Error("should not be at root after Push")
	}
	if s.Depth() != 2 {
		t.Errorf("Depth() = %d, want 2", s.Depth())
	}
	if s.Current().Name != "config" {
		t.Errorf("Current().Name = %q, want %q", s.Current().Name, "config")
	}

	ok := s.Pop()
	if !ok {
		t.Error("Pop() should succeed when not at root")
	}
	if !s.AtRoot() {
		t.Error("should be back at root after popping the only pushed frame")
	}
}

// TestCommandLevelStackCannotPopRoot - This test verifies that Pop at the
// root returns false and leaves the stack unchanged, rather than popping
// past the bottom.
func TestCommandLevelStackCannotPopRoot(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{})
	ok := s.Pop()
	if ok {
		t.Error("Pop() at the root should return false and do nothing")
	}
	if !s.AtRoot() || s.Depth() != 1 {
		t.Error("popping the root should leave the stack unchanged")
	}
}

// TestCommandLevelStackPopToRootFromDeepNesting - This test verifies that
// PopToRoot returns straight to depth 1 from several nested frames in one
// call, rather than needing one Pop per frame.
func TestCommandLevelStackPopToRootFromDeepNesting(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{})
	s.Push(CommandLevelFrame{Name: "config", PromptSuffix: "(config)"})
	s.Push(CommandLevelFrame{Name: "config-if", PromptSuffix: "(config-if)", Context: "eth0"})
	if s.Depth() != 3 {
		t.Fatalf("Depth() = %d, want 3 before PopToRoot", s.Depth())
	}

	s.PopToRoot()
	if !s.AtRoot() || s.Depth() != 1 {
		t.Errorf("PopToRoot should return to depth 1, got depth %d", s.Depth())
	}
	if s.Current().Name != "exec" {
		t.Errorf("Current().Name after PopToRoot = %q, want %q", s.Current().Name, "exec")
	}
}

// TestSetRootTreeSwapsNameSuffixAndTree - This test verifies that
// SetRootTree replaces Name, PromptSuffix, and Tree together and leaves
// Depth untouched, since it swaps data on the existing root frame rather
// than pushing a new one.
//
// Name matters as much as the tree. Anything asking which level a session
// is in reads it, so a stale Name would silently diverge from
// Session.CommandLevel and make that check unreliable.
func TestSetRootTreeSwapsNameSuffixAndTree(t *testing.T) {
	operatorTree := map[string]*Command{"show": {}}
	privilegedTree := map[string]*Command{"show": {}, "configure": {}}

	s := NewCommandLevelStack("base", "", operatorTree)
	if len(s.Current().Tree) != 1 {
		t.Fatalf("expected the base tree (1 command) before swapping, got %d", len(s.Current().Tree))
	}

	s.SetRootTree("exec", "(exec-suffix)", privilegedTree)

	if len(s.Current().Tree) != 2 {
		t.Errorf("expected the privileged tree (2 commands) after swapping, got %d", len(s.Current().Tree))
	}
	if s.Current().Name != "exec" {
		t.Errorf("SetRootTree should update Name, got %q, want %q", s.Current().Name, "exec")
	}
	if s.Current().PromptSuffix != "(exec-suffix)" {
		t.Errorf("SetRootTree should update PromptSuffix, got %q, want %q", s.Current().PromptSuffix, "(exec-suffix)")
	}
	if s.Depth() != 1 {
		t.Errorf("SetRootTree should not change Depth, got %d", s.Depth())
	}
}

// TestSetRootTreeDoesNotAffectAPushedFrame - This test verifies that
// SetRootTree only ever touches frames[0]. When a Command Level such as
// config has already been pushed, swapping the root frame underneath it
// must not disturb the currently active pushed frame's Name, PromptSuffix,
// or Tree.
func TestSetRootTreeDoesNotAffectAPushedFrame(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{"show": {}})
	configTree := map[string]*Command{"hostname": {}}
	s.Push(CommandLevelFrame{Name: "config", PromptSuffix: "(config)", Tree: configTree})

	s.SetRootTree("diagnostic", "(diag-suffix)", map[string]*Command{"show": {}, "configure": {}})

	if s.Current().Name != "config" {
		t.Fatalf("expected the pushed config frame to still be current, got %q", s.Current().Name)
	}
	if len(s.Current().Tree) != 1 {
		t.Errorf("expected the pushed frame's tree (1 command) to be untouched, got %d", len(s.Current().Tree))
	}
}

// TestCommandLevelFrameContextRoundTrips - This test verifies that a value
// stashed in CommandLevelFrame.Context survives exactly as put in, through
// the any round trip. This is the mechanism config-if handlers use to know
// which interface they are editing.
func TestCommandLevelFrameContextRoundTrips(t *testing.T) {
	s := NewCommandLevelStack("exec", "", map[string]*Command{})
	s.Push(CommandLevelFrame{Name: "config-if", Context: "eth0"})

	got, ok := s.Current().Context.(string)
	if !ok || got != "eth0" {
		t.Errorf("Context round-trip = %v (ok=%v), want \"eth0\"", s.Current().Context, ok)
	}
}

// TestMergeTreesCombinesBothMaps - This test verifies that MergeTrees
// returns a single map containing every entry from both the base and
// overlay maps.
func TestMergeTreesCombinesBothMaps(t *testing.T) {
	base := map[string]*Command{"hostname": {Desc: "set hostname"}}
	overlay := map[string]*Command{"help": {Desc: "help"}, "exit": {Desc: "exit"}}

	merged, err := MergeTrees(base, overlay)
	if err != nil {
		t.Fatalf("MergeTrees returned unexpected error: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("merged has %d entries, want 3", len(merged))
	}
	for _, name := range []string{"hostname", "help", "exit"} {
		if merged[name] == nil {
			t.Errorf("merged tree missing %q", name)
		}
	}
}

// TestMergeTreesRejectsCollision - This test verifies that a command name
// defined in both the base and overlay maps is reported as an error,
// rather than one silently overwriting the other.
func TestMergeTreesRejectsCollision(t *testing.T) {
	base := map[string]*Command{"help": {Desc: "mode-specific help, should not exist"}}
	overlay := map[string]*Command{"help": {Desc: "common help"}}

	_, err := MergeTrees(base, overlay)
	if err == nil {
		t.Fatal("expected an error when base and overlay both define \"help\", got nil")
	}
}

// TestMergeTreesDoesNotMutateInputs - This test verifies that modifying
// the map MergeTrees returns does not leak back into the base or overlay
// maps it was built from, confirming MergeTrees returns a new map rather
// than an alias of one of its inputs.
func TestMergeTreesDoesNotMutateInputs(t *testing.T) {
	base := map[string]*Command{"hostname": {Desc: "x"}}
	overlay := map[string]*Command{"help": {Desc: "y"}}

	merged, err := MergeTrees(base, overlay)
	if err != nil {
		t.Fatalf("MergeTrees returned unexpected error: %v", err)
	}
	merged["new-entry"] = &Command{Desc: "should not appear in base or overlay"}

	if _, exists := base["new-entry"]; exists {
		t.Error("mutating the merged result leaked in to base")
	}
	if _, exists := overlay["new-entry"]; exists {
		t.Error("mutating the merged result leaked in to overlay")
	}
}
