# CLI Environment

A Command Line Interface (CLI) environment contains a series of commands at
various Command Levels (hereafter just called "level") forming a tree like
structure. This collection of levels, and how one navigates to them, is called a
Tree Structure. Levels can exist off of the base level, or can be nested deeply.
In addition, the prompt can change and there can be additional authentication
requirements to reach a given level. 

In the simplest form of a CLI environment all commands will be found at the base
level, creating a flat tree structure. However, most CLI environments will have
additional levels forming a more complicated tree structure. For example,
network switches can have a `base` level, a `exec` or `enable` level, and inside
that a `configure terminal` level, and nested inside that a `interface eth#`
level and maybe even a `vlan #` level. 

Any unknown property names found in any of the YAML files that define the CLI
environment will result in a startup error, instead of silently being ignored.


## Tree Structure and level Properties

All properties are optional except for the `tree_file`. 

### `tree_file`

This property is the path to the YAML file that holds the commands for this
Command Level. It is the one required property for each entry, including the
base level.

### `is_base`

This property defines the entry point for a session. There MUST be only one
defined `is_base` in the entire `tree_structure.yaml` file. RouterCLI will
refuse to start if this is not set at least once, or if it was set more than
once.

### `parent`

This property defines the level a session must currently be in in order to reach
this level. It is set on every entry except for the base level since the base
level is where every session starts and there can be only one base level in the
Tree Structure. Every level, without exception, is reached through a `cmd_*.go`
file that enforces this parent requirement. The property MUST NOT be set for the
base level.

### `inherit_parent`

When `true`, this level's effective command set is its own commands plus
every command inherited from its parent, recursively up the entire Tree
Structure. The default is `false`. When set to `false` this level
contains only the commands listed in its own YAML file.

There are some commands that cannot be inherited, these are: any command that is
itself some other level's own `enter_command` or `exit_command`.

### `enter_command`

This property names the command that moves a session from the parent into this
level. It is REQUIRED for every non-base level. A level missing this
property, or naming a command that was never registered will result in a startup
error, rather than silently producing a level no session can ever reach.

### `hidden`

This property marks the level as hidden, meaning it cannot be discovered
through tab completion or a help menu.

### `exit_command`

This property names the command that moves a session back out to the parent
without ending the session. While it is optional, it is best practice to define
one. Any level with no `exit_command` can still be left with the generic `exit`
command.

### `password_hash`

This property is an optional hard coded secret of a bcrypt hash. When set this
is required when the `enter_command` is used. If this is not set or left empty
the `enter_command` work immediately, with no password prompt. This is an
ordinary, end user changeable secret, changed at any time with `password
manager` while a session is inside this level. `show running-config` and `show
startup-config` will print this hash as the literal `<HIDDEN>` placeholder
rather than its real value, unless the session issuing that command is itself
sitting at a level that has `show_secrets` enabled, see that property below. See
`vendor_defined_password_hash` below for a secret that is deliberately not end
user changeable, and never printed by either command at all, in any form.

**NOTE:** A `<HIDDEN>` placeholder ensures that only authorized individuals can
see the hashed password. If every user can see the hashed password then it may
be possible for someone to offline crack the password given enough time and
compute. 

### `password_user_settable`

This property controls whether `password_hash` for a given level is allowed to
be changed (e.g., `password manager`). The default is `true`. If set to `false`
then the `password_hash` cannot be changed within the CLI environment.

### `vendor_defined_password_hash`

This property is a hard coded secret that gates the `enter_command` exactly the
same way a `password_hash` does, however, it is not meant to be seen or changed by
an ordinary end user inside the CLI environment. The primary use case for this
feature is to create a gated menu for support staff or a sales engineer. This
MUST NOT be set if `password_hash` is also set for that level. Setting
this property for a given level also requires that the `hidden` property
and the `vendor_restricted` property both MUST be set to `true`, and the
`password_user_settable` property be set to `false`, for that same Command
Level.

**NOTE:** The reason you should make this level hidden is so that an end user
does not see something they cannot navigate to. `vendor_restricted`, described
below, is what ensures this is enforced. A hidden, password gated vendor tree
should never be visible to any end user, even the deployment's own initial
administrator, since that visibility is itself enough to eventually gain entry
to a menu the deployment was never meant to reach.

### `vendor_restricted`

This property, a boolean defaulting to `false`, marks a level as one whose
entire configuration state must never appear in `show running-config` or `show
startup-config`, in any form, under any circumstance. It is critical to note
that not even the `show_secrets` setting below would grant access. Setting this
property requires that `hidden` also be set to `true`.

**NOTE:** A vendor defined level and its state is captured in a file defined in
`VendorConfigFile` in `etc/routercli.yaml` alongside `StartupConfigFile`. `write
memory` saves a `vendor_restricted` level's own real, unredacted state there,
never into the `StartupConfigFile`, and neither `erase startup-config` nor
`restore-factory-defaults` ever removes it; both only ever touch
`StartupConfigFile`, `UsersFile`, and `RolesFile`. This is deliberate: a
`vendor_restricted` level's own state is meant to be managed by whoever built
the product based on this library, not reset by an ordinary end user putting the
deployment back into a factory reset state.

### `grant_auth_bypass`

This property grants a level with special access rights to use its own
freshly authenticated sessions to bypass other password prompts found on other
levels or commands for a short period of time, as defined in
`AuthBypassTrustWindow`. The use case for this feature is to enable a
configuration to be pasted into the CLI, even if various levels or
commands require additional authentication, without being prompted for each one.
The default is `false` and SHOULD only every be used in one level.

**NOTE:** Nothing about this property makes any pasted or replayed text itself
count as proof of a credential. Trust only ever comes from a real, live password
for the level carrying this property. Entering some other level under this
bypass mechanism never marks that other level's own `LastAuthenticatedAt`, so
the trust never chains any further than the one level that actually earned it. A
level shipped with neither `password_hash` nor `vendor_defined_password_hash`
set never has anything to check on entry, so it never sets `LastAuthenticatedAt`
either, and `grant_auth_bypass` quietly grants nothing at all until a real
password is configured for it.

### `show_secrets`

This property defines a level as one where `show running-config` and `show
startup-config` will bring the actual end user settable `password_hash` in its
hashed form, rather than the `<HIDDEN>` placeholder either command renders
everywhere else. In the example Tree Structure included with this library
`admin` is the one level that sets this. This property is read generically by
whatever renders configuration output, never hard coded against a level name, so
a product renaming or restructuring its own version of this level never needs to
touch that rendering code, only this one property on whichever level should
carry it. This property has nothing to do with `vendor_defined_password_hash`; a
level carrying one of those is always `vendor_restricted`, see that property
above, and stays out of both commands entirely regardless of `show_secrets`.

### `allowed_roles`

This property lists the roles that are allowed to access this level. This is a
separate and independent gate from `password_hash`. A level MAY set either,
both, or neither, and both are enforced when both are set. This is only enforced
while `AuthRequired` is enabled.

### `prompt_suffix`

This property contains the text appended to the prompt while this level is
currently active, for example `"(config)"`. An empty string is fine, and the
base level normally has none.

### `skip_common`

This property defines whether or not the give level inherits the standard
`help`, `exit`, and `end` commands merged in from `level_common.yaml`. The
default is `false`.


## Command Properties

This section defines the properties that are applicable to commands at a given
level. It is important to note that the command definition found in the yaml
file does not define what it actually does, that is located in the actual Go
code. The command name found in the `run` property must exactly match the name
found in the `command.Register()` function that is part of the `init ()`
function found in a `cmd_<something>.go` file in `cmd/core`, `cmd/session`, or
`cmd/product`. This is how the information in the YAML file and the Go code are
connected. If the names do not match, then the system refuses to start.

### `desc`

This property contains the one line description shown for this command in a
command listing. It is only used when `DescKey` is empty.

### `help`

This property contains a longer man page style body of text describing what this
command is and does. This is shown as part of the `help <command>`'s own
detailed output, which contains this command's `desc`, its `arghelp`, and, for a
command with subcommands, a listing of them. It is only used when `HelpKey` is
empty. If this is empty then a command will get a basic output build from the
other properties.

### `arghelp`

This property is a one line hint describing the argument(s) a command expects,
for example `"<2-1000> Enter a number for the 'length' command/parameter."`,
shown by the completer when Tab is pressed with nothing yet typed for that
argument. It only means something on a leaf command, one with no subcommands,
that also sets a `minargs` value, since a command with no required argument has
nothing to hint about.

### `desc_key`

This property is a key into the language catalog (`var/lang/`) that may be used
in place of a literal `desc`, when translation is needed or desired. If both
`desc` and `desc_key` are set for a given command, `desc_key` wins. It is best
practice to use the keyed version over the static version.

### `help_key`

This property works the same way as `desc_key`, but for `help` instead of
`desc`.

### `arghelp_key`

This property works the same way as `desc_key`, but for `arghelp` instead of
`desc`.

### `run`

This property is the name of the handler to call when this command runs. It must
exactly match the string passed to `command.Register()` in some
`cmd_<something>.go` file's `init()` function or RouterCLI will refuese to
start.

### `alias`

This property creates an alias for an existing command. When set, this command
becomes a pointer to another command name at the same level. Resolve() finds
the alias's name automatically and that is executed instead.

**NOTE:** This is a build time concept, declared once in a tree YAML file by whoever
builds a product on top of RouterCLI, For example, `?` pointing at `help` in
`level_common.yaml`. It is a different thing entirely from the runtime defined
command alias a session, or an administrator, can create and remove while
RouterCLI is running, `alias <alias> <word...>`, typed from whatever level the
alias should belong to. A tree file's own `alias` property is never involved when the
runtime `alias` command expands something a session actually typed. Unlike this
build time property, the runtime `alias` command refuses to redefine a name that
is already a defined runtime alias; `no alias <alias>` must remove it first.

### `hidden`

This property enables commands to be hidden. When `true` the command is hidden
from tab completion, command listings, and the dynamically built help output.
However, it is still reachable if the full command name is typed in.

### `negatable`

This property allows the command to be undone by prefixing it with a `no`
operator, running the same handler with a negated flag set rather than being a
separate registration.

### `password_hash`

This property contains an optional bcrypt hash gating this specific command,
independent of level. This is distinct from a level's own
`password_hash`, which gates entering a whole level rather than one command
inside it. For example, `show tech` or toggling the audit log can each carry
its own `password_hash`. This is an ordinary, end user changeable secret.
See `vendor_defined_password_hash` below for a secret that is deliberately
not end user changeable.

### `vendor_defined_password_hash`

This property is a hard coded secret baked in by whoever built this
product, gating this command exactly the way `password_hash` above does,
but never meant to be seen or changed by an ordinary end user, only known
out of band by support staff or a sales engineer. Do not set this alongside
`password_hash` on the same command, exactly one of the two is ever
meaningful at once. Setting this property requires `hidden` above to also
be `true` on this same command, and forbids `password_user_settable` from
being `true`, both enforced by `command.VerifyVendorDefinedSecrets`, which
runs at every startup and standalone through `--check-config`.

**NOTE:** `vendor_restricted`, see the level section above, exists
only on a level today, not on an individual command; nothing in
this project's own shipped tree currently renders a single command's own
`password_hash` or `vendor_defined_password_hash` inside `show
running-config` or `show startup-config` at all, so there is nothing yet
for a level `vendor_restricted` to protect against here. `hidden`
above remains the one property this rule actually needs.

### `password_user_settable`

This property controls whether an end user is allowed to set or change this
command's own secret at all. It defaults to `true` when left out of a tree
file entirely, matching this project's original behavior, so no existing
tree file needs to change to keep working exactly as it always has. A
command carrying `vendor_defined_password_hash` MUST NOT set this to
`true`, see that property's own entry above for the validation rule and
why.

### `allowed_roles`

This property, an optional list of role names, gates running this specific
command to whichever role in `RolesFile`, see this file's own roles
section below, holds one of the names listed here. Empty by default,
meaning this property gates nothing at all. This is the Command
counterpart to a level's own `allowed_roles` above, checked in
`main.go`'s own dispatch loop at the same point `password_hash` above is
already checked, and independent of it the same way: a command MAY set
either, both, or neither, and both are enforced when both are set. The
same `AuthRequired` dependency applies here too, see the Command
Level's own `allowed_roles` entry above for the full reasoning.

### `minargs`

This property sets the minimum number of arguments this command requires,
enforced before `run` is ever called. `nil` means no minimum, the common case
for most commands. This is not enforced when the command was reached
through `no`.

### `maxargs`

This property sets the maximum number of arguments this command accepts,
enforced the same way as `minargs`. `nil` means no maximum. This is a \*int
rather than a plain int on purpose, since a plain int would set a `nil` / zero
value to 0, which would silently mean "this command accepts zero arguments" for
every node that does not set it. This makes `nil` unambiguous.

### `maxarglength`

This property sets the maximum length, in runes, that is allowed for any single
argument, enforced the same way as `minargs` and `maxargs`.

### `requires`

This property names a feature flag that must be true for this command, and
its subcommands, to exist in the tree at all. It is checked once at startup
by `PruneDisabledCommands`, called from `main.go` right after the tree
structure is loaded. The default is empty, meaning the command is always
available. This differs from `password_hash` above in kind, not just in
name. `password_hash` gates whether a reachable command's own action is
allowed to run, while `requires` gates whether the command is reachable, or
even shown in help or tab completion, at all. That distinction matters for a
command whose whole reason to exist depends on a feature being turned on,
`password change` when `EnableCLIAuthentication` is `false` in
`etc/routercli.yaml` for instance, where the right behavior is for the
command not to exist rather than to exist and refuse.

The flag names checked today are `totp`, tied to `EnableTOTPAuthentication`,
and `password_change`, tied to `EnableCLIAuthentication`. See
`var/tree/level_user.yaml` for both in real use, and `etc/README.md` for
what each of those `routercli.yaml` settings does.

### `pageable`

This property, a boolean defaulting to `false`, marks a command as safe to
run through output paging and pipe filtering, `| include`, `| exclude`,
`| begin`, and the interactive `--More--` pager for output longer than one
screen. It is opt in on purpose, one command at a time, rather than on for
every command with an exclusion list. A command whose own handler reads
directly from the terminal partway through running, a masked password
prompt or a TOTP code for instance, must never be marked `pageable`, since a
pageable command's entire output is captured into memory before anything
reaches the real terminal, see the `paging` package's own `CaptureOutput`
function, and an interactive prompt captured that way would never actually
reach the person who needs to answer it. Every command in
`var/tree/level_base.yaml` that only prints and returns, `show version`,
`show interface`, `show running-config`, `show startup-config`, and `show
terminal` among them, sets `pageable: true`. `help`, in `var/tree/level_common.yaml`,
sets it as well, `help <command>` printing its whole man page style answer
up front with nothing further read from the terminal, the same safe shape
every `show` command already has. A command line that types a
pipe filter against a command left at the default `false` is refused with
an error rather than silently ignoring the filter and running the command
anyway. See `README.md`'s own section on output paging and filtering for
the full picture, and `etc/README.md` for the related `PagingEnabled`,
`DefaultPageLines`, `FilterMatchMode`, and `MaxFilterChainDepth` settings.

### `subcommands`

This property lists the nested subcommands reachable from this command. Each
command may have sub commands, for that command (e.g., `show version`, where
`show` is the command and `version` is the sub command.) It is common, though
not required, that when a command has one or more sub commands, that one of the
sub commands must be provided for the command to fully execute. 



## Roles

`RolesFile`, `var/tree/roles.yaml` by default, is where a deployment
declares every role name it recognizes, for use in a Command or Command
Level's own `allowed_roles` list elsewhere in `var/tree/`. Declaring a
role here, and referencing it in `allowed_roles`, only actually
restricts anything once `AuthRequired`, in `etc/routercli.yaml`, is
`true`. See the `allowed_roles` entries above for the full reasoning.

```yaml
# var/tree/roles.yaml
roles:
  admin:
    desc: "Full administrative access"
    bypass: true
  operator:
    desc: "Read only access to most commands"
```

### `desc`

A short, human readable description of what this role is for. Purely
documentation; nothing in package `command` itself reads it.

### `bypass`

A boolean, `false` by default. At most one role across the whole manifest
MAY set this `true`; a manifest setting it on more than one role is a hard
startup error. An account holding this one reserved role automatically
passes every `allowed_roles` check, on any Command or level,
regardless of what that check's own list actually contains. This is what
lets a deployment's very first seeded account, see `etc/README.md`'s own
factory defaults section, reach the `admin` level and start
assigning ordinary roles to everyone else, before any ordinary role exists
to grant it that access in the first place.

Roles are flat and unordered by design, not a numbered hierarchy the way
Cisco's own privilege levels are. An account MAY hold more than one role,
set through `account roles add` and `account roles remove` in the `admin`
level, never by hand editing `UsersFile` directly. Access through
`allowed_roles` is granted on any overlap at all between the roles an
account holds and the roles a Command or level names, never on
rank; there is no concept of one role outranking another.

A deployment that never needs role based access control at all MAY delete
`RolesFile` entirely; a missing `RolesFile` is not an error, and simply
means no Command or level anywhere may set `allowed_roles`, since
there would be nothing left to validate a name against. A Command or
level referencing a role name not actually declared in `RolesFile`
is a hard startup error, `command.VerifyRoles`, the same fail loudly
convention `command.VerifyCommandLevels` and
`command.VerifyVendorDefinedSecrets` already follow for a broken manifest.

See `README.md`'s own section on roles, the `admin` level, and
account management for the full account command reference, and
`etc/README.md`'s own factory defaults section for how a deployment
recovers if every account holding the bypass role is ever lost.


## Purpose

The purpose of this framework is to allow organizations to organize commands and
levels into whatever tree structure they need to support their desired CLI
environment.
