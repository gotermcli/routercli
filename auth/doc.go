// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package auth implements password hashing and verification, a multi user
password store loaded from a YAML file, the login prompts, TOTP based
multi factor authentication, and the Session type that tracks one
connection's state.

This package has no dependency on the command package, so a Command
Level can exist and run without auth ever being wired in. It answers two
questions only: is this the right password for this user, and what does
this session currently know about itself.

# Three Independent Layers

Logging in and holding elevated access are separate things. RouterCLI
keeps them as three independent layers:

 1. A login prompt at the start of a session.
 2. A password on a Command Level.
 3. A password on one specific command.

A deployment MAY use any of these, all of them, or none of them. None of
the three requires the others.

Session.Authenticated tracks the first. Session.CommandLevel tracks the
second.

# Password Storage

HashPassword and VerifyPassword are the only functions that touch the
stored form of a password. Both work against the same $id$encoded format
and neither is written against a specific algorithm.

PasswordHasher is the interface a deployment implements to add or swap
the algorithm those two functions use. RouterCLI registers bcrypt as the
default at package init. A deployment needing a different algorithm calls
RegisterPasswordHasher with its own implementation, and
SetDefaultPasswordHasher if new hashes should use it.

Passwords already hashed under another algorithm keep verifying, because
VerifyPassword dispatches on the id embedded in the stored value.

Plaintext passwords are never stored, logged, or held longer than the one
call that needs them. PromptSecret reads a masked password into a string
that goes straight to VerifyPassword and then out of scope.

# Users

A User entry in etc/users.yaml holds a username, a password hash, and
optionally a TOTP secret. LoadUsers parses and validates the whole file
at startup. A user with no password hash is a hard error, not a silently
unusable account.

SaveUsers writes the database back in the same shape LoadUsers reads, so
a running session can persist a change without an administrator hand
editing the file and restarting.

# Logging In

PromptLogin is the whole interactive flow. It reads a username, reads a
masked password, verifies it, and, when the matched user has a second
factor configured, follows with VerifySecondFactor before the session
counts as authenticated.

It retries up to maxAttempts times and calls auditFail after each wrong
attempt so the caller can log it. On success it returns a new Session.

# Sessions

Session holds just enough state to answer who this is and what they can
do right now, without re deriving it on every command.

This package sets Username and Authenticated. CommandLevel and
CommandLevelEnteredAt are set by whichever cmd_*.go file calls
EnterCommandLevel or ExitCommandLevel, which is why NewSession leaves
CommandLevel at its zero value.

# Multi Factor Authentication

TOTP is implemented here directly, per RFC 6238, with no external
dependency. The algorithm is small and well specified, and for something
this security sensitive it is worth being readable end to end in one
file.

GenerateTOTPSecret creates a secret for a user being enrolled.
TOTPProvisioningURI turns it into the otpauth:// URI an authenticator app
scans as a QR code, and FormatTOTPSecretForDisplay groups the same secret
for manual entry. VerifyTOTPCode checks a submitted code with a small
clock skew tolerance, since no two clocks agree indefinitely.

Enrollment is self service from inside a running session. The totp enable
and totp disable commands in package core drive these functions. A user
with no secret logs in with a password alone, then runs totp enable to
add a second factor to their own account.
*/
package auth
