// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import "fmt"

// ----------------------------------------------------------------------
// Public Functions - Tree Pruning
// ----------------------------------------------------------------------

// PruneDisabledCommands - A deployment turns features off through
// configuration. A command belonging to a disabled feature should not
// exist at all, rather than stay reachable and then fail, or worse do
// nothing, when someone runs it.
//
// This removes every command whose Requires names a flag that is false in
// enabled, along with its subcommands, recursively. path is used only to
// build the error message.
//
// A Requires naming a flag that is not in enabled at all is a hard error.
// Otherwise "requires: totp_typo" would leave that command reachable
// forever while its author believed it was gated.
//
// This mutates tree in place, which is safe for a level's top level map.
// It MUST NOT be called twice against the same nested Subcommands map,
// since MergeTrees reuses the same *Command pointers beneath a rebuilt top
// level. Prune once, against the level that owns the tree file, before it
// is merged into another.
func PruneDisabledCommands(tree map[string]*Command, enabled map[string]bool, path string) error {
	for name, c := range tree {
		commandPath := name
		if path != "" {
			commandPath = path + " " + name
		}

		if c.Requires != "" {
			on, known := enabled[c.Requires]
			if !known {
				return fmt.Errorf("command %q sets requires %q, but that is not a recognized flag name", commandPath, c.Requires)
			}
			if !on {
				delete(tree, name)
				continue
			}
		}

		if err := PruneDisabledCommands(c.Subcommands, enabled, commandPath); err != nil {
			return err
		}
	}
	return nil
}
