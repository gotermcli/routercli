// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package command is the reusable framework RouterCLI is built on. A
deployment writes its own commands and its own YAML command trees and
never needs to modify this package to do so.

Nothing here knows about hostnames, interfaces, or any other concrete
command, only about the shape a CLI environment takes. The commands
RouterCLI ships live in package core, a broadly reusable set, and package
product, a working example.

This package provides the command tree data structure, abbreviation
resolution, tab completion support, mode nesting, the Command Level
system, and the registration mechanism a handler uses to make itself
known.

# The Tree Shape

A command tree is a map[string]*Command. The key is the command word,
such as "show" or "hostname". Command describes that word: its
description, whether it is hidden, whether it carries a password gate,
its argument constraints, and its subcommands.

Trees are normally built from YAML rather than written in Go. The file
format is documented in var/tree/README.md. A deployment MAY build a
map[string]*Command directly in Go instead, but YAML lets someone add or
rearrange commands without touching Go source.

# Resolution and Completion

Command execution and tab completion need the same answer to one
question: given what has been typed, which command does it refer to, and
is that answer unambiguous. Resolve is that function.

Resolve implements Cisco and HP style abbreviation matching, so "sh run"
resolves to "show running-config" as long as "sh" and "run" are each
unambiguous at their position. It returns either a single matched Command
with any leftover argument tokens, or the list of candidates when more
than one command still matches.

ValidateArgs is a separate step. Resolve answers which command was meant.
ValidateArgs checks that command's MinArgs, MaxArgs, and MaxArgLength
against what is left over, before the handler runs. A handler never has
to check its own argument count.

# Command Level Navigation

A session moves through a nested sequence of Command Levels: the base
level, then config after "configure terminal", then config interface
after "interface eth0", and so on. CommandLevelStack tracks that
sequence. Push and Pop move in and out of a nested level. Current returns
the active frame, which is what both dispatch and completion resolve
against.

SetRootTree is different. It replaces the root frame's Name,
PromptSuffix, and Tree together, in place, without changing how many
frames are on the stack. This is what moving into a different Command
Level does.

SetRootTree is not a Push, so that "exit" at the root keeps meaning quit
the program at every level, the same way it does on a real Cisco or HP
device. Only a dedicated exit command, such as "disable", steps down a
level without disconnecting.

# Tree Structure

CommandLevel and TreeStructure describe every Command Level a deployment
defines, covering both privilege chains and plain nested modes. Levels
are declared in var/tree/tree_structure.yaml, each with its own enter and
exit command names and its own InheritParent setting.

Every level's enter and exit command is a hand written cmd_*.go file. A
level reached by swapping the root frame calls EnterCommandLevel and
ExitCommandLevel. A nested mode calls RequireCurrentCommandLevel and
pushes its own stack frame. Neither is generated from manifest data.

LoadTreeStructure parses and validates the manifest. VerifyCommandLevels
confirms that every declared enter_command and exit_command corresponds
to a registered command. Those manifest fields are metadata for that
check; this package does not register anything from them.

# Registration

Register makes a command handler known. A handler lives in its own
cmd_<name>.go file and calls Register from that file's init function.
This package owns the registry and the lookup LoadTree performs against
it when a tree file's run directive needs to resolve to a function.

# AppContext

AppContext is the value every command handler receives. It is
constructed once, at startup, and passed through the rest of the program
from there. Nothing constructs a second one.
*/
package command
