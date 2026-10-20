// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

// ----------------------------------------------------------------------
// Public Methods - Session
// ----------------------------------------------------------------------

// AtLevel - This method reports whether the session's current Command
// Level is exactly name.
//
// It compares against a name rather than exposing an "Elevated" bool
// because a tree can have more than one level reachable from base. Once
// there is more than one, "is this session elevated" has no single answer,
// while "is it at this level" always does.
func (s *Session) AtLevel(name string) bool {
	return s.CommandLevel == name
}
