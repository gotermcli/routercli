// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package core implements the command handlers that are useful to almost
any deployment built on RouterCLI, without being tied to one vendor's
command set or one deployment's application state. Login and session
management, elevating a session to a more privileged Command Level,
moving into and out of configuration mode, and password and TOTP self
service all belong here.

Package product holds the optional Cisco and HP flavored demonstration
commands. Package session holds the handlers that are local to one
connection, such as terminal paging and output filtering.

Nothing in this package requires package product, and nothing in package
product requires this package. A deployment MAY drop either import,
replace package product entirely, or add packages of its own alongside
both. Package command knows nothing about either one.

Every file here starts with cmd_ and can be deleted if a deployment does
not want what it provides. Importing this package, even as a blank
import, is what loads every handler in the directory, because each file
registers itself from its own init function.

Keep one file per command, or at least keep everything in a file closely
related to one command.

# Writing a New Command

A command has three parts: a name, a handler function matching
command.HandlerFunc, and a tree entry connecting the two.

Pick a reference name. It MUST match exactly on the Go side and the YAML
side.

Create a cmd_<name>.go file with an init function that calls
command.Register("<name>", handlerFunc). The file cmd_mode_control.go is
one of the simplest complete examples.

Create an entry in the appropriate var/tree/level_*.yaml file with a
"run: <name>" directive using that same string. The full field list,
including desc, minargs, maxargs, negatable, and hidden, is documented in
var/tree/README.md.

Both sides MUST exist and the names MUST match. A tree entry whose run
value was never registered is a startup error, not a runtime error the
first time someone types the command. A typo in either file stops the
program from starting rather than silently producing a command that does
nothing.

# The Handler Signature

	type HandlerFunc func(ctx *AppContext, args []string) error

ctx carries everything a handler needs about the running session: State
for a deployment's data, Logger, Session for login and elevation
state, Levels for every Command Level defined, Position for the current
Command Level stack, Translator, Negated, and Audit. A handler reads what
it needs and never constructs or replaces any of it.

args holds whatever tokens followed the command name. By the time the
handler runs, args has already been validated against the MinArgs,
MaxArgs, and MaxArgLength the tree entry specified. A handler for a
command with minargs: 1 can index args[0] directly, because a wrong count
means the handler was never called.

A non-nil error is printed back to the user with a leading "%", matching
Cisco and HP convention, and recorded in the audit log as a failure. Use
fmt.Errorf with a translated message for anything a real user might see.
A bare error is fine for something that should never happen, such as a
case the tree structure already made unreachable.

# Application State

ctx.State is declared as any, because package command has no idea what a
deployment will track. No handler in this package touches it. Everything
here operates on framework level values, which keeps this package useful
to a deployment whose state looks nothing like the example in package
product.

This package does hold the handlers that mutate the other three pieces of
shared, potentially daemon backed state: ctx.Levels, ctx.Users, and
ctx.Roles. Each goes through the matching ctx.DaemonClient mutation
method rather than writing the map or struct directly, so the change
lands in the daemon when one is configured and in process memory when one
is not.

# Negation

Cisco and HP style CLIs undo most configuration commands with a leading
"no", such as "no shutdown", rather than defining a separate
"no-shutdown" command. A tree entry opts in with "negatable: true", and
ctx.Negated is true for the duration of that one handler call.

A handler that supports negation checks it first:

	if ctx.Negated {
	        // Undo whatever this command normally does.
	        return nil
	}
	// Normal behavior.

# Entering a New Command Level

Some commands change which commands are reachable next, the way
"configure terminal" moves a session into config mode. That is a stack
push:

	ctx.Position.Push(command.CommandLevelFrame{
	        Name:         "config",
	        PromptSuffix: level.PromptSuffix,
	        Tree:         level.Tree,
	})

# enable, exec, and Privileged Mode

This package registers "enable" and "disable", elevating a session into a
Command Level named "exec". This follows the two step unprivileged then
privileged model real Cisco and HP devices use.

It is a convention, not a requirement. The name "exec" is not special
cased anywhere in package command. EnterCommandLevel, ExitCommandLevel,
and VerifyCommandLevels all work from whatever names
var/tree/tree_structure.yaml declares.

A deployment MAY rename this level, add further privilege tiers, or drop
the idea of a privileged mode entirely and run every command out of base.

# Command Levels

Every Command Level's enter and exit command is a hand written
cmd_<name>.go file. A deployment adds a level by writing one small file
following the same pattern, never by editing package command.

A root swap level, where only one of base or exec is active at a time,
calls EnterCommandLevel and ExitCommandLevel. Those functions do the
mechanical work: the parent check, the password check, the
Session.CommandLevel update, and swapping the root stack frame. They
report what happened and print nothing.

A nested, stacking mode, where config and a level built on top of it are
both active at once, calls RequireCurrentCommandLevel and pushes its own
stack frame, because a root swap cannot express two active levels.

Each file decides what to print, log, or audit for its own outcomes. The
string "Entering exec mode." lives in this package's i18n keys, not in
package command, so a different deployment can print something else, log
it instead, or say nothing.

The enter_command and exit_command fields in tree_structure.yaml are
declared metadata. VerifyCommandLevels confirms every declared name was
registered, catching a typo or a forgotten file at startup. Nothing in
package command registers a command from that data.

The file cmd_mode_control.go registers "exit" and "end", which leave a
nested level back to its parent or all the way to the root. Both are
ordinary commands with no special casing for which level was current.

# The user Command Level

The file cmd_user.go registers a Command Level named user. It is a
nested, stacking mode like config, but its parent is base rather than
exec, so it is reachable directly after logging in without running
"enable".

It adds one check no other level here needs. Entry is refused unless
ctx.Session.Authenticated is true, because every command inside acts on
the current session's entry in the user database, which only means
something for a session that logged in as somebody.

This is where a session manages its own account: its second factor
through cmd_totp.go and its password through cmd_password.go.

The file cmd_totp.go registers totp enable, totp enable qr, and totp
disable. The plain form shows the manually typed secret; the qr form also
shows a scannable QR code. Both share one interactive body and differ
only in which display function runs first.

These commands update a user's TOTP secret in memory through
ctx.DaemonClient.MutateUsers. Nothing survives a restart without an
explicit save, so a session MUST run "write memory" for an enrolled or
removed second factor to persist.

Every handler here splits its interactive half from its verify and save
half, so the verify and save decision is unit testable with a known code
and a fixed time. A rejected code is retried up to ctx.TOTPMaxAttempts
times rather than ejecting the session after one mistake.

Once a confirmation code is accepted, or every retry is used up,
runTOTPEnable clears the screen and the terminal scrollback, so the
freshly printed secret and QR code are not readable by whoever looks at
that terminal next.
*/
package core
