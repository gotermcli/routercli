// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

// LiteralCommandPath - Rendering configuration back out as pasteable text
// needs the words a person types to reach a Command Level, "configure"
// and "terminal" for example. A level only records the registered handler
// name, which says nothing about how many words produced it.
//
// Keeping a second declaration of those words would let the two drift, so
// this reads them from the tree that already defines the command.
//
// It walks tree and every nested Subcommands map for the one Command whose
// Run equals runKey, and returns the literal keys that reach it, in
// order. It returns nil, false when nothing matches.
//
// The first match wins. A tree registering the same handler at two paths
// is a configuration mistake this does not try to resolve.
func LiteralCommandPath(tree map[string]*Command, runKey string) ([]string, bool) {
	for key, cmd := range tree {
		if cmd.Run == runKey {
			return []string{key}, true
		}
		if len(cmd.Subcommands) > 0 {
			if rest, ok := LiteralCommandPath(cmd.Subcommands, runKey); ok {
				return append([]string{key}, rest...), true
			}
		}
	}
	return nil, false
}
