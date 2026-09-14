# RouterCLI Configuration Files

This directory holds the RouterCLI configuration file (`routercli.yaml`) and the multi-user login database (`users.yaml`).


## Properties for the routercli.yaml YAML file

This is the main configuration file for RouterCLI, loaded from the path given via `--config`/`-c` (default `etc/routercli.yaml`). A missing file is not an error as the values found in `DefaultSystemConfig()` are used instead. Unknown keys are an error, so a typo in this file fails loudly at startup rather than being silently ignored.

All fields are optional; every field not set falls back to the default shown below.

### Product Identity

#### `ProductName`

This property defines this deployment's display name. The default is `RouterCLI`. It is used to build the centered title of `help <command>`'s man page style header line, `<ProductName> Help Information`, so a project built on top of RouterCLI can show its own name there rather than the framework's. This is purely cosmetic, and separate from `TOTPIssuer` below, which has its own, narrower purpose and its own default.

### Security Settings

#### `PreventEscape`

This property attempts to prevent a user from being able to escape the CLI. The default is `false`. When set to `true` RouterCLI will ignore **SIGINT**, **SIGTSTP**, **SIGQUIT**, and **SIGTERM** at the OS level, so `Ctrl-C`, `Ctrl-Z` (suspend), `Ctrl-\`, and a plain `kill <pid>` cannot stop or background the process. It also changes the read loop so `Ctrl-C` does nothing observable and `Ctrl-D` no longer ends the session; it instead prints "Use 'exit' to leave." and keeps reading. The only way out becomes the `exit` command. NOTE: **SIGKILL** and **SIGSTOP** cannot be blocked or ignored by any process, on any OS, in any language. That is a kernel guarantee, not a gap in this implementation.

### Logging Support

#### `LogLevel`

This property defines the following logging levels, of `0`, `1`, `3`, or `5`. The default is `0`. A value of `1` enables `error` and `info`, a value of `3` also enables `warn`, and a value of `5` also enables `debug`. The `ROUTERCLI_DEBUG` environment variable enables `debug` logging for a one time run regardless of this setting.

#### `LogFile`

This is the path and filename for the main RouterCLI system log file. The default is **STDERR**. Set this property to send system log messages to a file instead of the terminal. In a production system this matters, especially once `PreventEscape` and `AuthRequired` are in use, as it is unlikely that an operator will be monitoring **STDERR**. This file is opened in append only mode. If for some reason it cannot be opened, due to a bad path or permissions issue, RouterCLI will fall back to **STDERR** and will print a warning to **STDERR** explaining why, rather than failing to start.

#### `HistoryFile`

This is the path and filename to the readline history file. The default is `var/log/history.log`. Unlike a real Cisco or HP device's small, in memory command history, this file is a genuine, persistent, cross-session log; every command a session submits is appended to it immediately, and it is never truncated or cleared by anything RouterCLI itself does. `show history` reads this same file back, see `DefaultHistorySize` below for how many of its most recent lines that command shows.

#### `DefaultHistorySize`

This property fixes how many past commands a session's Up and Down arrow recall remembers, for the whole life of that session, and sets how many lines `show history` shows until a session types `terminal history size <n>` itself. The default is `500`, matching the underlying readline library's built in default. Only the `show history` behavior can change after a session starts; the Up and Down arrow recall limit stays fixed at this value for that session regardless of anything typed afterward.

#### `AuditLogFile`

This is the path and filename to the audit log file. The default is `var/log/audit.log`.

#### `AuditLogEnabled`

This property enables the audit log at startup. The default is `false`. This only controls the starting state as audit logging can be toggled at runtime regardless of this setting, but depending on your implementation, with something like `audit-log enable` and `audit-log disable`.

### Internationalization Support

#### `CurrentLanguage`

This property defines the current and active language that is being used by the CLI. The default is `en`. The value of this property is this language code (e.g., `en` for English and `fr` for French). It is critical that these code match the language catalog's filename in the `LanguageDir`. If the requested code has no matching catalog it falls back to the `DefaultLanguage`.

#### `DefaultLanguage`

This property defines the default or fallback language that should be used when the current language's catalog is missing a string. The default is `en`. This is a separate setting from `CurrentLanguage`, since an operator running the CLI in `fr` with an incomplete French catalog should be able to fall back to specific and defined language (e.g., `en`).

#### `LanguageDir`

The location of the language catalog (`*.yaml`) files. The default is `var/lang`. Empty or missing is fine, since the language catalog is entirely optional, and any translated text falls back to its raw key, shown bracketed as `[[show.desc]]`, if no catalogs are loaded.

### Timeouts

#### `SessionIdleTimeout`

This property defines how long the read loop needs to wait for a line of input before giving up and ending the current session entirely (e.g. `10m` or `30s`), using the Go duration syntax. Zero disables it. The default is `0` (disabled).

#### `ElevationTimeout`

This property defines how long a session stays at a non-base Command Level before automatically reverting to the base level (e.g., `5m`), using the Go duration syntax. Checked once per read loop iteration, so a session sitting idle past this timeout is demoted the next time any line is entered, not through a background timer, since there is nothing useful to do before the user interacts with the CLI again anyway. The default is `0` (disabled).

#### `ReauthGracePeriod`

This property defines how long a Command Level's password check is skipped after it was last answered, for that same level, in this run of the program, using the Go duration syntax. A session that leaves a password gated level and comes right back within this window is let back in without being prompted a second time, the same way `sudo` on a Linux system remembers a recent authentication for a short while rather than asking every single time. The default is `0` (disabled), every entry always prompts.

This is a different property from `ElevationTimeout` above, not a longer or shorter version of the same idea. `ElevationTimeout` demotes a session that has stayed elevated too long with nothing typed. `ReauthGracePeriod` is only about a session that already left a level and is trying to get back in; it has no effect at all on a session that is still inside a level right now. A short value, on the order of one minute, is recommended: long enough to cover someone stepping out of a level by accident and straight back in, short enough that a terminal left unattended shortly after stepping down is not still trusted for long. This property must never be relied on to let a whole saved configuration paste back in without prompting for every gated level along the way; that is a different, separate mechanism, `AuthBypassTrustWindow` below, meant specifically for that, since a recent authentication into one level must never, by itself, grant entry into a different one it was never checked against.

#### `AuthBypassTrustWindow`

This property defines how long a real, live password check succeeding at a Command Level marked `grant_auth_bypass` in its own tree file, see `var/tree/README.md`, is trusted broadly enough to also waive every other Command Level's password check, using the Go duration syntax. This is what lets a whole saved configuration, `show running-config`'s output for instance, paste back in and reproduce the exact same access it had before, without stopping at a fresh prompt every time the pasted text moves into another gated level, while still requiring one, and only one, real, live, freshly typed credential to unlock any of it. Nothing inside pasted or replayed text can ever satisfy this on its own; see `admin`, this project's Command Level built for exactly this, in `var/tree/README.md`, for the mechanism this property controls. The property keeps this name, rather than being renamed alongside the level it was originally built for, since it is generic infrastructure any `grant_auth_bypass` level can use, not something specific to `admin` by name.

Unlike every other timeout on this page, the default here is not `0` (disabled); it is `5m`. `AuthBypassTrustWindow` at `0` would leave `admin` unable to do the one job it exists for, so this ships enabled, long enough to type or paste even a sizeable configuration by hand, short enough that it is never mistaken for a standing, general purpose bypass. A deployment that wants `admin` to only ever be a place to view and manage configuration, never a shortcut past any other level's password, sets this to `0`.

#### `RolesFile`

This property is the path to the role declaration manifest, see `var/tree/README.md`'s roles section, used by any Command or Command Level that sets `allowed_roles` in its own tree file. The default is `var/tree/roles.yaml`. A missing file is not an error; it means this deployment never uses `allowed_roles` anywhere, and RouterCLI refuses to start if any tree file references a role name that is not declared here.

#### `DefaultsDir`

This property is the directory holding this deployment's factory default files, at minimum a skeleton `UsersFile` seeded with one bootstrap account holding the reserved bypass role, see `var/tree/README.md`'s roles section. `erase users` and `restore-factory-defaults`, both in the `admin` Command Level, copy from here, matched to the live file's base name, rather than deleting a file to nothing the way `erase startup-config` always has. The default is `etc/defaults`.

### Command Tree Settings

#### `TreeStructure`

This is the path and filename to the tree structure manifest file. The default is `var/tree/tree_structure.yaml`. See `var/tree/README.md` for its full schema.

#### `CommonTreeFile`

This is the path and filename to the file defining the commands common to every Command Level (i.e., `help`, `exit`, `end`). These will be merged into every level at load time unless that level sets `skip_common`. The default is `var/tree/level_common.yaml`.

#### `StartupConfigFile`

This is the path and filename `admin`'s `write memory` and `erase startup-config` commands read and write, see `var/tree/README.md`'s `admin` section. `show startup-config` reads the same file back out. No other command ever writes to this file; RouterCLI never lets a typed change survive a restart without `write memory` being run first. The default is `var/startup-config/startup-config`, its own dedicated directory under `var/`, matching how `var/lang/` and `var/tree/` each get their own directory rather than sharing one general purpose `var/` catch-all. Both the filename and its location are fully controlled by this one setting; point it anywhere a deployment prefers. This file does not exist until something writes it, and RouterCLI treats a missing file the same as an empty one rather than an error.

RouterCLI also replays this file back in automatically, once, every time the process starts, before a session can even log in, the same way a real device applies its own saved configuration at boot before anyone can reach a prompt. This runs with no password prompting of its own, even for a Command Level that normally requires one: the trust here is not a credential typed at a terminal, since nobody has had the chance to type one yet, it is this process itself already having been allowed to run, and to read this file, by the operating system. Nothing recorded inside the file's text, a `password manager hash <hash>` line included, is ever treated as proof of anything on its own; see `command.AppContext.ReplayingStartupConfig`'s doc comment in `command/model.go`, and `loadStartupConfig` in `main.go`, for the full mechanism. A malformed or incompatible saved file, one naming a command that no longer exists for instance, fails the whole process at startup with a clear error rather than starting up in a silently, partially applied state.

#### `VendorConfigFile`

This is the path and filename `write memory` writes a `vendor_restricted` Command Level's real, unredacted state to, see `var/tree/README.md`'s `vendor_restricted` section. This is a separate file from `StartupConfigFile` for one reason: a `vendor_restricted` level's state must never appear in `show running-config` or `show startup-config` at all, and `StartupConfigFile` is exactly the file both of those commands read from or reproduce. The default is `var/startup-config/vendor-config`, alongside `StartupConfigFile`'s default file. `write memory` never creates this file at all for a deployment that never marks any Command Level `vendor_restricted`; there is no reason to leave a stray, empty file on disk for a project that does not use this feature. Neither `erase startup-config` nor `restore-factory-defaults` ever removes this file; a `vendor_restricted` level's state is managed by whoever built this product, not reset by an ordinary end user putting the deployment back into a factory reset state.

#### `AlphabeticalCommandOrder`

This property controls how `help`, `?`, and Tab completion order a listing of more than one command name. The default is `true`. When this is set to `true` every listing is sorted alphabetically by name. When this is set to `false` the listing follows the order commands are defined in their tree file. If this property is `false` then `MergeCommonCommands` MUST also be `false`.

#### `MergeCommonCommands`

This property controls where the common commands (e.g., `help`, `?`, `exit`, `end`) are merged into the respective commands at the Command Level. The default is `true`. When this is set to `true` the common commands are merged in and sorted with all the other commands at that Command Level. When this is set to `false` all the common commands are appended after everything else.

NOTE: Here are the four (4) use cases for the sorting of Commands in a Command Level.

 - `AlphabeticalCommandOrder` is `true` and `MergeCommonCommands` is `true` = All Commands are sorted alphabetically. This is the default and normal behavior.
 - `AlphabeticalCommandOrder` is `true` and `MergeCommonCommands` is `false` = All the normal commands are first in alphabetical order, then the common commands are listed in their own alphabetical order.
 - `AlphabeticalCommandOrder` is `false` and `MergeCommonCommands` is `true` = This use case does not make sense, because you cannot merge the commands and yet keep them in the order they were listed in their YAML file. This combination results in a hard error at startup.
 - `AlphabeticalCommandOrder` is `false` and `MergeCommonCommands` is `false` = All the normal commands are listed first in the order they appear in the YAML file. Then the common commands are listed also in the order in which they appear in the YAML file.

### Daemon Settings

#### `DaemonSocketPath`

This is the Unix domain socket path a `routerclid` process listens on, and a `routercli` CLI process connects to. With a daemon running, every attached session shares one copy of the product state, the tree structure's runtime defined aliases and level passwords, the user database, and the role set. Without one, each session holds its own copy, which drifts as soon as two sessions change different things.

The default is empty, meaning RouterCLI runs standalone, one process per connection, with its state freshly loaded at startup and no daemon involved.

Set this only once a real `routerclid` binary is running, built with `make build-daemon` from the repository root, and pointed at the same path. The daemon holds one persisted static identity key pair, used to authenticate itself to a connecting client, generated automatically the first time it starts and written beside the socket path itself, `<DaemonSocketPath>.key`, kept private to the daemon, and `<DaemonSocketPath>.key.pub`, world readable, so a connecting CLI client can read it before it ever dials the socket. Neither key file needs a configuration entry of its own; both are always derived from `DaemonSocketPath`, see `daemon.StaticKeyPath`.

A vendor building a new command that touches shared state, meaning the product state, the tree structure, the user database, or the role set, reaches that state through `command.AppContext.DaemonClient` rather than a direct field access. The same handler then works correctly whether the deployment runs standalone or against a daemon. The "Application State" sections in `cmd/product/doc.go` and `cmd/core/doc.go` carry the full pattern and worked examples.

### Authentication Settings

#### `AuthRequired`

This property determines whether a login prompt runs before the command loop starts. The default is `false`. When set to `false` the initial session login prompt never runs, and every session stays unauthenticated. This is independent of any Command Level or Command specific authentication requirements defined in the `password_hash` property for those Command Levels and Commands (see `var/tree/README.md`). A Command Level's `password_hash`, or an individual Command's `password_hash`, works exactly the same whether `AuthRequired` is `true` or `false`. Setting `AuthRequired` to `true` while both `EnableHostAuthentication` and `EnableCLIAuthentication` below are `false` is a hard error at startup, since there would then be no way to establish a session's identity.

`AuthRequired` also decides whether any Command or Command Level's `allowed_roles` property, see `var/tree/README.md`'s Roles section, is enforced at all. This project exists as a library first, meant to be picked up with nothing configured yet and produce a usable, wide open command line. With `AuthRequired` left at its default `false`, this project's shipped setting, no session anywhere has a real identity to hold a role against, so `allowed_roles` stays wide open everywhere, `admin` included, rather than locking a project builder out of a level they just declared in their own tree file. Turning `AuthRequired` on, whichever product built on this library does that, its own vendor, or its very first administrator, is the one moment `allowed_roles` starts to mean anything.

#### `EnableHostAuthentication`

This property will use the the identity of the user that is running RouterCLI as the authenticated user in RouterCLI. RouterCLI gets this information from the standard library's os/user.Current. This does not invoke a password check and that took place via the Operating System. The default is `false`. This is primarily used for a deployment that reached over an SSH session, where the user's shell is set to run RouterCLI and the authentication is handled by `sshd`.

#### `EnableCLIAuthentication`

This property will use RouterCLI's interactive username and password login prompt, checked against whichever provider `CLIAuthProvider` is named below. The default is `true`. `EnableHostAuthentication` and `EnableCLIAuthentication` are not mutually exclusive. Both can be `true` and would be used in a scenario where RouterCLI is reached via SSH using a shared Unix account. In this scenario the OS identity alone does not tell RouterCLI who is logging in and the username capture through the CLI authentication process becomes the session's identity. The account used via SSH is only logged as how the connection was established.

#### `EnableTOTPAuthentication`

This property enables TOTP token codes in RouterCLI. The default is `true`. TOTP authentication can be used with both `EnableHostAuthentication` and `EnableCLIAuthentication`, and one of those MUST be enabled first, otherwise it is a hard error at startup.

#### `AuthProviders`

This property defines all of the backend authentication systems in use by RouterCLI. Each entry requires a `name` and a `type`. The only current option is `local` and represents a check against a bcrypt hash in the `UsersFile`. The default is a single entry named `local` of type `local`.

```yaml
AuthProviders:
  - name: local
    type: local
```

#### `CLIAuthProvider`

This property defines which entry in the `AuthProviders` should be used by `EnableCLIAuthentication`. The default is `local`. This property is ignored when `EnableCLIAuthentication` is `false`, and MUST name an entry that exists in `AuthProviders` when `EnableCLIAuthentication` is `true`.

#### `UsersFile`

This is the path and filename for the multi-user login database. The default is `etc/users.yaml`. This is only used when `AuthRequired` is `true`. `account create`/`delete`, `account roles add`/`roles remove`, `password change`, and `totp enable`/`totp disable` all change only the current session's in memory copy of this data; `write memory`, in the `admin` Command Level, is the one command that writes it back to this file, alongside `StartupConfigFile` above. A change nobody saved this way is gone the moment the process restarts.

#### `LoginMaxAttempts`

This property defines how many wrong password attempts are allowed within the `LoginAttemptWindow` before the login prompt refuses to an additional attempts for `LoginLockoutDuration`. The default is `3`. This is tracked separately per username, so repeatedly failing one username's password can never lock out a different, uninvolved username.

#### `LoginAttemptWindow`

This property defines a sliding window that `LoginMaxAttempts` is counted within. The default is `0` (disabled). If this is set then `LoginLockoutDuration` MUST also be set.

#### `LoginLockoutDuration`

This property defines how long the login prompt should refuse further attempts (e.g., `5m`). The default is `0` (disabled). If this is set then `LoginAttemptWindow` MUST also be set.

#### `CommandLevelMaxAttempts` / `CommandLevelAttemptWindow` / `CommandLevelLockoutDuration`

These properties define the same things as the three login properties above, but just for Command Level authentication. The default for `CommandLevelMaxAttempts`, however, is `0` (disabled). This is tracked per Command Level, not per user and has the same validation rules as the login properties.

#### `CommandPasswordMaxAttempts` / `CommandPasswordAttemptWindow` / `CommandPasswordLockoutDuration`

These properties define the same things as the three login properties above, but just for a specific Command's authentication. The default for `CommandPasswordMaxAttempts`, however, is `0` (disabled). This is tracked per Command, not per user and has the same validation rules as the login properties.

#### `TOTPIssuer`

This property defines the "issuer" name that is shown in a user's authenticator app next to their account name. The default is `RouterCLI`. This is purely a cosmetic value, but is shown by every mainstream app, so it is worth setting to your organization's real name for production use. Used whenever a logged in user runs `totp enable` or `totp enable qr` from inside the running CLI, the only way to enroll in TOTP.

#### `TOTPMaxAttempts`

This property defines how many times in a row a session may retype a rejected code before `totp enable` or `totp disable` gives up on that attempt and reports a failure. The default is `3`. Unlike the login and Command Level attempt limits above, there is no windowed lockout variant for this, only a flat cap on how many codes a single `totp enable` or `totp disable` run accepts before giving up.

#### `PasswordMinLength`

This property defines the minimum length in characters for a new password that is set via the `password change` command. The default is `10`. Every password, regardless of this setting, is rejected if it exceeds 72 bytes, the hard limit for the bcrypt algorithm.

#### `PasswordRequireUppercase` / `PasswordRequireNumbers` / `PasswordRequireSpecialChars`

These three properties each add one composition rule a new password MUST satisfy: at least one uppercase letter, at least one digit, and at least one punctuation or symbol character, respectively, checked with Go's `unicode.IsUpper`, `unicode.IsDigit`, and `unicode.IsPunct` / `unicode.IsSymbol`. All three default to `false`. Current guidance from NIST 800-63B recommends against mandatory composition rules in favor of length, since they tend to push people toward predictable substitutions rather than stronger passwords, which is why none of the three are on by default. They exist for a deployment that MUST still satisfy an external composition-based policy. Each is independent, so any combination may be turned on.

#### `PasswordChangeMaxAttempts`

This property defines how many times in a row the `password change` command lets a session retry, both re-entering its current password, and a TOTP code if one is configured, and re-typing a new password that failed to match its own confirmation or the policy above, before giving up. The default is `3`.

### Output Paging and Filtering Settings

#### `PagingEnabled`

This property is the deployment wide switch for the interactive `--More--` pager. The default is `true`. It only ever applies to a command whose own tree entry sets `pageable: true`, see `var/tree/README.md`. Turning this off does not disable `| include`, `| exclude`, or `| begin` filtering itself, only the pause; a Pageable command's output is still filtered, it is written straight through afterward with no pause at all. This value here is only the starting point at boot; `configure terminal`, then `line`, then `paging` or `no paging`, changes it at runtime, and, once saved, at every future boot too, without editing this file, see `README.md`'s Output paging and filtering section.

#### `DefaultPageLines`

This property sets how many lines a session shows before pausing, used only when the real terminal's height cannot be detected, a non-interactive session for instance, and no `terminal length` has been typed yet that session. The default is `24`. When the terminal's height can be detected, that real height, minus one line reserved for the `--More--` prompt itself, is used instead. The same runtime override applies here as `PagingEnabled` above: `line`, then `length <0-512>`, changes this value without editing this file.

#### `FilterMatchMode`

This property chooses how `| include`, `| exclude`, and `| begin` match their pattern against each line of output. The default is `substring`, a plain, literal text search, predictable for an operator who never wants to think about regular expression metacharacters, a period in an IP address for instance. The other allowed value, `regex`, compiles the pattern as a real Go RE2 regular expression, matching exactly what a real Cisco or HP device does. Either value can be switched at runtime, for the current session only, with `terminal filter-mode <substring|regex>`.

#### `MaxFilterChainDepth`

This property limits how many `| ...` filters may be chained together on one command line, `show running-config | include eth0 | exclude shutdown` being a chain of two. The default is `2`. This exists as a security limit, not just a convenience one: an unbounded chain lets a session ask this deployment to do an unbounded amount of filtering work from one typed line. A value of `0` disables filtering entirely; a command line with any `| ...` at all is then refused with an error. A chain deeper than this value is also refused with an error, never silently truncated or run anyway.

## Properties for the users.yaml YAML file

The multi-user login database, only read when `AuthRequired: true` in `routercli.yaml`. The file's top-level configuration is a single `users:` key mapping username to that user's entry, the same top-level-key convention `tree_structure.yaml` uses, so both files read consistently if you have both open at once.

```yaml
users:
  admin:
    password: "$6$$2a$10$..."
  bob:
    password: "$6$$2a$10$..."
```

A user with no `password` set is a hard error at load time. An account nobody can ever log in to is almost certainly a mistake, not the intended setup.

### Accounts

#### `username`

There is no `username` property. The login name is the YAML key itself under `users:`, for example `admin` or `bob` in the example above, not a field written inside that user's entry.

#### `password`

This is a required property and contains the bcrypt hash of a user's password as a "$id$encoded" string. This is generated with `./routercli --hashpassword`.

#### `totp_secret`

An optional base32-encoded TOTP secret. When set, this user must also provide a valid six-digit code from their authenticator app to log in. This is checked right after the password verifies. Empty means no second factor is required for this user. This is never set by hand; a user with no `totp_secret` set yet logs in with a password alone, then sets it themselves by running `totp enable` or `totp enable qr` from inside the running CLI.

#### `roles`

An optional list of role names this account holds, checked against a Command or Command Level's `allowed_roles`, see `var/tree/README.md`'s roles section. Empty by default, meaning this account holds no role at all. This is never set by hand; it is changed only through `account roles add` and `account roles remove`, both in the `admin` Command Level.

#### `must_change_password`

An optional boolean, `false` by default. When `true`, a session logging in as this account is forced straight into changing its password, before anything else runs, the moment login succeeds. This is set automatically by `account create`, in the `admin` Command Level, whenever it prompted for this account's first password interactively or generated one, never when a pre-computed hash was imported instead, since an imported hash is presumed to already be the real intended credential. A successful password change, forced or voluntary, always clears this. This is never set by hand for any other reason.

### Factory defaults

`etc/defaults/` holds skeleton copies of `UsersFile` and `RolesFile`, restored over the live files by `restore-factory-defaults`, and, for `UsersFile` alone, by `erase users` too, both in the `admin` Command Level, see `var/tree/README.md`'s roles section. The shipped copy seeds exactly one bootstrap account, holding this deployment's reserved bypass role and a real, working, well-known password rather than an unusable placeholder, since `auth.LoadUsers` refuses any account with an empty password hash outright. `must_change_password` on that account forces it to be replaced with a real one the moment anyone logs in with it.

This bootstrap account matters once a deployment turns `AuthRequired` on; see `AuthRequired`'s entry above for why `admin`'s `allowed_roles` gate is not even enforced before that.
