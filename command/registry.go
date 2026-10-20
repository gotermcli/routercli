// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"fmt"
)

// registry - This variable holds every handler that has self- registered
// through Register(), keyed by the same string a command tree YAML file
// uses in its "run" directive. This is package-level rather than passed
// around, since an init() function, where Register is called from, has no
// other way to reach a constructor-supplied instance. This is the same
// pattern database/sql drivers and net/http/pprof handlers use.
var registry = map[string]HandlerFunc{}

// ----------------------------------------------------------------------
// Public Functions - Registry
// ----------------------------------------------------------------------

// Register - This function lets a command file make itself known to the
// tree loader from its init, so adding a command never means editing this
// file or main.go.
//
// name is what a tree file's "run" directive references to wire this
// handler into a place in the tree.
//
// Registering the same name twice panics at startup. That is two command
// files both claiming the same name by mistake, and letting the second
// registration silently win would be worse.
func Register(name string, fn HandlerFunc) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("routercli: command handler %q is already registered, check for a duplicate Register() call", name))
	}
	registry[name] = fn
}

// ----------------------------------------------------------------------
// Private Functions - Registry
// ----------------------------------------------------------------------

// lookupHandler - This function looks up a registered handler by name. It
// is used by the tree loader when resolving a "run" directive from a tree
// YAML file into an actual HandlerFunc.
func lookupHandler(name string) (HandlerFunc, bool) {
	fn, ok := registry[name]
	return fn, ok
}
