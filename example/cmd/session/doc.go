// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package session implements the command handlers that never leave one CLI
connection: "terminal length", "terminal width", "terminal history size",
"terminal filter-mode", "show terminal", and "show history".

Every one of them reads or writes a field that lives directly on
*command.AppContext: PageLines, TerminalWidth, HistorySize, FilterMode,
and HistoryFile. None of them touches ctx.State, ctx.Levels, or
ctx.DaemonClient, so nothing here is shared state a daemon could own.

That boundary is what separates this package from the other two. Package
core holds the reusable, framework level handlers. Package product holds
the vendor flavored demonstration commands. This package holds what
belongs to one connection.

Nothing here requires package core or package product, and neither of
those requires this package. A deployment with no need for session scoped
terminal settings MAY drop this import entirely.

Every file here starts with cmd_ and can be deleted individually. Each
file registers itself from its own init function, so importing this
package is what loads them all.

# Writing a New Command

The same three part shape every cmd package follows: a reference name
matching exactly on the Go side and the YAML side, a cmd_<name>.go file
with an init function calling command.Register, and a "run: <name>" entry
in the appropriate var/tree/level_*.yaml file.

# Application State

Everything registered here operates only on AppContext fields that stay
private to one connection.

HistoryFile is read fresh from disk on every "show history" call rather
than cached, so the listing always reflects what readline has actually
written.

A daemon holding any of these would add a network round trip to something
that is correctly instantaneous and correctly private to one terminal.

# Negation

None of these commands are negatable. Resetting paging is "terminal
length 0", not "no terminal length", following the Cisco meaning of zero
as off.
*/
package session
