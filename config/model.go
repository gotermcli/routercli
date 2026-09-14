// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package config

import "time"

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// Duration - This type is a YAML friendly wrapper around time.Duration. A
// YAML value such as "10m" is a string, while time.Duration is an integer
// count of nanoseconds. This type lets a configuration file write a
// duration the same way Go's duration syntax does, and handles the
// conversion on both read and write.
type Duration time.Duration

// SystemConfig - This type holds the settings that control routercli
// itself. A field left unset takes the value from DefaultSystemConfig. See
// LoadSystemConfig for how a YAML file is decoded into this type.
type SystemConfig struct {
	// ProductName is the deployment's display name, shown in the centered
	// title of the man page style header "help <command>" prints. The
	// default is "RouterCLI", so a deployment that never sets it still gets
	// a sensible title rather than a blank one.
	ProductName   string `yaml:"ProductName"`
	PreventEscape bool   `yaml:"PreventEscape"`
	LogLevel      int    `yaml:"LogLevel"`
	LogFile       string `yaml:"LogFile"`
	// BaseDir is the directory every other relative path in this file
	// resolves against. It is what lets a deployment keep its data files
	// somewhere other than the directory the binary happens to be started
	// from.
	//
	// A relative BaseDir resolves against the directory holding this
	// configuration file, not against the working directory, so the same
	// file works whatever directory the binary is launched from. The
	// shipped example sets ".." for exactly that reason: its configuration
	// lives in example/etc/ and its data in example/var/.
	//
	// Empty, the default, leaves every path resolving against the working
	// directory.
	//
	// An absolute path anywhere in this file is never rewritten, whatever
	// BaseDir is set to.
	BaseDir string `yaml:"BaseDir"`

	HistoryFile           string   `yaml:"HistoryFile"`
	AuditLogFile          string   `yaml:"AuditLogFile"`
	AuditLogEnabled       bool     `yaml:"AuditLogEnabled"`
	CurrentLanguage       string   `yaml:"CurrentLanguage"`
	DefaultLanguage       string   `yaml:"DefaultLanguage"`
	LanguageDir           string   `yaml:"LanguageDir"`
	SessionIdleTimeout    Duration `yaml:"SessionIdleTimeout"`
	ElevationTimeout      Duration `yaml:"ElevationTimeout"`
	ReauthGracePeriod     Duration `yaml:"ReauthGracePeriod"`
	AuthBypassTrustWindow Duration `yaml:"AuthBypassTrustWindow"`
	TreeStructure         string   `yaml:"TreeStructure"`
	CommonTreeFile        string   `yaml:"CommonTreeFile"`
	StartupConfigFile     string   `yaml:"StartupConfigFile"`

	// VendorConfigFile holds a VendorRestricted Command Level's real,
	// unredacted state.
	//
	// It is separate from StartupConfigFile so that "erase startup-config"
	// and "restore-factory-defaults" never touch it, and so its content
	// never appears in either show command.
	VendorConfigFile string `yaml:"VendorConfigFile"`

	AuthRequired bool   `yaml:"AuthRequired"`
	UsersFile    string `yaml:"UsersFile"`

	// DaemonSocketPath is the Unix domain socket a routerclid process
	// listens on and a CLI client connects to, for state shared across
	// sessions.
	//
	// Empty, the default, runs the CLI standalone: one process per
	// connection, its own state loaded at startup, no daemon involved. A
	// deployment sets this, and runs the daemon pointed at the same path,
	// only when it wants shared state.
	DaemonSocketPath               string   `yaml:"DaemonSocketPath"`
	LoginMaxAttempts               int      `yaml:"LoginMaxAttempts"`
	LoginAttemptWindow             Duration `yaml:"LoginAttemptWindow"`
	LoginLockoutDuration           Duration `yaml:"LoginLockoutDuration"`
	CommandLevelMaxAttempts        int      `yaml:"CommandLevelMaxAttempts"`
	CommandLevelAttemptWindow      Duration `yaml:"CommandLevelAttemptWindow"`
	CommandLevelLockoutDuration    Duration `yaml:"CommandLevelLockoutDuration"`
	CommandPasswordMaxAttempts     int      `yaml:"CommandPasswordMaxAttempts"`
	CommandPasswordAttemptWindow   Duration `yaml:"CommandPasswordAttemptWindow"`
	CommandPasswordLockoutDuration Duration `yaml:"CommandPasswordLockoutDuration"`
	TOTPIssuer                     string   `yaml:"TOTPIssuer"`
	TOTPMaxAttempts                int      `yaml:"TOTPMaxAttempts"`
	PasswordMinLength              int      `yaml:"PasswordMinLength"`
	PasswordRequireUppercase       bool     `yaml:"PasswordRequireUppercase"`
	PasswordRequireNumbers         bool     `yaml:"PasswordRequireNumbers"`
	PasswordRequireSpecialChars    bool     `yaml:"PasswordRequireSpecialChars"`
	PasswordChangeMaxAttempts      int      `yaml:"PasswordChangeMaxAttempts"`

	// EnableHostAuthentication trusts the operating system account the
	// process runs as, read through os/user.Current, as proof of who is
	// connecting. No password is prompted for on this path.
	//
	// This is for a deployment reached over SSH, where sshd already
	// authenticated the Unix account before routercli started, whether as a
	// login shell or through a ForceCommand.
	EnableHostAuthentication bool `yaml:"EnableHostAuthentication"`

	// EnableCLIAuthentication, when true, runs routercli's interactive
	// username and password login, auth.PromptLogin, checked against
	// whichever provider CLIAuthProvider names below. It is the only
	// authentication source enabled by default.
	//
	// EnableHostAuthentication and EnableCLIAuthentication are not
	// mutually exclusive. Both true describes a shared Unix account
	// reached over SSH, where the OS identity alone does not tell
	// routercli which real person is at the keyboard. In that combination,
	// the CLI login's username becomes Session.Username, the identity used
	// for everything from that point on, while the OS account routercli
	// was reached as is kept on Session.HostUsername purely as a record of
	// how the connection arrived. See main.go's establishSession.
	EnableCLIAuthentication bool `yaml:"EnableCLIAuthentication"`

	// EnableTOTPAuthentication is the deployment wide switch for requiring a
	// TOTP code on top of whichever flag above identifies the account.
	//
	// It cannot stand alone. validate rejects it unless at least one of the
	// two above is also true, since a second factor is a step up on a
	// primary identity, not a substitute for one.
	//
	// With CLI authentication on, this governs whether the per-user
	// TOTPSecret check runs at all; false means a session is never
	// challenged even if a user record carries a secret. With only host
	// authentication on, this is the only thing that requires a code.
	EnableTOTPAuthentication bool `yaml:"EnableTOTPAuthentication"`

	// AuthProviders names every authentication backend this deployment has
	// configured, so adding one, an LDAP or RADIUS server for instance, is
	// an entry here rather than a code change.
	//
	// Only CLIAuthProvider is used today, and only the "local" type, bcrypt
	// hashes in UsersFile, has an implementation. An unrecognized type is a
	// startup error.
	AuthProviders []AuthProviderConfig `yaml:"AuthProviders"`

	// CLIAuthProvider names which entry in AuthProviders
	// EnableCLIAuthentication's login prompt checks a typed password
	// against. It is ignored entirely when EnableCLIAuthentication is
	// false.
	CLIAuthProvider string `yaml:"CLIAuthProvider"`

	// AlphabeticalCommandOrder, true by default, sorts any listing of more
	// than one command name by name. That covers "help", "?", and the Tab
	// completion candidate list. Cisco and HP both sort alphabetically,
	// which is why this is the default.
	//
	// False shows commands in the order their own tree file defines them,
	// with a level's own commands before common ones regardless of
	// MergeCommonCommands, because there is no single combined definition
	// order across two files.
	//
	// This MUST be true whenever MergeCommonCommands is.
	AlphabeticalCommandOrder bool `yaml:"AlphabeticalCommandOrder"`

	// MergeCommonCommands, true by default, places a common command, such as
	// help or exit, in its normal alphabetical position among every other
	// command in a listing. Cisco and HP do the same.
	//
	// False appends the common commands after everything else, alphabetical
	// among themselves.
	//
	// This MUST NOT be true while AlphabeticalCommandOrder is false.
	// validate rejects that at startup, because without one alphabetical
	// order there is nothing coherent to merge into.
	MergeCommonCommands bool `yaml:"MergeCommonCommands"`

	// PagingEnabled, true by default, is the deployment wide switch for the
	// interactive "--More--" pause. Cisco and HP always pause, which is why
	// this is the default.
	//
	// False prints every line straight through without ever blocking on a
	// keypress. Output filtering is a separate concern and stays available
	// either way.
	PagingEnabled bool `yaml:"PagingEnabled"`

	// DefaultPageLines, 24 by default, matching the classic terminal height,
	// is how many lines a Pageable command shows before pausing when no
	// session has typed "terminal length" and the real terminal height
	// cannot be read, such as with piped stdin.
	DefaultPageLines int `yaml:"DefaultPageLines"`

	// DefaultHistorySize, 500 by default, matching the readline library,
	// fixes how many past commands Up and Down arrow recall remembers for
	// the life of the session.
	//
	// It also sets how many lines "show history" prints, unless a session
	// types "terminal history size <n>". Only that second use can change
	// after startup; the arrow recall limit is fixed when the session
	// begins.
	DefaultHistorySize int `yaml:"DefaultHistorySize"`

	// FilterMatchMode, "substring" by default, chooses how a "| include",
	// "| exclude", or "| begin" pattern is matched.
	//
	// "substring" keeps a line that literally contains the typed text, which
	// is predictable for an operator who never wants to think about a
	// metacharacter hiding inside an ordinary word. "regex" compiles the
	// pattern as an RE2 expression instead, matching Cisco and HP exactly.
	// validate rejects anything else.
	//
	// A session can change this with "terminal filter-mode".
	FilterMatchMode string `yaml:"FilterMatchMode"`

	// MaxFilterChainDepth, 2 by default, is the most filter stages one
	// command line may chain. "show running-config | include interface |
	// exclude shutdown" is a chain of two.
	//
	// A line asking for more is refused with an error naming the maximum,
	// never silently truncated. Zero disables filtering entirely, which is a
	// hardening option for a deployment that wants paging but no pipe
	// filtering. validate rejects a negative value.
	MaxFilterChainDepth int `yaml:"MaxFilterChainDepth"`

	// RolesFile is where a deployment declares which role names exist for
	// use with a Command or CommandLevel's allowed_roles list, see
	// command.LoadRoles. A missing file is not an error, and means this
	// deployment never uses AllowedRoles at all.
	RolesFile string `yaml:"RolesFile"`

	// DefaultsDir holds this deployment's factory default files, at minimum
	// a skeleton UsersFile seeded with one bootstrap account holding the
	// bypass role.
	//
	// "erase users" and "restore-factory-defaults" copy from here, matched
	// by the live file's base name, rather than deleting a file to nothing
	// the way "erase startup-config" does.
	DefaultsDir string `yaml:"DefaultsDir"`
}

// AuthProviderConfig - This type is one entry in SystemConfig's
// AuthProviders list, naming one authentication backend and which kind it
// is. See AuthProviders' own doc comment for what Type values are
// recognized today.
type AuthProviderConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}
