// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"fmt"
	"sort"
)

// ----------------------------------------------------------------------
// Public Functions - Verify
// ----------------------------------------------------------------------

// VerifyCommandLevels - This function catches a manifest that names a
// level's enter or exit command without any cmd_*.go file having
// registered it. Without this check that mistake surfaces only when a user
// first types the command and gets "unknown command".
//
// It checks a successfully loaded TreeStructure against consistency rules
// beyond what loading itself requires, and returns every problem found
// rather than only the first, so a deployment can fix them in one pass.
//
// Two rules are checked. Every non-base Command Level MUST declare a
// non-empty EnterCommand, since a level with none has no way to be
// verified at all. And every declared EnterCommand and ExitCommand MUST
// name a command that is registered, meaning some init function really did
// call Register with that exact name.
//
// This is a separate pass from LoadTreeStructure so it can run on its own.
// The --check-config flag runs this and nothing else, then exits, without
// building an AppContext or entering the read loop. It also runs at
// ordinary startup, immediately after loading, because broken
// configuration MUST fail loudly rather than silently produce an
// unreachable Command Level.
func VerifyCommandLevels(levels *TreeStructure) []error {
	var problems []error

	for _, level := range levels.Order {
		if level.IsBase {
			continue
		}
		if level.EnterCommand == "" {
			problems = append(problems, fmt.Errorf("command level %q has no enter_command declared in tree_structure.yaml", level.Name))
			continue // nothing to look up in the registry below
		}
		if _, ok := lookupHandler(level.EnterCommand); !ok {
			problems = append(problems, fmt.Errorf("command level %q declares enter_command %q, but no cmd_*.go file has registered a command by that name", level.Name, level.EnterCommand))
		}
		if level.ExitCommand != "" {
			if _, ok := lookupHandler(level.ExitCommand); !ok {
				problems = append(problems, fmt.Errorf("command level %q declares exit_command %q, but no cmd_*.go file has registered a command by that name", level.Name, level.ExitCommand))
			}
		}
	}

	return problems
}

// VerifyRegisteredCommandsAreReachable - This function checks the
// opposite direction from VerifyCommandLevels: that every handler some
// cmd_*.go file registered is actually reachable from the loaded tree.
//
// A registered handler becomes reachable one of two ways. A tree file
// points a run directive at its name, or a Command Level declares it as an
// enter_command or an exit_command. A handler neither of those references
// can never be dispatched to. It is dead code that compiles, links, and
// runs its init function exactly like live code.
//
// Nothing else catches that. A Go analyzer sees the init function called
// through the package import and Register called inside it, so at the Go
// level every registered handler is live. The deadness exists only in the
// relationship between the Go and the YAML, which is what this check
// looks at.
//
// This runs alongside VerifyCommandLevels, both at startup and under
// --check-config, and returns every problem rather than only the first.
func VerifyRegisteredCommandsAreReachable(levels *TreeStructure) []error {
	referenced := map[string]bool{}

	for _, level := range levels.Order {
		if level.EnterCommand != "" {
			referenced[level.EnterCommand] = true
		}
		if level.ExitCommand != "" {
			referenced[level.ExitCommand] = true
		}
		collectRunNames(level.Tree, referenced)
	}

	var problems []error
	for name := range registry {
		if !referenced[name] {
			problems = append(problems, fmt.Errorf("command handler %q is registered, but no tree file runs it and no command level declares it as an enter_command or exit_command, so nothing can ever dispatch to it", name))
		}
	}

	// registry is a map, so iteration order is random. Sort so a run of
	// --check-config reports the same problems in the same order twice.
	sort.Slice(problems, func(i, j int) bool {
		return problems[i].Error() < problems[j].Error()
	})

	return problems
}

// collectRunNames - This function walks tree and records every non-empty
// Run value into referenced, descending through Subcommands.
func collectRunNames(tree map[string]*Command, referenced map[string]bool) {
	for _, c := range tree {
		if c == nil {
			continue
		}
		if c.Run != "" {
			referenced[c.Run] = true
		}
		collectRunNames(c.Subcommands, referenced)
	}
}

// VerifyVendorDefinedSecrets - A VendorDefinedPasswordHash is an
// implementer's baked in secret, meant to stay out of an ordinary user's
// reach. That promise only holds if the surrounding properties agree with
// it, and a mistake there is invisible at runtime.
//
// This checks a loaded TreeStructure against the rules example/var/tree/README.md
// documents, at startup and under --check-config, collecting every problem
// rather than stopping at the first.
//
// Rules 1 to 3 apply to a CommandLevel and a Command. Rules 4 and 5 apply
// to a CommandLevel only, since VendorRestricted has no Command form.
//
//  1. PasswordHash and VendorDefinedPasswordHash MUST NOT both be set.
//     Nothing checks both, so PasswordHash would be dead configuration.
//  2. PasswordUserSettable MUST NOT be true alongside a vendor secret.
//     Resolution already handles it, but writing it reflects a
//     misunderstanding worth catching early.
//  3. Hidden MUST be true alongside a vendor secret. A secret listed in
//     help and tab completion defeats the point.
//  4. Hidden MUST be true alongside VendorRestricted, for the same reason.
//  5. VendorRestricted MUST be true alongside a vendor secret. Otherwise
//     "show running-config" still prints a "<HIDDEN>" placeholder that
//     reveals the level exists, which rule 3 alone does not prevent.
func VerifyVendorDefinedSecrets(levels *TreeStructure) []error {
	var problems []error

	for _, level := range levels.Order {
		if level.VendorRestricted && !level.Hidden {
			problems = append(problems, fmt.Errorf("command level %q sets vendor_restricted: true but not hidden: true - a vendor restricted level must always be hidden", level.Name))
		}
		if level.VendorDefinedPasswordHash == "" {
			continue
		}
		if level.PasswordHash != "" {
			problems = append(problems, fmt.Errorf("command level %q sets both password_hash and vendor_defined_password_hash - remove one, they must never both be set on the same level", level.Name))
		}
		if level.PasswordUserSettable != nil && *level.PasswordUserSettable {
			problems = append(problems, fmt.Errorf("command level %q sets vendor_defined_password_hash and password_user_settable: true together - a vendor defined password must never be user settable", level.Name))
		}
		if !level.Hidden {
			problems = append(problems, fmt.Errorf("command level %q sets vendor_defined_password_hash but not hidden: true - a vendor defined password must always be hidden", level.Name))
		}
		if !level.VendorRestricted {
			problems = append(problems, fmt.Errorf("command level %q sets vendor_defined_password_hash but not vendor_restricted: true - a vendor defined password must always be vendor restricted", level.Name))
		}
	}

	visited := make(map[*Command]bool)
	var walk func(path string, tree map[string]*Command)
	walk = func(path string, tree map[string]*Command) {
		for name, cmd := range tree {
			if visited[cmd] {
				continue
			}
			visited[cmd] = true
			full := name
			if path != "" {
				full = path + " " + name
			}
			if cmd.VendorDefinedPasswordHash != "" {
				if cmd.PasswordHash != "" {
					problems = append(problems, fmt.Errorf("command %q sets both password_hash and vendor_defined_password_hash - remove one, they must never both be set on the same command", full))
				}
				if cmd.PasswordUserSettable != nil && *cmd.PasswordUserSettable {
					problems = append(problems, fmt.Errorf("command %q sets vendor_defined_password_hash and password_user_settable: true together - a vendor defined password must never be user settable", full))
				}
				if !cmd.Hidden {
					problems = append(problems, fmt.Errorf("command %q sets vendor_defined_password_hash but not hidden: true - a vendor defined password must always be hidden", full))
				}
			}
			walk(full, cmd.Subcommands)
		}
	}
	for _, level := range levels.Order {
		walk("", level.Tree)
	}

	return problems
}

// VerifyRoles - This function checks every AllowedRoles reference in a
// loaded TreeStructure against the declared role set.
//
// A role name that is not declared is a hard error. An unknown role would
// otherwise silently gate a command no real user could pass, discovered
// only the first time someone tried it.
//
// A nil role set, meaning no roles file exists, still validates cleanly
// against a tree that never sets AllowedRoles, and still fails loudly
// against one that does.
func VerifyRoles(levels *TreeStructure, roles *RoleSet) []error {
	var problems []error

	knownRole := func(name string) bool {
		if roles == nil {
			return false
		}
		_, ok := roles.ByName[name]
		return ok
	}

	for _, level := range levels.Order {
		for _, r := range level.AllowedRoles {
			if !knownRole(r) {
				problems = append(problems, fmt.Errorf("command level %q references role %q in allowed_roles, which is not declared in roles.yaml", level.Name, r))
			}
		}
	}

	visited := make(map[*Command]bool)
	var walk2 func(path string, tree map[string]*Command)
	walk2 = func(path string, tree map[string]*Command) {
		for name, cmd := range tree {
			if visited[cmd] {
				continue
			}
			visited[cmd] = true
			full := name
			if path != "" {
				full = path + " " + name
			}
			for _, r := range cmd.AllowedRoles {
				if !knownRole(r) {
					problems = append(problems, fmt.Errorf("command %q references role %q in allowed_roles, which is not declared in roles.yaml", full, r))
				}
			}
			walk2(full, cmd.Subcommands)
		}
	}
	for _, level := range levels.Order {
		walk2("", level.Tree)
	}

	return problems
}
