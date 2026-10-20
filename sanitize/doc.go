// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package sanitize makes untrusted text safe for the places RouterCLI sends it.

A CLI takes text from people and from other processes, then writes it to a
terminal, to a line oriented log, and to a configuration file that is later
replayed as commands. Each of those has its own structure, and text that
carries the wrong bytes can forge that structure rather than sit inside it. A
newline in an audit entry writes a second entry. A newline in a configuration
value writes a second command. An escape sequence printed to a terminal moves
the cursor, rewrites what is already on screen, or changes the window title.

None of that needs a clever attack. It needs one byte the sink treats as
punctuation.

# Validate at the door, escape at the exit

Two jobs, and they are not interchangeable.

Validation decides whether to accept input at all, and belongs where the input
arrives, so a refusal names the real problem while the caller can still be told
about it. Escaping decides how to render text that was already accepted, and
belongs at each sink, because only the sink knows what its own structure is.

Doing only the first leaves anything already stored dangerous. Doing only the
second means bad input is carried around and rendered differently everywhere it
lands.

# What this package does not do

It does not decide policy. Whether a hostname may contain an emoji is a
deployment's question, not this package's, so the validators here reject only
what is structurally dangerous and leave taste to the caller.

It does not escape for HTML, SQL, or a shell. RouterCLI passes text to none of
them, and a function that claims to be safe everywhere is trusted in places it
was never checked for.
*/
package sanitize
