# RouterCLI

A Go framework for building network equipment-style command line interfaces,
inspired by Cisco IOS and HP ProCurve. This currently supports nested Command
Levels, changing prompts, abbreviation-aware tab completion (`sh run` →
`show running-config`), `no <command>` negation, hidden and password-gated
commands, multi-user authentication, an audit log, multi-factor authentication
(TOTP), and multi-language support.


[![Go Reference](https://pkg.go.dev/badge/github.com/gotermcli/routercli.svg)](https://pkg.go.dev/github.com/gotermcli/routercli) [![CI](https://github.com/gotermcli/routercli/actions/workflows/ci.yml/badge.svg)](https://github.com/gotermcli/routercli/actions/workflows/ci.yml) [![codecov](https://codecov.io/gh/gotermcli/routercli/graph/badge.svg)](https://codecov.io/gh/gotermcli/routercli) [![Release](https://img.shields.io/github/v/release/gotermcli/routercli)](https://github.com/gotermcli/routercli/releases) [![Go Version](https://img.shields.io/github/go-mod/go-version/gotermcli/routercli)](https://github.com/gotermcli/routercli) [![License](https://img.shields.io/github/license/gotermcli/routercli)](https://github.com/gotermcli/routercli/blob/main/LICENSE)


## Quick start

```sh
make build
./bin/routercli --config example/etc/routercli.yaml
```

That runs the bundled example CLI with no setup required.

```
router> show version
RouterCLI 0.1
router> enable
Entering exec mode.
router# configure terminal
router(config)# hostname mytest
Hostname set to mytest.
mytest(config)# interface eth0
mytest(config-if)# description "uplink to core"
Interface description set.
mytest(config-if)# end
mytest# show running-config
! (example running-config)
hostname mytest
interface eth0
 description "uplink to core"
!
mytest# disable
Leaving exec mode, back to base.
mytest> exit
```

Notice the prompt picks up the configured hostname immediately, and that
`configure terminal` requires `enable` first. `config` is a Command Level
whose `parent` is `exec`, so it is unreachable from the base level (see
`var/tree/README.md`).

`go run ./example --config example/etc/routercli.yaml` also works.
`example/main.go` is the only non-test source file in `package main`;
everything else lives in its own package.

## What is part of the framework versus the example

This repository contains both the RouterCLI framework and a working example CLI.
It is important to understand the distinction when building your own CLI:

 - **The framework**:
   - `auditlog/`, `auth/`,
   - `command/`, `config/`, `completer/`
   - `tokenize/`, `i18n/`, `paging/`.
   - These packages know nothing about routers, interfaces, or hostnames. They
     are the reusable machinery that enables things like: Auditing,
     Authentication, Command resolution, Configuration file parsing, Tab
     completion, Interactive `?` help, Command Levels and Tree Structure
     processing, internationalization (i18n), and output paging and
     pipe filtering
 - **The CLI Environment (currently populated with a broadly reusable core set and a working example)**:
   - `example/cmd/core/` (the command handlers broadly useful to almost any project, login and session elevation, terminal paging and filtering, password and TOTP self service, and similar),
   - `example/cmd/product/` (the Cisco and HP flavored demonstration commands, `hostname`, `interface`, and similar, meant to be read, copied, and replaced),
   - `var/tree/*.yaml` (the Tree Structure, Command Level definitions)
   - `var/lang/*.yaml` (the language catalog)
   - Unlike the framework packages, both `example/cmd/core/` and `example/cmd/product/` are openly optional. See "What is `example/cmd/core` versus `example/cmd/product`, and is either one required?" below.

## Layout

```
RouterCLI
│
├── Framework
│   ├── command
│   ├── completer
│   ├── auth
│   ├── auditlog
│   ├── config
│   ├── tokenize
│   ├── i18n
│   └── paging
│
└── example/  (the CLI Environment)
    ├── main.go
    ├── cmd/core/
    ├── cmd/product/
    ├── cmd/session/
    ├── cmd/multiuser/
    ├── var/tree/
    ├── var/lang/
    └── etc/
```

The split is visible in the directory layout. Everything at the repository
root is the library. Everything under `example/` is the worked example, and
every file there can be deleted or replaced.

Building your own CLI means keeping the library packages as they are, keeping
as much or as little of `example/cmd/core` as fits your own project, replacing
`example/cmd/product` and its tree and language entries with your own, and
writing the small entry and exit files your own Tree Structure needs.

### Where things live

The example keeps its configuration in `example/etc/` and its data in
`example/var/`. `example/etc/routercli.yaml` sets `BaseDir: ".."`, which is
the directory every other relative path in that file resolves against.

A relative `BaseDir` resolves against the directory holding the configuration
file rather than the working directory, so these all work:

```sh
./bin/routercli --config example/etc/routercli.yaml
cd example && ../bin/routercli --config etc/routercli.yaml
/anywhere/else$ /path/to/routercli --config /path/to/example/etc/routercli.yaml
```

Leave `BaseDir` unset and every path resolves against the working directory,
which is the behavior a deployment that never sets it already has. An absolute
path is never rewritten.

### What is `example/cmd/core` versus `example/cmd/product`, and is either one required?

Neither package is required by package `command`, and neither package is
required by the other. `example/cmd/core` holds command handlers useful to almost
any project built on RouterCLI: elevating a session to a more privileged
Command Level, entering and leaving configuration mode, terminal paging and
output filtering settings, and password and TOTP self service. `example/cmd/product`
holds the Cisco and HP flavored demonstration commands this repository ships
as its own working example, `hostname`, `interface` configuration, `show
running-config`, and similar, built on top of what `example/cmd/core` provides but in
no way required by it.

A project keeps `example/cmd/core` largely unchanged, drops `example/cmd/product` entirely
and writes its own commands in its place, and still gets everything
`example/cmd/core` provides working correctly, since nothing in `example/cmd/core` depends on
`example/cmd/product`'s application state. A project could equally drop parts of
`example/cmd/core` it does not want, TOTP self service for instance, by deleting the
one file that registers it and removing its entries from the affected
`var/tree/level_*.yaml` files.

This same "encouraged, not required" reasoning covers one further, specific
convention: `example/cmd/core` registers `enable` and `disable`, moving a session
into a Command Level named `exec`, the same two tier, unprivileged then
privileged shape real Cisco and HP devices both use. This is an encouraged
convention this repository's example follows, not a requirement package
`command` or `main.go` imposes. A project is free to rename this Command
Level to something else entirely, add further privilege tiers beyond plain
`exec`, or drop the whole idea of a privileged mode and run every command
directly out of `base`. See `example/cmd/core/doc.go`'s "Encouraged, Not
Required" section for the full reasoning, and `example/cmd/core/cmd_enable.go`'s
doc comment for why nothing beyond that one file's string literals would
need to change to rename it.

## Directory layout

```
Core System

main.go        The entry point: startup, config loading, and the read loop.

auth/          Password hashing (bcrypt), multi-user store, login, 
               TOTP second-factor enrollment and verification, and
               rate limiting (login attempts and, via package
               command, Command Level and per-command passwords)

auditlog/      Timestamped, runtime-toggleable audit trail

command/       The framework core: Command, Resolve(), ValidateArgs(),
               HelpText(), the plugin registry, CommandLevelStack, the
               YAML tree loader, and the Tree Structure / Command Level
               system (CommandLevel, LoadTreeStructure,
               EnterCommandLevel/ExitCommandLevel, VerifyCommandLevels);
               also role based access control (RoleSet, LoadRoles,
               VerifyRoles, Authorized)

completer/     Tab completion and the interactive "?" contextual help
               (readline Listener, not readline's AutoComplete, which
               is too limited for abbreviation-aware, tree-driven completion)

config/        A YAML configuration file parser

tokenize/      Quote-aware line tokenizer and its inverse, used for the
               show-running-config round-trip guarantee

i18n/          Support for internationalization via a language catalog
               (flat YAML per language, with a real fallback chain)

paging/        Output paging and pipe filtering: SplitPipeline and
               ParseStages turn typed tokens into a filter pipeline,
               ApplyFilters runs "| include", "| exclude", and "|
               begin" against a command's captured output, and Display
               is the interactive "--More--" pager itself


End Application

example/cmd/core/      Command handlers broadly useful to almost any project,
               one file per command, each self-registering through
               init(): session elevation, configuration mode entry,
               terminal paging and filtering, and password and TOTP
               self service.

example/cmd/product/   The Cisco and HP flavored demonstration commands this
               repository ships as its own working example, built the
               same way, one file per command self-registering through
               init(). Meant to be read, copied, and replaced.

var/tree/      The Tree Structure manifest (tree_structure.yaml), each
               Command Level's command tree (level_*.yaml), and the
               role declaration manifest (roles.yaml)

var/lang/      The language catalog, one YAML file per language

var/log/       System, history, and audit logs

etc/           The main configuration file (routercli.yaml), the
               multi-user database (users.yaml), and, under
               etc/defaults/, factory default skeleton copies of both
               UsersFile and RolesFile, restored by "erase users" and
               "restore-factory-defaults"
```

## Building your own CLI on top of this

### 1. Add a command

Every command is one file in `example/cmd/core` or `example/cmd/product` that registers a
handler from `init()`. A command with no dependency on your own project's
application state, the way `example/cmd/core/cmd_show.go`'s "show version" has none,
fits naturally in `example/cmd/core`, or your own equivalent package if you have
replaced it. A command that reads or mutates your own project's state, the
way `example/cmd/product/cmd_set.go` does, fits in `example/cmd/product`, or your own
replacement for it. Copy the shape of an existing one:
`example/cmd/core/cmd_show.go`'s "show version" registration for a simple read-only
command with no state dependency, `example/cmd/product/cmd_set.go` for one that
takes an argument and mutates your own project's state, or
`example/cmd/product/cmd_interface.go` if you are building something that enters a
new mode.

```go
// example/cmd/product/cmd_ping.go
package product

import (
   "fmt"

   "github.com/gotermcli/routercli/command"
)

func init() {
   command.Register("ping", func(ctx *command.AppContext, args []string) error {
      fmt.Println(ctx.Translator.T("ping.text"))
      return nil
   })
}
```

Then add it to whichever `var/tree/level_*.yaml` file should offer it:

```yaml
ping:
  desc_key: ping.desc
  run: ping
```

`run:` must match the string passed to `command.Register()` exactly, or the
program refuses to start. A bad handler reference is a hard startup error on
purpose, since a command that silently does nothing because of a typo is a
worse failure mode than refusing to launch. That is the whole mechanism: no
import statements to add anywhere else, and no switch statement to extend.
`main.go` never needs to change.

### 2. Command Level options

Please see `var/tree/README.md` for more information on the Tree Structure,
Command Levels, and the commands within them.

### 3. Supporting `no <command>`

Set `negatable: true` on the command, then check `ctx.Negated` inside the
handler. This is exactly how the bundled example's `interface shutdown`
works:

```go
command.Register("interface.shutdown", func(ctx *command.AppContext, args []string) error {
   iface := ...
   if ctx.Negated {
      iface.Shutdown = false
      return nil
   }
   iface.Shutdown = true
   return nil
})
```

`ctx.Negated` is true for exactly the duration of that one call, so read it
once near the top of the handler. `no <command>` runs the same handler
as `<command>` alone. It is not a second registration. Argument
validation (`minargs` and `maxargs`) is skipped entirely when negated, since
"no X" usually has a different valid shape than "X" itself. For example, `no
description` takes zero arguments to clear a value that `description <text>`
set. The handler decides what is acceptable for its own negated case.

A command must opt in with `negatable: true`. Nothing is negatable by default.
In particular, do not mark a mode-pushing command such as `interface` negatable
without a handler that knows what `ctx.Negated` should mean for it.
`no interface eth0` naturally reads as "delete this interface", not "enter its
config mode negated", and nothing does that deletion semantic for you
automatically.

### 4. Adding a new Command Level

A Command Level is an entry in `var/tree/tree_structure.yaml`, its own
`level_<name>.yaml`, and a hand-written `cmd_*.go` file, in `example/cmd/core` or
`example/cmd/product`, that moves a session into it. See `var/tree/README.md`
for the full manifest schema. There is no separate registration step in
`main.go`. Add the entry to the manifest and write the one small file, and
it is available.

**A nested, stacking mode** (config-if layered on top of config, for
example) calls `command.RequireCurrentCommandLevel` directly and pushes
its own `CommandLevelStack` frame. This is what
`example/cmd/core/cmd_configure.go` and `example/cmd/product/cmd_interface.go` already do:

```go
// entering the new mode
level := ctx.Levels.ByName["config-router"]
if err := command.RequireCurrentCommandLevel(ctx, "config-router", level.Parent); err != nil {
   return err
}
ctx.Position.Push(command.CommandLevelFrame{
   Name:         "config-router",
   PromptSuffix: level.PromptSuffix,
   Tree:         level.Tree,
   Context:      protocolName, // whatever the mode's commands need back
})
```

**A root swap Command Level** (a new Command Level alongside `exec` or
`diagnostic`, where, unlike the nested mode above, only one can ever be
active at a time) calls `command.EnterCommandLevel` and
`command.ExitCommandLevel` instead. See `example/cmd/core/cmd_enable.go` and
`example/cmd/product/cmd_diagnostic_mode.go` for the exact pattern to copy.

Either way, `help`, `exit`, and `end` come from `var/tree/level_common.yaml`
automatically. Do not redefine them in a Command-Level-specific file; doing so
is a hard startup error (see `command.MergeTrees`). `exit` goes up exactly one
level, and only quits the whole program when already at the root. `end` jumps
straight back to the root from anywhere. Both come for free once your level's
tree is loaded through the manifest. You do not implement them per level.

Run `./routercli --check-config` after adding a new level. It loads and verifies
the whole Tree Structure, including confirming that your new `enter_command`
and `exit_command` were registered, and exits without starting the
interactive loop, so you can catch a typo or a forgotten file immediately.

### 5. Adding a language

Copy `var/lang/en.yaml`, translate the values, and save it as
`var/lang/<code>.yaml`. That is it, with no code change needed. Missing
keys in a partial translation fall back to `DefaultLanguage`, and then to
a visible `[[bracketed.key]]` placeholder if missing everywhere,
rather than failing or showing blank text.

Switch language at runtime with `language set <code>`, or set
`CurrentLanguage` and `DefaultLanguage` in `etc/routercli.yaml` for the
startup defaults.

### 6. Turning on authentication

```yaml
# etc/routercli.yaml
AuthRequired: true
UsersFile: etc/users.yaml
```

Generate a password hash for `etc/users.yaml`:

```sh
./routercli --hashpassword
```

A user needs `password` set to log in, but this only matters when
`AuthRequired` is `true`. When it is `false`, login is skipped entirely and
`etc/users.yaml` is never even read.

A session's identity can come from either, or both, of two independent
sources, each with its own on and off switch:

```yaml
# etc/routercli.yaml
EnableHostAuthentication: false   # trust the OS account routercli runs as
EnableCLIAuthentication: true     # routercli's interactive login prompt
```

`EnableCLIAuthentication` is the original behavior shown above and stays on
by default. `EnableHostAuthentication` trusts whichever operating system
account routercli itself is running as, read through the standard library,
with no password prompted for or checked on that path at all. This is meant
for a deployment reached over SSH, where `sshd` already authenticated the
underlying Unix account before routercli ever started, whether routercli is
installed as that account's login shell or reached through a
`ForceCommand`. At least one of the two MUST be true whenever `AuthRequired`
is `true`, or routercli refuses to start.

Both may be true together, which describes a shared Unix account reached
over SSH, where the OS identity alone does not tell routercli which real
person is at the keyboard. In that combination, the CLI login's
username becomes the session's identity used from that point on, while the
OS account routercli was reached as is kept only as a record of how the
connection arrived.

A TOTP second factor, described further below, has its own system-wide
switch, `EnableTOTPAuthentication`, on by default and unable to stand alone,
since a second factor is a step up on top of a primary identity, not a
substitute for one.

Which backend `EnableCLIAuthentication`'s login prompt checks a
typed password against is configurable rather than hardcoded, so a project
can add a new kind of backend, an LDAP or a RADIUS server for instance, by
adding an entry rather than by changing code:

```yaml
# etc/routercli.yaml
AuthProviders:
  - name: local
    type: local
CLIAuthProvider: local
```

`AuthProviders` lists every backend this deployment has configured, and
`CLIAuthProvider` names which entry the login prompt uses. Only
`local`, bcrypt hashes checked against `UsersFile`, has a real
implementation today. See `etc/README.md` for the full field list and
exactly what each setting does.

Setting a password gate to enter a Command Level is also possible. The example
uses the concept of elevating with `enable`, but this would be called whatever
your own manifest calls the command to do this kind of Command Level swap. It
is a separate, decoupled concern from login. It checks the Command Level's
shared secret, `password_hash` in `var/tree/tree_structure.yaml`, settable at
runtime through `password manager` from config mode. That command prompts for
the new secret, masked, rather than taking it as a same-line argument,
specifically so it never ends up in the audit log or readline history in
plaintext.

**Rate limiting** guards login, elevating into a Command Level, and an
individual command's `password_hash`, each independently
configurable:

```yaml
# etc/routercli.yaml
LoginAttemptWindow: 2m
LoginLockoutDuration: 5m
CommandLevelMaxAttempts: 3
CommandLevelAttemptWindow: 2m
CommandLevelLockoutDuration: 5m
```

The windowed lockout behavior shown above is disabled by default for all three,
matching a real device's `enable` having no attempt limit either. Login
itself still keeps this project's original flat cap of `LoginMaxAttempts`
(three by default) even with the window off, since that part was never
optional. See `etc/README.md` for the full field list
(`Login*`, `CommandLevel*`, and `CommandPassword*`, nine fields in total) and
exactly how the window and lockout pair validation works.

**Two-factor login (TOTP / Google Authenticator)** is opt-in per user, and
enrollment is entirely self service, from inside a running session. A user
with no `totp_secret` set yet in `etc/users.yaml` logs in with a password
alone, then runs `user` followed by `totp enable` (or `totp enable qr` for a
scannable QR code alongside the plain text secret), reachable directly after
login with no need to run `enable` first. Either works with Google
Authenticator, Authy, 1Password, or any standard TOTP app. It then asks for
the code the app is showing right now, to confirm the secret
works, before saving `totp_secret` into that same user's entry in
`etc/users.yaml`. A user with no `totp_secret` set is never prompted for a
code at all, so this does not change the login flow for anyone who has not
enrolled.

Authentication in this project lives entirely in `auth/`, with password hashing,
the TOTP implementation, and the login flow all in one place, specifically so a
future second-factor method, most likely a hardware security key, has one clear
seam to plug into: `auth.SecondFactorRequired` and `auth.VerifySecondFactor`
are the only two functions that need to know which methods exist.

### 7. Audit logging

```yaml
# etc/routercli.yaml
AuditLogFile: var/log/audit.log
AuditLogEnabled: false   # starting state only
```

The audit log can also be enabled at runtime, if you so desire. The shipping
example does this with `audit-log enable`, `audit-log disable`, and `audit-log
status`. This is useful for turning it on mid-session without a restart. In the
example these are only reachable once a session has moved past the base Command
Level and live in `var/tree/level_exec.yaml` not the base level's tree.
This means an unelevated session cannot reach them. Every dispatched command is
recorded with a timestamp, the username, or `-` if unauthenticated, and whether
it succeeded.

A `SESSION START` entry and a matching `SESSION END` entry bracket every
session unconditionally, written even when `AuditLogEnabled` is off, so a
session's boundaries are always in the record regardless of the runtime
`audit-log enable` and `audit-log disable` toggle. `SESSION START` also
names the connecting host account when `EnableHostAuthentication` brought
the session in.

This is a different thing from the general system log(`LogLevel` and `LogFile`
in `etc/routercli.yaml`). The audit log is a fixed-format compliance record of
commands run, while the system log is debug-level tracing plus warnings for
diagnosing RouterCLI itself. `LogFile` defaults to stderr. Set it to send that
output to a real file instead, independent of `AuditLogFile`.

### 8. Locking down the shell

```yaml
# etc/routercli.yaml
PreventEscape: true
```

When this is enabled RouterCLI attempts to prevent a shell escape by ignoring
things like Ctrl-C, Ctrl-Z, Ctrl-\\, and `kill <pid>` at the OS level. Ctrl-D
no longer ends the session either; it instead prints a reminder to use `exit`
and keeps reading. The only way out becomes the `exit` command. NOTE: This
cannot block `SIGKILL` or `SIGSTOP`. Nothing can, on any OS, in any language.
That is a kernel guarantee, not a gap in this implementation.

### 9. Output paging and filtering

RouterCLI can pipe a command's output through `| include`, `| exclude`,
and `| begin`, the same three keywords a real Cisco or HP device supports,
and pause long output behind an interactive `--More--` prompt, the same way
those devices do. Both live in the `paging` package, and both apply only to
a command whose own tree entry sets `pageable: true`, see `var/tree/README.md`.
This is opt-in per command on purpose, one command at a time, rather than on
for every command with an exclusion list, since a command whose handler
reads directly from the terminal partway through running, a masked password
prompt for instance, must never have its output captured this way. The
shipped example marks every `show *` command `pageable: true`.

```
router# show running-config | include interface
interface eth0
interface eth1
router# show running-config | exclude !
hostname router1
interface eth0
router# show running-config | begin interface
interface eth0
 description uplink
interface eth1
```

Filters chain left to right, each one narrowing what the previous one
already produced:

```
router# show running-config | include interface | exclude eth1
interface eth0
 description uplink
```

How deep a chain may go is a security setting, `MaxFilterChainDepth` in
`etc/routercli.yaml`, `2` by default. A command line asking for more filters
than that is refused with an error rather than silently truncated or run
anyway. Setting it to `0` disables filtering entirely.

By default a pattern is matched as a plain, literal substring,
`FilterMatchMode: substring` in `etc/routercli.yaml`, predictable for an
operator who never wants to think about regular expression
metacharacters, a period in an IP address for instance. Switching to
`FilterMatchMode: regex` compiles the pattern as a real Go RE2 regular
expression instead, matching exactly what a real Cisco or HP device does.
Either mode can also be switched at runtime, for the current session only,
with `terminal filter-mode <substring|regex>`.

Output longer than one screen pauses behind a `--More--` prompt, honoring
the same keys a real device does: Space shows the next full page, Enter or
Return shows exactly one more line, and `q`, `Q`, or Ctrl-C stops
immediately. `terminal length <0-512>` sets how many lines a session shows
before pausing, for the rest of that session; `terminal length 0` disables
pausing entirely, the real Cisco convention for "never pause." With no
`terminal length` ever typed, the real terminal's detected height is
used instead, read fresh every time a Pageable command runs, so a session
resized mid-run is honored immediately, with no staleness to correct.
`terminal width <0-512>` works the same way for width, though otherwise
has no effect within RouterCLI itself beyond what `show terminal` reports
back, see item 12 below for more on how, and how little, a terminal
resize matters here. `PagingEnabled: false` in `etc/routercli.yaml` turns
pausing off deployment-wide, without affecting filtering at all.

`terminal length` and `terminal width` are session-only values, exactly
matching real Cisco and HP behavior: neither ever appears in
`show running-config` or `show startup-config` on a real device, and
neither does here either. `show terminal` reports a session's current
values instead, the one place they are surfaced back:

```
router# show terminal
Length: 24 lines, Width: 80 columns
Paging: enabled (session), enabled (global)
Filter mode: substring
```

A deployment can still change what a fresh session, one that has never
typed `terminal length`, `terminal width`, or `terminal filter-mode`
itself, falls back to, without editing `etc/routercli.yaml` and
restarting. `configure terminal`, then `line`, enters a Command Level of
its own, `line`, holding exactly three settings: `length <0-512>`,
`width <0-512>`, and `paging`, negatable as `no paging`. Each one takes
effect immediately, the same as `hostname` or any other configuration
command, and, once saved with `write memory`, survives a restart,
replayed back in at boot exactly like every other saved setting:

```
router(config)# line
router(config-line)# length 30
Default terminal length set to 30.
router(config-line)# width 100
Default terminal width set to 100.
router(config-line)# no paging
Paging disabled by default for this deployment.
router(config-line)# end
router# admin
router(admin)# write memory
Running-config saved to startup-config, and every account change saved to disk.
```

This is a different thing from `terminal length` and
`terminal width` above, not a persisted way of writing the same value.
`line length` and `line width` change what a session with no override of
its own sees, exactly the role real Cisco and HP's `line vty` and
`line console` modes play; they never touch a session that has already
typed `terminal length` or `terminal width` for itself. `line` renders
in `show running-config` and `show startup-config` only once at least
one of its three settings has been changed, the same
"nothing configured, nothing shown" convention `hostname` and every
interface already follow. RouterCLI has no listener of its own yet, one
process per connection, invoked however a deployment's wrapper
chooses, so unlike a real device, `line` keeps a single, global set of
defaults today rather than a separate `line vty` and `line console`
split. A real split needs RouterCLI to own a network listener
first, so it knows which kind of connection it is talking to rather
than guessing, left for a future daemon architecture.

### 10. Login and exec banners

```
router(config)# banner motd "Authorized users only"
router(config)# banner login "Enter your credentials below"
```

`banner motd` and `banner login` match Cisco's two commands of the
same names. Both are shown before authentication, `banner motd` first,
matching real Cisco's ordering: a message of the day banner is about
the connection itself, shown to every connection regardless of whether a
login prompt follows at all, while a login banner is shown only
immediately before a real username prompt runs, skipped entirely
when `AuthRequired: false` or `EnableCLIAuthentication: false` in
`etc/routercli.yaml` mean no such prompt ever appears. `no banner motd`
and `no banner login` clear whichever one is named. Both persist to
`startup-config` the same way `hostname` does, see
`example/cmd/product/cmd_banner.go`.

### 11. Command history and `show history`

```
router(config)# terminal history size 200
router# show history
```

`HistoryFile` in `etc/routercli.yaml`, `var/log/history.log` by default,
is a genuine, persistent, cross-session log, not the small, in memory
ring buffer a real Cisco or HP device keeps; every command a session
submits is appended to it immediately, and nothing RouterCLI itself does
ever truncates or clears it. `show history` reads this same file back and
prints its most recent `DefaultHistorySize` lines, `500` by default,
matching the underlying `chzyer/readline` library's built in
default. `terminal history size <0-1000>`, wider than Cisco's
`<0-256>` range on purpose, room enough for `DefaultHistorySize`'s
default, changes how many lines `show history` shows for the rest of
that session.

This does not also change how many past commands the session's Up
and Down arrow recall remembers. That limit is fixed once, from
`DefaultHistorySize`, when a session starts, and never changes
afterward. The underlying readline library reads its own copy of that
limit from a background goroutine that runs for the entire life of a
session, so reassigning it later, once discovered during this project's
own `go test -race` verification, is a genuine data race, not a
theoretical one; see `command.AppContext.HistorySize`'s doc comment
in `command/model.go` for the full reasoning.

### 12. Terminal resize awareness

RouterCLI reacts to a real terminal resize, `SIGWINCH`, the signal a
terminal emulator sends on every resize, logging one `Debugln` entry each
time it happens, `terminal resized, now <width> columns by <height>
lines`. This exists purely for observability, a line a deployment can
grep for, not for correctness: nothing in this project caches a stale
terminal size anywhere to begin with. `paging.EffectivePageLines` and
`paging.EffectiveTerminalWidth` both read the real terminal's current
size fresh on every single call already, so `show terminal`, and every
Pageable command's pager, already reflect a resize the next time
either is consulted, with no `SIGWINCH` handling required for
correctness at all. `watchTerminalResize` in `main.go` is the one call
site, started once at session start and stopped, through its own
returned `stop` function, before the process exits.

### 13. Roles, the admin Command Level, and account management

RouterCLI's role based access control lets a Command or a Command Level
declare which named roles may reach it, `allowed_roles` in the tree YAML,
checked against whichever roles the currently logged in account holds.
This is a separate, independent gate from `password_hash`. A Command or
Command Level MAY set either, both, or neither, and both are enforced
when both are set.

`allowed_roles` is only enforced while `AuthRequired`, in
`etc/routercli.yaml`, is `true`. RouterCLI exists as a library first,
meant to be picked up with nothing configured yet and produce a
working, wide open command line, not one that quietly locks
a project builder out of a level they just declared in their own tree
file. With `AuthRequired` off, RouterCLI's shipped default, no
session anywhere has a real identity to hold a role against in the
first place, so `allowed_roles` stays wide open everywhere, `admin`
included, until a deployment, its own vendor, or its very first
administrator, turns `AuthRequired` on.

```yaml
# var/tree/roles.yaml
roles:
  admin:
    desc: "Full administrative access"
    bypass: true
  operator:
    desc: "Read only access to most commands"
```

Roles are flat and unordered by design, not a numbered hierarchy the way
Cisco's privilege levels are. An account MAY hold more than one role,
set in `etc/users.yaml`:

```yaml
users:
  alice:
    password: "$6$..."
    roles: [operator]
```

A Command or a Command Level gates access to whichever roles it names:

```yaml
# var/tree/tree_structure.yaml
admin:
  allowed_roles: [admin]
```

Access is granted on any overlap at all between the two lists, never on
rank. An account holding none of a gate's named roles is refused, the
same deny by default convention `password_hash` already follows for a
wrong or missing credential. `RolesFile` in `etc/routercli.yaml`, `var/tree/roles.yaml`
by default, is where a deployment declares which role names exist at all;
a Command or a Command Level referencing a name not declared there is a
hard startup error, the same fail loudly convention this project applies
throughout. A deployment that never needs role based access control at
all MAY delete `RolesFile` entirely; a missing `RolesFile` is not an
error, and means no Command or Command Level anywhere may set
`allowed_roles`.

At most one role across the whole manifest MAY set `bypass: true`. An
account holding that one reserved role automatically passes every
`allowed_roles` check anywhere in the tree, regardless of what that
check's list contains. This exists to solve the bootstrap
problem: the very first account a deployment ever seeds needs some way
to reach the level that assigns roles to everyone else in the first
place, before any ordinary role exists to grant it that access.

The shipped example ships exactly one Command Level gated this way, `admin`,
reached from `exec` with `admin`, left with `return`. It carries
`show running-config`, `show startup-config`, and `erase
startup-config`. `admin` also carries this deployment's account
management commands, under the word `account`, chosen to
avoid confusion with the existing self service `user` Command Level:

```
router(admin)# account create bob
router(admin)# account create carol generate
router(admin)# account create dave hash $6$$2a$10$...
router(admin)# account roles add bob operator
router(admin)# account roles remove bob operator
router(admin)# account delete bob
```

`account create <username>` alone prompts interactively, masked, twice,
for the new account's first password, the same flow `password change`
already uses, so a real password never ends up typed on the command line
or written to the audit log. `account create <username> generate`
auto-generates a password meeting this deployment's configured
password policy instead, printed once, the only time it is ever shown in
plain text. `account create <username> hash <hash>` imports an
already-computed hash directly, never a plaintext password, for bulk
resets or preloading identical accounts across many devices. The first
two forms set a new `must_change_password` flag on the account, forcing
whoever logs in with it straight into changing the password before
anything else runs; the third does not, since an imported hash is
presumed to already be the real intended credential. `account delete`,
and `account roles remove` when removing the reserved bypass role,
refuses when the target is the last account left holding it, closing off
a deployment locking itself out of `admin` entirely with no way back.

Every one of the commands above, `account create`/`delete`, `account
roles add`/`roles remove`, `password change`, and `totp enable`/`totp
disable` alike, changes only this session's in memory copy of
`UsersFile`. None of them writes to disk on their own; RouterCLI never
lets a single typed command survive a restart automatically, no matter
how it looks on a real device. `write memory` is the one command that
writes anything to disk at all, saving an account or role change
alongside everything `running-config` already covers:

```
router(admin)# write memory
Running-config saved to startup-config, and every account change saved to disk.
```

Real Cisco and HP ship `write memory` as a synonym alongside a separate
`copy running-config startup-config` that only ever covers the first
half of this. RouterCLI ships `write memory` alone, on purpose,
matching this project's design goal against building two commands
that would otherwise almost, but not quite, do the same thing: one
command, reachable one way, saves everything in memory to disk.

Because an account or role change can lock the very session that made it
out of the device, `reload` and `reboot`, full synonyms for the same
command, accept an optional delay, matching real Cisco and HP ProCurve:

```
router(admin)# reload 300
Reload scheduled in 300 seconds. Use 'no reload' or 'no reboot' to cancel it before it fires.
router(admin)# no reload
Scheduled reload cancelled.
```

The use case this exists for is the same one every real network operator
already knows. Save the current, known good configuration with `write
memory`, schedule a reload a few minutes out with `reload 300`, make the
change, then log out and log back in to confirm it works. If
login now fails, waiting out the delay restores the last saved
configuration automatically and access returns once the reload fires; if
it succeeds, `no reload` cancels the pending reload before that happens.
Once the change is confirmed working, `write memory` saves it as the new
known good configuration. `reload`/`reboot` typed with no delay reloads
immediately instead, rereading `UsersFile`, `RolesFile`, and
`startup-config` fresh from disk, rebuilding the current session's in
memory state from them, and ending the connection, forcing whoever ran it
to reconnect, discarding any unsaved change along the way exactly as a
real restart would.

Recovering from a lockout, however it happened, is what
`restore-factory-defaults` is for. `etc/defaults/` holds skeleton copies
of `UsersFile` and `RolesFile`, restored over the live files by
`restore-factory-defaults`, and, separately, by `erase users`, which
restores `UsersFile` alone rather than deleting it to nothing the way
`erase startup-config` safely can: an empty user database would
permanently lock every account, the bypass role included, out of the
whole deployment. `restore-factory-defaults` additionally erases
`startup-config` and then reloads, the same as running `reload` with no
delay on its own. RouterCLI is one process per connection today, with no
persistent daemon behind it, so `reload` cannot affect any other already
connected session. Multi-session awareness, and the daemon architecture
true multi-session support would need, is future work, not something
this project has built yet.

### 14. Command Aliases

A session, or an administrator setting one up ahead of time, can define a
short name of their own for any command, the same idea real Cisco's
`alias exec` and `alias configure` cover. `alias <alias> <word...>`
defines one, scoped to whichever Command Level the session is standing in
at the moment it is typed, so there is no separate level argument to get
right:

```
router# admin
router(admin)# alias wr write memory
Alias "wr" defined for admin, expands to: write memory
router(admin)# wr
Running-config saved to startup-config, and every account change saved to disk.
```

Aliases are scoped per Command Level on purpose, exactly like real Cisco
keeps `alias exec` and `alias configure` as two separate namespaces: an
alias defined while standing in `exec` has no effect while a session is
sitting in `config`, and the other way around, since RouterCLI never asks
which level an alias belongs to, it already knows, `ctx.Position.Current()`
at the moment `alias` runs. This also means an alias cannot be typed at
all until a session is inside the level it was defined for, the
same as any other command that only exists in one Command Level's
tree. `<alias>` is the short word a session will type from then on; it is
refused outright if it collides with a real, already reachable command in
that same level, so an alias can never silently shadow one. It is also
refused if it collides with an alias already defined at that level;
unlike real Cisco's "define again to change it" convention, RouterCLI
requires `no alias <alias>` first, a deliberate security measure. Anyone
able to type at a session could otherwise change what an already trusted
alias name quietly expands to, and a session that never happens to run
`show aliases` again would have no way to notice. Requiring an explicit
removal first, its own separate confirmation printed, makes that kind of
change something a session has to see happen. `<word...>` is
the real command it expands to, taken literally, with any argument a
session types after the alias itself appended on the end, `wr` above
taking no further argument, but an alias defined with only part of a
command, `alias sh show`, would let `sh interface` run `show interface`.
`no alias <alias>` removes one, again read from whichever level the
session is currently standing in. `show aliases` lists every alias
currently defined, grouped by Command Level.

An alias is expanded once, against only the very first word of a typed
line, before that line is resolved against the command tree at all, so an
alias whose own expansion happens to start with another alias's name
is never chased a second time; a session can never define a cycle that
hangs command dispatch. Interactive Tab completion and `?` help do not
currently expand an alias the way running one does: typing an alias's
short name still runs the real command it stands for, but completing
partway through typing one offers no special help beyond what the
alias's literal name already gets as an ordinary word.

Real Cisco and HP have no literal "help <command>" form of their own;
extra help for one specific command comes from typing the command
itself followed by `?`, `alias ?` for example, and that stays exactly
what `?` and Tab completion do here too for every ordinary command,
unchanged, answering only "what can I type next." RouterCLI's
`help` command accepts an optional command name for a different, more
detailed purpose, a real man page style description of that one
command rather than the next keystroke's hint. Its output opens
with a header line in the classic, mirrored man page shape, the
command's name in all capitals at both the left and right margins
with a centered title, `<ProductName> Help Information`, in between,
`ProductName` a deployment's configured display name, see
`ProductName` in `etc/README.md`, `RouterCLI` itself by default. NAME,
SYNOPSIS, DESCRIPTION, and SUBCOMMANDS sections follow, each only when
that command has something for it to show. `help alias`
prints a NAME section holding alias's name and description, a
SYNOPSIS section built from the same argument hint `?` would show,
and, when that command declares a longer `help` entry of its own in
the language file, a DESCRIPTION section holding that longer body as
well, `alias` and `reload` both ship one, real examples of what a
project built on this framework can add for its own commands. `help
show`, a command with subcommands rather than arguments of its own,
prints a SUBCOMMANDS section instead, one level deep, each entry's
name, description, and usage hint together on one line, useful
anywhere the raw `?` keypress itself is inconvenient to send, a
non-interactive pipe or a copied transcript for instance. A name typed
after `help` that matches more than one command lists the matching
candidate names, with no header line, there being no one command to
build one for; a name matching nothing at all is refused with an
error rather than printing nothing silently. `help` with nothing after
it still lists every command available at the current Command Level,
unchanged.

The NAME, SYNOPSIS, and DESCRIPTION sections wrap their own prose to
the session's terminal width, the same live width `show terminal`
already reports, falling back to a classic eighty column default only
when that width cannot be read, a piped or redirected
session for instance. Every continuation line, not only a section's
own first line, carries that section's left margin, so a long
DESCRIPTION paragraph reads as one properly indented block rather than
its first line indented and the rest left for a real terminal's
dumb wrap to drop back at column zero. The SUBCOMMANDS section is
different, its own column aligned name, description, and usage hint
never rewrapped, so a narrow terminal cannot break its own alignment.
A blank line opens the whole block, before the header, and one more
closes it, after the last section's content, so a detailed `help`
answer never runs straight into the next prompt with no separation of
its own. `help` is itself pageable, the same mechanism every `show`
report style command already uses, so a block longer than one screen
pauses with the classic `--More--` prompt instead of scrolling past
the top of the terminal; see section 9 above for `terminal length` and
`terminal width`, and for how a session's paging preference is
set.

The one place `?` and Tab completion do treat `help` specially is
`help`'s argument. Typing a command name after `help` completes,
and shows `?` help for, that name against the very same tree a bare,
top-level Tab or `?` would, `help con` followed by Tab expanding to
`help configure ` for instance, rather than being left alone as a
plain, free-form argument with nothing to complete, since that
argument always names another real command, never arbitrary text.
This is the one deliberate exception; nothing else changes about how
`?` and Tab completion answer "what can I type next" for every
ordinary command's real argument, and every command shipped in
this project's tree that takes one now documents it with a real
`arghelp`, so `?` and Tab both have something to show rather than
nothing.

An alias applies to the running session right away and shows up in `show
running-config` immediately, the same as any other configuration change,
whatever Command Level it belongs to, `base` and `user` included. It
reaches disk, and so survives a restart, only once `write memory`
saves it, exactly like every other setting RouterCLI ships;
see section 13 above for `write memory` itself and for `reload`'s
save, schedule, verify, cancel or let it fire pattern. No command
anywhere in RouterCLI writes to disk on its own.

## 15. Testing

```sh
test -z "$(gofmt -l .)"
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

Every framework package has real unit test coverage. `command/` in
particular is worth reading if you are extending the resolution or
negation logic, since its tests double as executable documentation of
exactly what is guaranteed, for example
`TestResolveMultipleSubtreeOptionsNeverAutoPicksOne` and
`TestResolveNegatedCommand`.

To check a Tree Structure, either your own or the bundled example's after
editing it, run `./routercli --check-config`. It loads and verifies the Tree
Structure without starting the interactive loop, and is worth running as part
of the same routine.

## 16. Code execution logic

1. `main.go` runs.
2. Every `init()` in `example/cmd/core` and `example/cmd/product` runs. This happens
   automatically, before `main()` even begins, purely because `main.go`
   imports both packages, `example/cmd/core` as a blank import since nothing needs
   to reference it by name, `example/cmd/product` as a named import for its
   `State` type in `example/cmd/product`. Every command, including each Command Level's
   enter and exit command, is registered here.
3. `main.go` reads `etc/routercli.yaml` to find the Tree Structure
   manifest's path, along with the common tree's path.
4. `main.go` calls `command.LoadTreeStructure`, which parses
   `tree_structure.yaml`, resolves each Command Level's parent chain and
   inheritance, loads every `level_<name>.yaml` file, and merges in the
   common tree, validating every `run:` reference against the registry
   as it goes.
5. `main.go` calls `command.VerifyCommandLevels`, confirming every
   declared `enter_command` and `exit_command` corresponds to a
   registered command. This is the same check whether running normally or
   through `--check-config`. A broken manifest fails loudly here, either way.
6. `main.go` calls `command.LoadRoles` and `command.VerifyRoles`,
   confirming every `allowed_roles` reference anywhere in the tree names a
   role declared in `RolesFile`. A missing `RolesFile` is not
   itself an error, see section 13 above; a broken one, or a dangling
   `allowed_roles` reference, fails loudly here the same as a broken Tree
   Structure does.
7. If `--check-config` was passed, `main.go` prints the result and exits
   without starting the interactive loop. Otherwise, it builds the session
   and enters the read, resolve, validate, and dispatch loop.
