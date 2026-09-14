// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"errors"
	"fmt"
)

// ErrQuit - This variable lets any handler, in any Command Level, request
// that the whole program stop rather than just popping one level, without
// runLoop needing to know that a command named "exit" exists at all. The
// shared "exit" command handler returns this when called at the root, see
// CommandLevelStack.AtRoot. runLoop checks for this specific error after
// every command runs. Its job is asking whether RunFunc() requested a
// quit, which stays correct regardless of what the command is named.
var ErrQuit = errors.New("quit")

// ----------------------------------------------------------------------
// Public Methods - CommandLevelStack
// ----------------------------------------------------------------------

// Push - This method enters a new, deeper Command Level, for example
// "configure terminal" pushing the config frame, or "interface eth0"
// pushing a config interface frame on top of that.
func (m *CommandLevelStack) Push(frame CommandLevelFrame) {
	m.frames = append(m.frames, frame)
}

// Pop - This method leaves the current Command Level, returning to the one
// below it. It returns false and does nothing if already at the root
// frame. The root cannot be popped, because popping the root is not going
// up a level, it is quitting the program, see ErrQuit, which is a
// different operation with different consequences and should not happen by
// accident through a generic Pop() call.
func (m *CommandLevelStack) Pop() bool {
	if len(m.frames) <= 1 {
		return false
	}
	m.frames = m.frames[:len(m.frames)-1]
	return true
}

// PopToRoot - This method jumps straight back to the root frame from any
// depth. This is what the "end" command does, matching Cisco's "end".
func (m *CommandLevelStack) PopToRoot() {
	m.frames = m.frames[:1]
}

// Current - This method returns the top-of-stack frame, the Command Level
// currently in effect.
func (m *CommandLevelStack) Current() CommandLevelFrame {
	return m.frames[len(m.frames)-1]
}

// AtRoot - This method reports whether the stack is currently at the root
// frame, depth 1. The shared "exit" handler uses this to decide whether to
// pop one level or request that the whole program quit, see ErrQuit.
func (m *CommandLevelStack) AtRoot() bool {
	return len(m.frames) <= 1
}

// Depth - This method returns how many frames are on the stack. It is 1 at
// the root.
func (m *CommandLevelStack) Depth() int {
	return len(m.frames)
}

// SetRootTree - Moving between privilege levels such as base and exec is
// not the same as entering a nested mode. Only one privilege level is
// active at a time, so pushing a frame for it would be wrong.
//
// This replaces the root frame's Name, PromptSuffix, and Tree together,
// in place, leaving the number of frames unchanged.
//
// Because it is not a push, "exit" at the root keeps meaning quit the
// program at every privilege level, the same as a real Cisco or HP
// device. Stepping down a level is a dedicated command such as "disable".
func (m *CommandLevelStack) SetRootTree(name, promptSuffix string, tree map[string]*Command) {
	m.frames[0].Name = name
	m.frames[0].PromptSuffix = promptSuffix
	m.frames[0].Tree = tree
}

// ----------------------------------------------------------------------
// Public Functions - CommandLevelStack
// ----------------------------------------------------------------------

// MergeTrees - This function combines base with overlay into a new map.
// This is used to merge a Command Level's tree, such as
// example/var/tree/level_config.yaml, with the commands common to every level,
// example/var/tree/level_common.yaml, things like help, exit, and end. A name
// present in both is a hard error rather than silently letting one side
// win. A tree file accidentally redefining "help" or "exit" is almost
// certainly a mistake, and silently picking a winner is exactly the kind
// of bug that stays invisible until someone notices that "help" behaves
// differently at one level than another with no idea why. Neither input
// map is mutated. The result is always a new map.
func MergeTrees(base, overlay map[string]*Command) (map[string]*Command, error) {
	merged := make(map[string]*Command, len(base)+len(overlay))
	for name, cmd := range base {
		merged[name] = cmd
	}
	for name, cmd := range overlay {
		if _, exists := merged[name]; exists {
			return nil, fmt.Errorf("command %q is defined in both trees being merged - remove the duplicate", name)
		}
		merged[name] = cmd
	}
	return merged, nil
}
