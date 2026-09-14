// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
)

// init - This function registers every command reachable from inside the
// admin Command Level.
//
// "show running-config", "show startup-config", and "erase startup-config"
// are not registered here. They live in package product and are reachable
// from admin because the admin tree file points its run directives at
// those same already registered names.
//
// "admin" and "return.admin" are the level's entry and exit commands, the
// same enter and exit shape cmd_enable.go uses.
//
// Everything else here belongs to this level: account create, account
// delete, account roles add and remove, erase users,
// restore-factory-defaults, and reload.
func init() {
	command.Register("admin", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["admin"]
		entered, err := command.EnterCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !entered {
			fmt.Println(ctx.Translator.T("admin.already_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("admin.entered"))
		return nil
	})

	command.Register("return.admin", func(ctx *command.AppContext, args []string) error {
		level := ctx.Levels.ByName["admin"]
		exited, err := command.ExitCommandLevel(ctx, level, ctx.Levels.ByName[level.Parent])
		if err != nil {
			return err
		}
		if !exited {
			fmt.Println(ctx.Translator.T("return_admin.not_here"))
			return nil
		}
		fmt.Println(ctx.Translator.T("return_admin.left"))
		return nil
	})

	command.Register("account.create", runAccountCreate)
	command.Register("account.delete", runAccountDelete)
	command.Register("account.roles.add", runAccountRolesAdd)
	command.Register("account.roles.remove", runAccountRolesRemove)
	command.Register("erase.users", runEraseUsers)
	command.Register("restore-factory-defaults", runRestoreFactoryDefaults)
	// "reload" and "reboot" are full synonyms, both registered against
	// this exact same function, sharing this exact same pending state,
	// ctx.ReloadScheduler. See runReboot's doc comment.
	command.Register("reboot", runReboot)
	command.Register("disconnect.user", runDisconnectUser)
}

// ----------------------------------------------------------------------
// account create, account delete
// ----------------------------------------------------------------------

// runAccountCreate - This function is the registered "account create"
// handler. It carries the fixed command.HandlerFunc signature, so it
// passes the real process's stdin file descriptor and stdout along to
// runAccountCreateWithIO, the same split runPasswordChange in
// cmd_password.go already uses, and for the same reason: a test can hand
// runAccountCreateWithIO a pty's slave file directly instead of needing a
// real terminal attached to the test binary.
func runAccountCreate(ctx *command.AppContext, args []string) error {
	return runAccountCreateWithIO(ctx, int(os.Stdin.Fd()), os.Stdout, args)
}

// runAccountCreateWithIO - This function drives "account create
// <username>" in one of three shapes, distinguished by how many trailing
// tokens follow the username.
//
// These could not be modeled as nested subcommands the way "totp enable"
// and "totp enable qr" are. The username is a free argument that must come
// before any keyword, and command resolution cannot skip past a token that
// failed to match a subcommand and resume keyword matching afterward.
//
//   - "account create <username>" alone prompts twice, masked, for the new
//     password, through the same flow "password change" uses, so a real
//     password is never typed on the command line or written to the audit
//     log.
//   - "account create <username> generate" generates a password meeting
//     ctx.PasswordPolicy and prints it once, the only time it is shown in
//     plain text.
//   - "account create <username> hash <hash>" imports an already computed
//     hash, never a plaintext password, for bulk resets or preloading
//     identical accounts across many devices.
//
// The first two set MustChangePassword on the new account, forcing whoever
// logs in with it straight into changing the password. The third does not,
// since an imported hash is presumed to be the intended credential rather
// than a placeholder.
func runAccountCreateWithIO(ctx *command.AppContext, fd int, stdout io.Writer, args []string) error {
	username := args[0]
	if ctx.Users == nil {
		return fmt.Errorf("%s", ctx.Translator.T("account.create.no_user_database"))
	}
	if _, exists := ctx.Users[username]; exists {
		return fmt.Errorf("%s", ctx.Translator.T("account.create.already_exists", username))
	}

	var hash string
	mustChange := false

	switch {
	case len(args) == 1:
		newPassword, err := auth.PromptNewPassword(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		confirmPassword, err := auth.PromptPasswordConfirmation(stdout, fd, ctx.Translator)
		if err != nil {
			return err
		}
		if newPassword != confirmPassword {
			return fmt.Errorf("%s", ctx.Translator.T("password.change.mismatch"))
		}
		if violations := auth.ValidatePassword(newPassword, ctx.PasswordPolicy); len(violations) > 0 {
			printPasswordViolations(ctx, violations)
			return fmt.Errorf("%s", ctx.Translator.T("account.create.policy_violation"))
		}
		h, err := auth.HashPassword(newPassword)
		if err != nil {
			return err
		}
		hash = h
		mustChange = true

	case len(args) == 2 && args[1] == "generate":
		newPassword, err := generatePassword(ctx.PasswordPolicy)
		if err != nil {
			return err
		}
		h, err := auth.HashPassword(newPassword)
		if err != nil {
			return err
		}
		hash = h
		mustChange = true
		fmt.Println(ctx.Translator.T("account.create.generated_password", newPassword))

	case len(args) == 3 && args[1] == "hash":
		if !auth.IsRecognizedHash(args[2]) {
			return fmt.Errorf("%s", ctx.Translator.T("account.create.unrecognized_hash"))
		}
		hash = args[2]
		mustChange = false

	default:
		return fmt.Errorf("%s", ctx.Translator.T("account.create.usage"))
	}

	// The pre-checks above, ctx.Users == nil and the already-exists
	// lookup, stay direct reads off ctx.Users; only the write itself runs
	// inside ctx.DaemonClient.MutateUsers, re-deriving its own working map
	// from the closure's users parameter rather than closing over
	// ctx.Users, the same discipline cmd_hostname.go's "hostname" handler
	// already follows.
	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		users[username] = &auth.User{
			Username:           username,
			PasswordHash:       hash,
			MustChangePassword: mustChange,
		}
		return nil, nil
	}); err != nil {
		return err
	}
	// This only ever changes the in memory user database, never
	// ctx.UsersFile on disk. See this file's top of file doc comment, and
	// this project's "nothing survives a restart without an explicit save"
	// core rule: a new account created here is gone again
	// the next time this deployment reloads or restarts, unless a session
	// explicitly runs "write memory" or a future equivalent first.
	ctx.Logger.Debugln("DEBUG: account", username, "created by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("account.create.confirm", username))
	return nil
}

// runAccountDelete - This function is the registered
// "account delete <username>" handler.
//
// It refuses when username is the last account holding the bypass role.
// That closes off the one unrecoverable mistake this design could
// otherwise allow: a deployment locking itself out of its own admin level
// with no way back in.
//
// "restore-factory-defaults" is the recovery path if that happens anyway,
// whether by hand editing users.yaml or otherwise.
func runAccountDelete(ctx *command.AppContext, args []string) error {
	username := args[0]
	user, exists := ctx.Users[username]
	if !exists {
		return fmt.Errorf("%s", ctx.Translator.T("account.delete.not_found", username))
	}
	if isLastBypassRoleHolder(ctx, user) {
		return fmt.Errorf("%s", ctx.Translator.T("account.delete.last_bypass_role_holder", username))
	}

	// See runAccountCreateWithIO's comment above for why only the write
	// itself, not the lookups above, runs inside
	// ctx.DaemonClient.MutateUsers.
	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		delete(users, username)
		return nil, nil
	}); err != nil {
		return err
	}
	// This only ever changes the in memory user database. A reload or
	// restart before "write memory" brings the deleted account right back.
	ctx.Logger.Debugln("DEBUG: account", username, "deleted by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("account.delete.confirm", username))
	return nil
}

// isLastBypassRoleHolder - This function reports whether user is currently
// the only account in ctx.Users holding the deployment's bypass role.
// False whenever no role in this deployment is marked bypass at all, or
// user does not hold it in the first place, there being nothing for either
// "account delete" or "account roles remove" to protect.
func isLastBypassRoleHolder(ctx *command.AppContext, user *auth.User) bool {
	if ctx.Roles == nil || ctx.Roles.BypassRole == "" {
		return false
	}
	if !hasRole(user, ctx.Roles.BypassRole) {
		return false
	}
	count := 0
	for _, u := range ctx.Users {
		if hasRole(u, ctx.Roles.BypassRole) {
			count++
		}
	}
	return count <= 1
}

// hasRole - This function reports whether user's Roles list contains name.
func hasRole(user *auth.User, name string) bool {
	for _, r := range user.Roles {
		if r == name {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------
// account roles add, account roles remove
// ----------------------------------------------------------------------

// runAccountRolesAdd - This function is the registered "account roles add
// <username> <role>" handler. role must already be declared in
// example/var/tree/roles.yaml; an unrecognized role name is refused outright
// rather than silently accepted and left to never match any AllowedRoles
// check, the same fail loudly convention this project applies to every
// other malformed request.
func runAccountRolesAdd(ctx *command.AppContext, args []string) error {
	username, roleName := args[0], args[1]
	user, exists := ctx.Users[username]
	if !exists {
		return fmt.Errorf("%s", ctx.Translator.T("account.roles.no_such_account", username))
	}
	if !roleKnown(ctx.Roles, roleName) {
		return fmt.Errorf("%s", ctx.Translator.T("account.roles.unknown_role", roleName))
	}
	if hasRole(user, roleName) {
		fmt.Println(ctx.Translator.T("account.roles.add.already_has", username, roleName))
		return nil
	}

	// See runAccountCreateWithIO's comment for why only the write itself,
	// not the lookups above, runs inside ctx.DaemonClient.MutateUsers, and
	// re-derives user from the closure's users parameter rather than
	// reusing the user pointer read above.
	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		target := users[username]
		target.Roles = append(target.Roles, roleName)
		return nil, nil
	}); err != nil {
		return err
	}
	// This only ever changes the in memory user database, never
	// ctx.UsersFile on disk.
	ctx.Logger.Debugln("DEBUG: role", roleName, "added to", username, "by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("account.roles.add.confirm", roleName, username))
	return nil
}

// runAccountRolesRemove - This function is the registered "account roles
// remove <username> <role>" handler. It carries the same last bypass role
// holder protection as "account delete", see isLastBypassRoleHolder's doc
// comment: removing a lone remaining bypass role from its own holder is
// exactly as unrecoverable as deleting that account outright would be.
func runAccountRolesRemove(ctx *command.AppContext, args []string) error {
	username, roleName := args[0], args[1]
	user, exists := ctx.Users[username]
	if !exists {
		return fmt.Errorf("%s", ctx.Translator.T("account.roles.no_such_account", username))
	}
	if !hasRole(user, roleName) {
		fmt.Println(ctx.Translator.T("account.roles.remove.does_not_have", username, roleName))
		return nil
	}
	if ctx.Roles != nil && roleName == ctx.Roles.BypassRole && isLastBypassRoleHolder(ctx, user) {
		return fmt.Errorf("%s", ctx.Translator.T("account.delete.last_bypass_role_holder", username))
	}

	kept := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		if r != roleName {
			kept = append(kept, r)
		}
	}
	// See runAccountCreateWithIO's comment for why only the write itself
	// runs inside ctx.DaemonClient.MutateUsers, re-deriving its own target
	// from the closure's users parameter rather than reusing the user
	// pointer read above.
	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		users[username].Roles = kept
		return nil, nil
	}); err != nil {
		return err
	}
	// This only ever changes the in memory user database, never
	// ctx.UsersFile on disk.
	ctx.Logger.Debugln("DEBUG: role", roleName, "removed from", username, "by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("account.roles.remove.confirm", roleName, username))
	return nil
}

// roleKnown - This function reports whether name is a role this deployment
// has declared in example/var/tree/roles.yaml. False for a nil roles, meaning no
// roles.yaml was ever loaded at all.
func roleKnown(roles *command.RoleSet, name string) bool {
	if roles == nil {
		return false
	}
	_, ok := roles.ByName[name]
	return ok
}

// ----------------------------------------------------------------------
// Password generation
// ----------------------------------------------------------------------

const (
	passwordGenMinLength = 16
	passwordGenLower     = "abcdefghijklmnopqrstuvwxyz"
	passwordGenUpper     = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	passwordGenDigits    = "0123456789"
	passwordGenSpecial   = "!@#$%^&*()-_=+"
)

// generatePassword - This function returns a random password satisfying
// policy, for "account create <username> generate".
//
// The length is the larger of policy.MinLength and sixteen characters,
// longer than whatever bare minimum a deployment configured, capped at
// MaxPasswordLength so ValidatePassword can never reject a password this
// just generated.
//
// Every character class policy requires appears at least once. The rest is
// filled from the full pool and the whole result shuffled with
// crypto/rand, so a required character never sits at a predictable
// position.
func generatePassword(policy auth.PasswordPolicy) (string, error) {
	length := policy.MinLength
	if length < passwordGenMinLength {
		length = passwordGenMinLength
	}
	if length > auth.MaxPasswordLength {
		length = auth.MaxPasswordLength
	}

	pool := passwordGenLower + passwordGenDigits
	var required []byte

	if policy.RequireUppercase {
		pool += passwordGenUpper
		c, err := randomChar(passwordGenUpper)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}
	if policy.RequireNumbers {
		c, err := randomChar(passwordGenDigits)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}
	if policy.RequireSpecialChars {
		pool += passwordGenSpecial
		c, err := randomChar(passwordGenSpecial)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}

	remaining := length - len(required)
	if remaining < 0 {
		remaining = 0
	}
	buf := make([]byte, 0, length)
	buf = append(buf, required...)
	for i := 0; i < remaining; i++ {
		c, err := randomChar(pool)
		if err != nil {
			return "", err
		}
		buf = append(buf, c)
	}

	shuffled, err := shuffleBytes(buf)
	if err != nil {
		return "", err
	}
	return string(shuffled), nil
}

// randomChar - This function returns one byte chosen uniformly at random
// from charset, using crypto/rand rather than math/rand, the same
// reasoning every other secret this project generates, auth.HashPassword's
// salt among them, already follows: a generated password is a real
// credential, not test data.
func randomChar(charset string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	if err != nil {
		return 0, err
	}
	return charset[n.Int64()], nil
}

// shuffleBytes - This function returns a copy of b in a random order, a
// standard Fisher-Yates shuffle driven by crypto/rand, so
// generatePassword's required characters, appended in a fixed order above,
// do not predictably end up at the front of every generated password.
func shuffleBytes(b []byte) ([]byte, error) {
	out := make([]byte, len(b))
	copy(out, b)
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return out, nil
}

// ----------------------------------------------------------------------
// erase users, restore-factory-defaults, reload
// ----------------------------------------------------------------------

// restoreFromDefaults - This function overwrites live with a fresh copy of
// ctx.DefaultsDir's skeleton file, matched to live's base name, for
// example example/etc/defaults/users.yaml restoring example/etc/users.yaml. restored is
// false, with a nil error, when no matching default file exists at all, a
// deployment that never set one up for this particular file, distinct from
// a real error reading or writing one that does exist.
func restoreFromDefaults(ctx *command.AppContext, live string, perm os.FileMode) (restored bool, err error) {
	def := filepath.Join(ctx.DefaultsDir, filepath.Base(live))
	data, err := os.ReadFile(def)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading default file %q: %w", def, err)
	}
	if dir := filepath.Dir(live); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return false, fmt.Errorf("preparing directory for %q: %w", live, err)
		}
	}
	if err := os.WriteFile(live, data, perm); err != nil {
		return false, fmt.Errorf("writing %q: %w", live, err)
	}
	return true, nil
}

// runEraseUsers - This function is the registered "erase users" handler.
//
// Unlike "erase startup-config", which deletes its own file, deleting
// users.yaml would permanently lock every account, the bypass role
// included, out of this level and out of the deployment's whole identity
// database. This replaces it with the skeleton copy from the defaults
// directory instead, then reloads it so the current session immediately
// sees the restored account set.
//
// reloadFromDisk reassigns ctx.Users to a freshly loaded map and always
// ends the process right afterward. "erase users" does not end the
// session, so reassigning the same way would silently desync ctx.Users
// from the Store, which would go on holding the map from before the erase.
//
// Clearing and repopulating instead keeps the same map object that both
// ctx.Users and the Store already point at, identity intact, so the two
// never diverge.
func runEraseUsers(ctx *command.AppContext, args []string) error {
	restored, err := restoreFromDefaults(ctx, ctx.UsersFile, 0600)
	if err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("erase_users.failed", err))
	}
	if !restored {
		fmt.Println(ctx.Translator.T("erase_users.no_defaults"))
		return nil
	}
	freshUsers, err := auth.LoadUsers(ctx.UsersFile)
	if err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("erase_users.failed", err))
	}
	if _, err := ctx.DaemonClient.MutateUsers(func(users auth.Users) (any, error) {
		clear(users)
		for username, user := range freshUsers {
			users[username] = user
		}
		return nil, nil
	}); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("erase_users.failed", err))
	}
	ctx.Logger.Debugln("DEBUG: users.yaml restored to factory defaults by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("erase_users.confirm"))
	return nil
}

// runRestoreFactoryDefaults - This function is the registered
// "restore-factory-defaults" handler. It is the recovery path once
// "account delete" or "account roles remove" has refused as far as it
// will, or when a deployment wants a genuine factory reset.
//
// It erases startup-config, restores users.yaml and roles.yaml from the
// defaults directory, then runs the same work a reboot does, ending this
// connection so the next one starts fresh.
//
// A missing default for roles.yaml is not an error. A deployment that
// never used AllowedRoles may reasonably have none.
func runRestoreFactoryDefaults(ctx *command.AppContext, args []string) error {
	if err := os.Remove(ctx.StartupConfigFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s", ctx.Translator.T("restore_factory_defaults.failed", err))
	}
	if _, err := restoreFromDefaults(ctx, ctx.UsersFile, 0600); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("restore_factory_defaults.failed", err))
	}
	if _, err := restoreFromDefaults(ctx, ctx.RolesFile, 0640); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("restore_factory_defaults.failed", err))
	}
	ctx.Logger.Debugln("DEBUG: factory defaults restored by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("restore_factory_defaults.confirm"))
	return runReboot(ctx, nil)
}

// runReboot - The registered "reboot" handler. "reload" is an alias for it
// in the tree, so both words reach here and share one pending state.
//
// The workflow is the familiar one: schedule a reboot with a delay, make a
// risky change, then cancel before the timer fires if it worked, or let it
// fire and fall back to the last saved configuration.
//
// Three shapes, by ctx.Negated and args:
//
//   - Negated cancels a scheduled reboot, and errors if none was pending.
//   - With a seconds argument, schedules this handler to run itself later
//     and returns at once, leaving the session free to keep working.
//   - Otherwise runs immediately. See performReboot.
//
// The delay is a timer inside this process, not a request to anything.
// runLoop notices it fire and ends the session, since this handler has
// already returned by then.
//
// runRestoreFactoryDefaults calls this directly with nil args.
func runReboot(ctx *command.AppContext, args []string) error {
	if ctx.Negated {
		if ctx.ReloadScheduler == nil || !ctx.ReloadScheduler.Cancel() {
			return fmt.Errorf("%s", ctx.Translator.T("reboot.not_pending"))
		}
		ctx.Logger.Debugln("DEBUG: scheduled reload cancelled by", ctx.Session.Username)
		fmt.Println(ctx.Translator.T("reboot.cancelled"))
		return nil
	}

	if len(args) == 1 {
		seconds, err := strconv.Atoi(args[0])
		if err != nil || seconds <= 0 {
			return fmt.Errorf("%s", ctx.Translator.T("reboot.invalid_delay", args[0]))
		}
		if ctx.ReloadScheduler == nil {
			return fmt.Errorf("%s", ctx.Translator.T("reboot.not_supported"))
		}
		ctx.ReloadScheduler.Schedule(time.Duration(seconds) * time.Second)
		ctx.Logger.Debugln("DEBUG: reload scheduled by", ctx.Session.Username, "in", seconds, "seconds")
		fmt.Println(ctx.Translator.T("reboot.scheduled", seconds))
		return nil
	}

	return performReboot(ctx)
}

// performReboot - This function is runReboot's immediate branch, the no args,
// not negated case, separated out so its daemon aware decision reads
// clearly on its own.
//
// A deployment configured with a daemon, meaning DaemonSocketPath is not
// empty, asks that daemon to reboot rather than doing the standalone, one
// session work itself.
//
// A deployment with no daemon configured re-reads users.yaml, roles.yaml,
// and startup-config fresh from disk, rebuilds this session's in memory
// state from them, and then ends the connection through command.ErrQuit,
// the same sentinel "exit" returns at the base level, forcing a reconnect.
func performReboot(ctx *command.AppContext) error {
	err := ctx.DaemonClient.Reboot()
	if err == nil {
		// The daemon accepted the request and is already rereading its own
		// state from disk. It is about to broadcast a rebooting farewell to
		// every attached session, this one included.
		//
		// So this session ends asynchronously, through its FarewellChannel,
		// not through this call returning. Nothing more is printed here.
		//
		// ErrQuit is not returned, since that would end this one session
		// immediately, ahead of and separately from the farewell every other
		// session receives the same way.
		return nil
	}
	if !errors.Is(err, command.ErrDaemonNotConfigured) {
		return fmt.Errorf("%s", ctx.Translator.T("reboot.failed", err))
	}
	// No real daemon backs this deployment; fall back to exactly the
	// standalone behavior this command already had before daemon support
	// existed at all: reread from disk and end this one session alone, the
	// same reasoning runReboot's doc comment already gives for why
	// RouterCLI, standalone, has no other connected session to notify
	// anyway.
	if err := reloadFromDisk(ctx); err != nil {
		return err
	}
	ctx.Logger.Debugln("DEBUG: reload run by", ctx.Session.Username)
	fmt.Println(ctx.Translator.T("reboot.confirm"))
	return command.ErrQuit
}

// reloadFromDisk - The shared work behind an immediate reboot, called by
// performReboot and by ReloadFromDiskForRestart.
//
// It re-reads users.yaml, roles.yaml, and startup-config from disk and
// rebuilds ctx from them. It neither ends the session nor prints anything;
// each caller does that its own way.
//
// ctx.Users and ctx.Roles are reassigned directly rather than through
// MutateUsers or MutateRoles, because both callers end the process
// immediately afterward. Nothing can observe the difference, and routing a
// wholesale replacement through a mutation closure would mean rebuilding
// the same map twice.
//
// If that ever stops being true, and something continues running after
// this returns, these two assignments MUST move inside the closures.
func reloadFromDisk(ctx *command.AppContext) error {
	if ctx.UsersFile != "" {
		users, err := auth.LoadUsers(ctx.UsersFile)
		if err != nil {
			return fmt.Errorf("%s", ctx.Translator.T("reboot.failed", err))
		}
		ctx.Users = users
	}
	roles, err := command.LoadRoles(ctx.RolesFile)
	if err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("reboot.failed", err))
	}
	ctx.Roles = roles
	if err := command.LoadStartupConfig(ctx, ctx.StartupConfigFile); err != nil {
		return fmt.Errorf("%s", ctx.Translator.T("reboot.failed", err))
	}
	return nil
}

// ReloadFromDiskForRestart - This function lets main.go perform a
// scheduled reboot's file re-reading once its timer fires uncancelled.
//
// It exists for the same reason RunPasswordChange does: main.go never
// resolves and dispatches a command by name, so it needs a real function
// to call from a path with no Command or typed args to route through
// runReboot.
func ReloadFromDiskForRestart(ctx *command.AppContext) error {
	return reloadFromDisk(ctx)
}

// ----------------------------------------------------------------------
// disconnect user
// ----------------------------------------------------------------------

// runDisconnectUser - This function is the registered
// "disconnect user <username> [<session-id>]" handler. It sits in admin
// beside "reboot", gated the same way, because it carries the same weight.
//
// args[0] is the username, guaranteed by the tree file's minargs. args[1],
// a session ID naming one session exactly, appears only when someone typed
// one, usually to resolve ambiguity a bare attempt already reported.
//
// DisconnectUser does the work and the disambiguation, refusing with a
// candidate listing error when a username matches more than one session.
// This handler keeps no session registry of its own.
//
// ErrDaemonNotConfigured gets its own message rather than the generic
// failure text. A deployment with no daemon should have this command
// pruned from its tree entirely, so a session that somehow reaches this
// handler, a stale binary against fresh configuration for instance,
// deserves an honest explanation rather than something reading like a real
// per-user failure.
func runDisconnectUser(ctx *command.AppContext, args []string) error {
	username := args[0]
	sessionID := ""
	if len(args) > 1 {
		sessionID = args[1]
	}

	if err := ctx.DaemonClient.DisconnectUser(username, sessionID); err != nil {
		if errors.Is(err, command.ErrDaemonNotConfigured) {
			return fmt.Errorf("%s", ctx.Translator.T("disconnect.user.not_supported"))
		}
		return fmt.Errorf("%s", ctx.Translator.T("disconnect.user.failed", err))
	}
	ctx.Logger.Debugln("DEBUG: disconnect user run by", ctx.Session.Username, "against", username)
	fmt.Println(ctx.Translator.T("disconnect.user.confirm", username))
	return nil
}
