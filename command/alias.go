// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

// ExpandAlias - This function checks tokens[0] against the aliases defined
// for the session's current Command Level and, on a match, returns a new
// slice with tokens[0] replaced by the expansion, trailing arguments left
// in place after it.
//
// tokens comes back unchanged when there is nothing to expand.
//
// Expansion is a single pass, never recursive. An alias whose expansion
// starts with another alias name is resolved one level deep, the same
// restraint Cisco applies, so a session can never define a cycle that
// hangs dispatch.
//
// This runs in the read loop before Resolve, not inside it. An alias is a
// textual substitution on what was typed, not a new kind of node Resolve
// needs to walk.
func ExpandAlias(ctx *AppContext, tokens []string) []string {
	if len(tokens) == 0 || ctx.Levels == nil || ctx.Position == nil {
		return tokens
	}
	level, ok := ctx.Levels.ByName[ctx.Position.Current().Name]
	if !ok || len(level.Aliases) == 0 {
		return tokens
	}
	expansion, ok := level.Aliases[tokens[0]]
	if !ok {
		return tokens
	}
	expanded := make([]string, 0, len(expansion)+len(tokens)-1)
	expanded = append(expanded, expansion...)
	expanded = append(expanded, tokens[1:]...)
	return expanded
}
