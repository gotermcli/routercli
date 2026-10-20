// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package product implements a working set of Cisco and HP flavored
demonstration commands: hostname, interface configuration, a trivial
diagnostic self test, and the "set description" and "show *" commands
that report this package's state back out.

Nothing here is part of the reusable framework, and nothing here is
required by it. Package core holds the handlers that are broadly useful
across deployments with nothing to do with network gear. Package session
holds the settings local to one connection.

This package exists to be replaced. A deployment not modeling network
equipment, or simply wanting a different command set, removes this
import, deletes this directory, and writes its own following the patterns
below. The dependency runs one way: this package sits on top of package
command and package core, never the reverse.

Every file here starts with cmd_ and can be deleted individually. Each
file registers itself from its own init function, so importing this
package is what loads them all.

Keep one file per command, or at least keep everything in a file closely
related to one command.

# Writing a New Command

A command has three parts: a name, a handler function matching
command.HandlerFunc, and a tree entry connecting the two.

Pick a reference name. It MUST match exactly on the Go side and the YAML
side.

Create a cmd_<name>.go file with an init function that calls
command.Register("<name>", handlerFunc). The file cmd_hostname.go is the
simplest complete example.

Create an entry in the appropriate var/tree/level_*.yaml file with a
"run: <name>" directive using that same string. The full field list is
documented in var/tree/README.md.

Both sides MUST exist and the names MUST match. A tree entry whose run
value was never registered is a startup error rather than a runtime
error the first time someone types the command.

# The Handler Signature

	type HandlerFunc func(ctx *AppContext, args []string) error

ctx carries everything a handler needs about the running session. args
holds whatever tokens followed the command name, already validated
against the MinArgs, MaxArgs, and MaxArgLength the tree entry specified.

A non-nil error is printed back to the user with a leading "%", matching
Cisco and HP convention, and recorded in the audit log as a failure.

# Application State

The State type is defined in model.go. It is a plain struct, one field
per piece of running configuration style state, such as Hostname,
Description, and per interface state. A deployment extending or replacing
this package adds fields to its own equivalent type.

Every handler here that writes state reaches it through
ctx.DaemonClient.MutateProductState rather than a direct type assertion
on ctx.State. The same handler then works correctly whether the
deployment is standalone or backed by a daemon.

	_, err := ctx.DaemonClient.MutateProductState(func(productState any) (any, error) {
	        state := productState.(*State)
	        // Mutate state here.
	        return nil, nil
	})
	return err

Three rules keep this correct against a real daemon:

Resolve state from the closure's productState parameter every time, never
from a pointer read or captured before the closure ran. A remote daemon
hands the closure a freshly fetched copy on each call.

Read session local values outside the closure. ctx.Position, ctx.Negated,
and args are not shared state, so nothing about resolving them belongs
inside it. The interface name in cmd_description_if.go, taken from
ctx.Position.Current().Context before the mutation call, is a worked
example.

Reserve MutateProductState for an actual write. A command that only reads
state, such as "show running-config", reads ctx.State.(*State) directly.
command.DaemonClient exposes only mutation methods and no read method at
all.

A command that instead touches the tree structure's runtime data, the
user database, or the role set uses MutateLevels, MutateUsers, or
MutateRoles. The same three rules apply. Those handlers live in package
core.

# Negation

Cisco and HP style CLIs undo most configuration commands with a leading
"no", such as "no shutdown". A tree entry opts in with "negatable: true",
and ctx.Negated is true for the duration of that one handler call.

	if ctx.Negated {
	        // Undo whatever this command normally does.
	        return nil
	}
	// Normal behavior.

# Entering a New Command Level

Some commands change which commands are reachable next. That is a stack
push:

	ctx.Position.Push(command.CommandLevelFrame{
	        Name:         "config-if",
	        PromptSuffix: level.PromptSuffix,
	        Tree:         level.Tree,
	        Context:      ifaceName,
	})

Context carries the subject a nested mode is editing, such as which
interface, so the commands inside that mode know what they are acting on.

# Diagnostic Mode and Interface Mode

The file cmd_diagnostic_mode.go registers a Command Level named
diagnostic. It is a trivial example of a third, non inheriting privilege
tier reached only from exec, holding one command, an equally trivial self
test. It exists to prove the shape works and to give a third tier
something to run. A real deployment's diagnostic mode would do more.

The file cmd_interface.go registers "interface", entering config-if mode
for one named interface at a time. The commands cmd_description_if.go and
cmd_shutdown.go only make sense inside that mode.

Which level a command is reachable from changes nothing about how it is
written. The file cmd_set.go is an ordinary command file that happens to
be reachable directly from exec rather than needing config mode first.
*/
package product
