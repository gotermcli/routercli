// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"strings"
	"testing"
)

// TestVerifyCommandLevelsPassesWhenEverythingRegistered - This test
// verifies that a manifest whose enter_command and exit_command are both
// registered handlers produces no problems.
func TestVerifyCommandLevelsPassesWhenEverythingRegistered(t *testing.T) {
	registerTestHandlers()
	Register("test-verify-enter", func(*AppContext, []string) error { return nil })
	Register("test-verify-exit", func(*AppContext, []string) error { return nil })

	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    enter_command: test-verify-enter
    exit_command: test-verify-exit
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	if problems := VerifyCommandLevels(levels); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

// TestVerifyCommandLevelsCatchesMissingEnterCommand - This test verifies
// the rule that every non-base level must declare enter_command.
// LoadTreeStructure itself does not enforce this, only this separate
// verification pass does.
func TestVerifyCommandLevelsCatchesMissingEnterCommand(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyCommandLevels(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (missing enter_command), got %d: %v", len(problems), problems)
	}
}

// TestVerifyCommandLevelsCatchesUnregisteredEnterCommand - This test
// verifies the case where the manifest names a command nobody wrote. This
// is the mistake this whole check exists to catch, a typo in the manifest
// or a forgotten cmd_*.go file, instead of it only surfacing the first
// time a user types the command.
func TestVerifyCommandLevelsCatchesUnregisteredEnterCommand(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    enter_command: this-was-never-registered-anywhere
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyCommandLevels(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (unregistered enter_command), got %d: %v", len(problems), problems)
	}
}

// TestVerifyCommandLevelsCatchesUnregisteredExitCommand - This test
// verifies that a declared exit_command naming a handler nobody registered
// is caught, the same as an unregistered enter_command.
func TestVerifyCommandLevelsCatchesUnregisteredExitCommand(t *testing.T) {
	registerTestHandlers()
	Register("test-verify-exit-only-enter", func(*AppContext, []string) error { return nil })

	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    enter_command: test-verify-exit-only-enter
    exit_command: this-was-never-registered-either
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyCommandLevels(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (unregistered exit_command), got %d: %v", len(problems), problems)
	}
}

// TestVerifyCommandLevelsSkipsTheBaseLevel - This test verifies that a
// base level, which has no EnterCommand by definition, is never reported
// as a problem for lacking one.
func TestVerifyCommandLevelsSkipsTheBaseLevel(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	if problems := VerifyCommandLevels(levels); len(problems) != 0 {
		t.Errorf("expected no problems for a base only manifest, got %v", problems)
	}
}

// TestVerifyCommandLevelsExitCommandIsOptional - This test verifies that a
// level with no exit_command declared at all, left through the generic
// "exit" or "end" instead, is never reported as a problem.
func TestVerifyCommandLevelsExitCommandIsOptional(t *testing.T) {
	registerTestHandlers()
	Register("test-verify-no-exit-cmd", func(*AppContext, []string) error { return nil })

	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	nestedTree := writeTree(t, "  hostname:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  config:
    tree_file: `+nestedTree+`
    parent: operator
    enter_command: test-verify-no-exit-cmd
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	if problems := VerifyCommandLevels(levels); len(problems) != 0 {
		t.Errorf("expected no problems when exit_command is simply omitted, got %v", problems)
	}
}

// ----------------------------------------------------------------------
//
// VerifyVendorDefinedSecrets
//
// ----------------------------------------------------------------------

// TestVerifyVendorDefinedSecretsPassesWhenNoneSet - This test verifies
// that an ordinary manifest, nobody using VendorDefinedPasswordHash at
// all, produces no problems, the common case every existing tree file in
// this project is in today.
func TestVerifyVendorDefinedSecretsPassesWhenNoneSet(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	if problems := VerifyVendorDefinedSecrets(levels); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

// TestVerifyVendorDefinedSecretsPassesLevelFullyValid - This test verifies
// that a level meeting every rule at once, Hidden true, VendorRestricted
// true, PasswordUserSettable left nil, and no PasswordHash also set,
// produces no problems.
func TestVerifyVendorDefinedSecretsPassesLevelFullyValid(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	levels.ByName["operator"].VendorDefinedPasswordHash = "$6$$vendorhash"
	levels.ByName["operator"].Hidden = true
	levels.ByName["operator"].VendorRestricted = true

	if problems := VerifyVendorDefinedSecrets(levels); len(problems) != 0 {
		t.Errorf("expected no problems for a fully valid vendor defined level, got %v", problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesLevelBothHashesSet - This test
// verifies rule 1: a level must not set both PasswordHash and
// VendorDefinedPasswordHash at once, since EffectivePasswordHash would
// then silently make PasswordHash dead configuration.
func TestVerifyVendorDefinedSecretsCatchesLevelBothHashesSet(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	level := levels.ByName["operator"]
	level.PasswordHash = "$6$$ordinary"
	level.VendorDefinedPasswordHash = "$6$$vendorhash"
	level.Hidden = true           // isolate the "both set" rule from the "not hidden" rule
	level.VendorRestricted = true // isolate the "both set" rule from the "not vendor_restricted" rule

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (both hashes set), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesLevelUserSettableTrue - This test
// verifies rule 2: a level must not set PasswordUserSettable true
// alongside a VendorDefinedPasswordHash.
func TestVerifyVendorDefinedSecretsCatchesLevelUserSettableTrue(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	level := levels.ByName["operator"]
	level.VendorDefinedPasswordHash = "$6$$vendorhash"
	level.Hidden = true           // isolate the "user settable true" rule from the "not hidden" rule
	level.VendorRestricted = true // isolate the "user settable true" rule from the "not vendor_restricted" rule
	yes := true
	level.PasswordUserSettable = &yes

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (password_user_settable: true), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesLevelNotHidden - This test verifies
// rule 3 and rule 4 together: a level left with Hidden false fails both
// "hidden must be true alongside vendor_defined_password_hash" and "hidden
// must be true alongside vendor_restricted" at once, once VendorRestricted
// is set the way rule 5 now always requires whenever
// VendorDefinedPasswordHash is set. The two problems share one root cause,
// a missing hidden: true, rather than being two independent mistakes.
func TestVerifyVendorDefinedSecretsCatchesLevelNotHidden(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	levels.ByName["operator"].VendorDefinedPasswordHash = "$6$$vendorhash"
	levels.ByName["operator"].VendorRestricted = true
	// Hidden left at its zero value, false.

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 2 {
		t.Fatalf("expected exactly 2 problems (not hidden, reported once for the vendor defined hash and once for vendor_restricted), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesLevelNotVendorRestricted - This
// test verifies rule 5: a level must set VendorRestricted true alongside a
// VendorDefinedPasswordHash, since a "<HIDDEN>" placeholder in "show
// running-config" or "show startup-config" still reveals that a locked
// level exists, which VendorRestricted alone prevents. See
// CommandLevel.VendorRestricted's doc comment.
func TestVerifyVendorDefinedSecretsCatchesLevelNotVendorRestricted(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	level := levels.ByName["operator"]
	level.VendorDefinedPasswordHash = "$6$$vendorhash"
	level.Hidden = true // isolate the "not vendor_restricted" rule from the "not hidden" rule
	// VendorRestricted left at its zero value, false.

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (not vendor_restricted), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesLevelVendorRestrictedNotHidden
// -This test verifies rule 4 in isolation: VendorRestricted true requires
// Hidden true, even on a level carrying no VendorDefinedPasswordHash of
// its own at all. A level like this, meant only to hold state a vendor's
// tooling manages, still needs to stay out of ordinary discovery the same
// way a password gated one does.
func TestVerifyVendorDefinedSecretsCatchesLevelVendorRestrictedNotHidden(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	levels.ByName["operator"].VendorRestricted = true
	// No VendorDefinedPasswordHash at all, and Hidden left at its zero
	// value, false.

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (vendor_restricted without hidden), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsCatchesCommandViolations - This test
// verifies that the same three rules apply to an individual Command
// reachable inside a level's Tree, not just to the level itself, mirroring
// TestVerifyVendorDefinedSecretsCatchesLevelBothHashesSet and its siblings
// above but for Command.Hidden instead of CommandLevel.Hidden.
func TestVerifyVendorDefinedSecretsCatchesCommandViolations(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	show := levels.ByName["operator"].Tree["show"]
	show.VendorDefinedPasswordHash = "$6$$vendorhash"
	// Hidden left false, PasswordUserSettable left nil (so only the "not
	// hidden" rule fires here, matching the level level test above's
	// isolation approach).

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem (command not hidden), got %d: %v", len(problems), problems)
	}
}

// TestVerifyVendorDefinedSecretsDedupesSharedCommand - This test verifies
// that a command reachable through more than one level's merged tree, via
// InheritParent, the identical *Command pointer in each, see MergeTrees's
// doc comment, is only ever reported once, not once per level that happens
// to inherit it.
func TestVerifyVendorDefinedSecretsDedupesSharedCommand(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	execTree := writeTree(t, "  configure:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    inherit_parent: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	// "show" is defined once, in operator's tree file, and reachable from
	// both operator and exec, since exec inherits.
	show := levels.ByName["operator"].Tree["show"]
	if levels.ByName["exec"].Tree["show"] != show {
		t.Fatal("test setup error: expected exec to inherit the identical *Command pointer for \"show\"")
	}
	show.VendorDefinedPasswordHash = "$6$$vendorhash"
	// Hidden left false, so exactly one rule fires, and the dedupe this
	// test is about is whether it fires once or twice.

	problems := VerifyVendorDefinedSecrets(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem, a shared command reported once regardless of how many levels reach it, got %d: %v", len(problems), problems)
	}
}

// ----------------------------------------------------------------------
// VerifyRoles
// ----------------------------------------------------------------------

// TestVerifyRolesPassesWhenNoneReferenced - This test verifies that a tree
// setting no allowed_roles anywhere, the state of every existing tree file
// in this project before AllowedRoles existed at all, produces no
// problems, even with a nil RoleSet.
func TestVerifyRolesPassesWhenNoneReferenced(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	if problems := VerifyRoles(levels, nil); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

// TestVerifyRolesPassesWhenEveryReferenceIsDeclared - This test verifies
// that a CommandLevel and a Command, each setting allowed_roles to a role
// that is declared in the RoleSet, produce no problems.
func TestVerifyRolesPassesWhenEveryReferenceIsDeclared(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n    allowed_roles: [operator]\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
    allowed_roles: [admin]
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	roles := &RoleSet{ByName: map[string]*Role{
		"admin":    {Name: "admin", Bypass: true},
		"operator": {Name: "operator"},
	}}

	if problems := VerifyRoles(levels, roles); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

// TestVerifyRolesCatchesUnknownLevelRole - This test verifies that a
// CommandLevel's allowed_roles referencing a role not declared in
// roles.yaml is caught, rather than silently accepted and left to never
// match any real user's roles.
func TestVerifyRolesCatchesUnknownLevelRole(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
    allowed_roles: [ghost-role]
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyRoles(levels, &RoleSet{ByName: map[string]*Role{}})
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem for an unknown level role, got %d: %v", len(problems), problems)
	}
}

// TestVerifyRolesCatchesUnknownCommandRole - This test verifies that a
// Command's allowed_roles referencing an undeclared role is caught the
// same way a level's is.
func TestVerifyRolesCatchesUnknownCommandRole(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n    allowed_roles: [ghost-role]\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyRoles(levels, &RoleSet{ByName: map[string]*Role{}})
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem for an unknown command role, got %d: %v", len(problems), problems)
	}
}

// TestVerifyRolesCatchesUnknownRoleWithNilRoleSet - This test verifies
// that a nil RoleSet, a deployment with no RolesFile at all, still catches
// an allowed_roles reference somewhere in the tree, since there is nothing
// at all for such a reference to be validated against.
func TestVerifyRolesCatchesUnknownRoleWithNilRoleSet(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
    allowed_roles: [admin]
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	problems := VerifyRoles(levels, nil)
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem with a nil RoleSet and a real allowed_roles reference, got %d: %v", len(problems), problems)
	}
}

// TestVerifyRolesDedupesSharedCommand - This test verifies that a Command
// reachable from more than one CommandLevel, through inherit_parent, is
// only reported once for an unknown role, matching
// VerifyVendorDefinedSecrets' own dedupe behavior.
func TestVerifyRolesDedupesSharedCommand(t *testing.T) {
	registerTestHandlers()
	opTree := writeTree(t, "  show:\n    run: test.noop\n    allowed_roles: [ghost-role]\n")
	execTree := writeTree(t, "  hostname:\n    run: test.noop\n")
	common := emptyCommonTree(t)
	manifest := writeManifest(t, `
  operator:
    tree_file: `+opTree+`
    is_base: true
  exec:
    tree_file: `+execTree+`
    parent: operator
    inherit_parent: true
`)
	levels, err := LoadTreeStructure(manifest, common)
	if err != nil {
		t.Fatalf("LoadTreeStructure: %v", err)
	}

	show := levels.ByName["operator"].Tree["show"]
	if levels.ByName["exec"].Tree["show"] != show {
		t.Fatal("test setup error: expected exec to inherit the identical *Command pointer for \"show\"")
	}

	problems := VerifyRoles(levels, &RoleSet{ByName: map[string]*Role{}})
	if len(problems) != 1 {
		t.Fatalf("expected exactly 1 problem, a shared command reported once regardless of how many levels reach it, got %d: %v", len(problems), problems)
	}
}

// TestVerifyRegisteredCommandsAreReachableAcceptsAReferencedHandler - This
// test verifies that a handler a tree file runs is not reported, whether
// it is referenced through a run directive or as a level's enter command.
func TestVerifyRegisteredCommandsAreReachableAcceptsAReferencedHandler(t *testing.T) {
	restore := swapRegistry(map[string]HandlerFunc{
		"reachable.by.run":   noopRun,
		"reachable.by.enter": noopRun,
		"reachable.by.exit":  noopRun,
	})
	defer restore()

	levels := &TreeStructure{
		Order: []*CommandLevel{{
			Name:         "exec",
			EnterCommand: "reachable.by.enter",
			ExitCommand:  "reachable.by.exit",
			Tree: map[string]*Command{
				"show": {Subcommands: map[string]*Command{
					"version": {Run: "reachable.by.run"},
				}},
			},
		}},
	}

	if problems := VerifyRegisteredCommandsAreReachable(levels); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

// TestVerifyRegisteredCommandsAreReachableReportsAnOrphan - This test
// verifies that a handler nothing references is reported. That is a
// command file registering a name no tree file ever runs, which no Go
// analyzer can see, since the registration itself is live code.
func TestVerifyRegisteredCommandsAreReachableReportsAnOrphan(t *testing.T) {
	restore := swapRegistry(map[string]HandlerFunc{
		"used":     noopRun,
		"orphaned": noopRun,
	})
	defer restore()

	levels := &TreeStructure{
		Order: []*CommandLevel{{
			Name: "base",
			Tree: map[string]*Command{"thing": {Run: "used"}},
		}},
	}

	problems := VerifyRegisteredCommandsAreReachable(levels)
	if len(problems) != 1 {
		t.Fatalf("expected exactly one problem, got %v", problems)
	}
	if !strings.Contains(problems[0].Error(), "orphaned") {
		t.Errorf("problem should name the orphaned handler, got %q", problems[0])
	}
}

// TestVerifyRegisteredCommandsAreReachableIsDeterministic - This test
// verifies the reported order is stable. registry is a map, so without
// sorting, two runs of --check-config would list the same problems in
// different orders.
func TestVerifyRegisteredCommandsAreReachableIsDeterministic(t *testing.T) {
	restore := swapRegistry(map[string]HandlerFunc{
		"aaa": noopRun, "bbb": noopRun, "ccc": noopRun, "ddd": noopRun,
	})
	defer restore()

	levels := &TreeStructure{Order: []*CommandLevel{{Name: "base"}}}

	first := VerifyRegisteredCommandsAreReachable(levels)
	for i := 0; i < 20; i++ {
		again := VerifyRegisteredCommandsAreReachable(levels)
		if len(again) != len(first) {
			t.Fatalf("problem count changed between runs: %d then %d", len(first), len(again))
		}
		for j := range first {
			if first[j].Error() != again[j].Error() {
				t.Fatalf("problem order changed between runs at %d: %q then %q", j, first[j], again[j])
			}
		}
	}
}

// swapRegistry - This function replaces the package level registry for the
// duration of one test and returns a function restoring it. The registry is
// package level, populated by every cmd package's init, so a test needs a
// known set rather than whatever happens to be linked in.
func swapRegistry(with map[string]HandlerFunc) func() {
	saved := registry
	registry = with
	return func() { registry = saved }
}
