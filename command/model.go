// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package command

import (
	"time"

	"github.com/gologme/log"
	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/i18n"
	"github.com/gotermcli/routercli/paging"
)

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// helpEntry - This type is one line of dynamically built help output. It
// holds a full command path, such as "show running-config", and its
// resolved description.
type helpEntry struct {
	path string
	desc string
}

// commandTreeFile - This type is the top-level shape of a tree file.
// Everything lives under a single "commands:" key. This is the only
// wrapper type this file needs. Command itself decodes directly through
// its own yaml tags, see Command's doc comment, rather than through a
// separate mirror type.
type commandTreeFile struct {
	Commands commandMap `yaml:"commands"`
}

// CommandLevelFrame - A session can be several levels deep, in config and
// then inside an interface. Each entered level is one frame, and the
// current level is whichever frame is on top of the stack.
//
// Name identifies the level, such as "config" or "config-if". It is not
// shown to the user, but lets a handler know where it is without matching
// against a formatted prompt string.
//
// PromptSuffix is appended to the prompt while this frame is current, so
// "(config)" gives "router(config)#". It is empty for the root frame.
//
// Tree is what is reachable while this frame is current, built by merging
// the level's tree file with the common tree, so every level gets help,
// exit, and end without redefining them.
//
// Context carries whatever the level is editing, such as the interface
// name for "interface eth0". This package never interprets it.
type CommandLevelFrame struct {
	Name         string
	PromptSuffix string
	Tree         map[string]*Command
	Context      any
}

// CommandLevelStack - This type is the runtime Command Level state for one
// session, a stack of CommandLevelFrame, with the root at index 0. The
// root frame can never be popped. It represents the base Command Level, or
// whichever level a session has moved to once SetRootTree has swapped it.
// Leaving the root is what quits the program, see ErrQuit, not something
// CommandLevelStack itself decides.
type CommandLevelStack struct {
	frames []CommandLevelFrame
}

// Command - This type represents a single command in a Command Level, the
// building block every tree is made of, whether loaded from YAML or
// constructed by hand.
//
// Every field except RunFunc and PasswordRateLimiter decodes directly from
// YAML through its own tag. example/var/tree/README.md documents what each YAML
// property means.
type Command struct {
	// Desc is the one line description shown in a help listing. DescKey
	// is the message catalog key for the same text, and wins when both are
	// set.
	Desc    string `yaml:"desc"`
	DescKey string `yaml:"desc_key"`

	// Help is the longer body shown by "help <command>". HelpKey is the
	// catalog key for the same text, and wins when both are set.
	Help    string `yaml:"help"`
	HelpKey string `yaml:"help_key"`

	// ArgHelp is the argument hint shown after the command name, such as
	// "<hostname>". ArgHelpKey is the catalog key for the same text, and
	// wins when both are set.
	ArgHelp    string `yaml:"arghelp"`
	ArgHelpKey string `yaml:"arghelp_key"`

	// Run holds the raw "run:" name a YAML file gave for this command.
	// LoadTree resolves it into RunFunc against the handler registry
	// immediately after decoding.
	//
	// A command built directly in Go sets RunFunc itself and leaves this
	// empty. A container with only Subcommands leaves both empty, which is
	// why RunFunc is nil checked at dispatch time.
	Run string `yaml:"run"`

	// Alias names another command this one expands to.
	Alias string `yaml:"alias"`

	// Hidden keeps this command out of help listings and tab completion.
	// It stays runnable by anyone who types it in full.
	Hidden bool `yaml:"hidden"`

	// Negatable allows this command to be reached through a leading "no".
	Negatable bool `yaml:"negatable"`

	// PasswordHash gates running this command behind a password prompt.
	PasswordHash string `yaml:"password_hash"`

	// VendorDefinedPasswordHash is a bcrypt hash baked into the tree by
	// whoever built this product, gating this command exactly the way
	// PasswordHash does, but never meant to be seen or changed by an
	// ordinary end user, only known out of band by support staff or a
	// sales engineer. See EffectivePasswordHash and UserSettablePassword
	// below for exactly how this interacts with PasswordHash, and
	// example/var/tree/README.md for the full explanation, including the rules the
	// configuration checker enforces around this field: a command setting
	// VendorDefinedPasswordHash MUST also set Hidden true, and MUST NOT
	// set PasswordUserSettable true or set PasswordHash at the same time.
	VendorDefinedPasswordHash string `yaml:"vendor_defined_password_hash"`

	// PasswordUserSettable controls whether an end user is allowed to set
	// or change this command's secret at all. This is a *bool, not a plain
	// bool, so that leaving it out of a tree file entirely, the
	// overwhelming common case today, keeps today's actual behavior, true,
	// rather than silently locking every existing command's password the
	// moment this field was added. See UserSettablePassword below for how
	// a nil value, an explicit true, and an explicit false are each
	// resolved, and why VendorDefinedPasswordHash being set always wins
	// regardless of what this field says.
	PasswordUserSettable *bool `yaml:"password_user_settable"`

	// MinArgs and MaxArgs bound how many argument tokens may follow this
	// command. Both are *int so that leaving them out of a tree file means
	// unbounded, rather than zero. ValidateArgs checks them before the
	// handler runs, so a handler never checks its own argument count.
	MinArgs *int `yaml:"minargs"`
	MaxArgs *int `yaml:"maxargs"`

	// MaxArgLength caps the length of any single argument token. Zero
	// means no cap.
	MaxArgLength int `yaml:"maxarglength"`

	// Requires names a feature flag that MUST be true for this command,
	// and its Subcommands, to exist in the tree at all.
	// PruneDisabledCommands checks it once at startup. Empty, the default,
	// means always available.
	//
	// This differs from PasswordHash in kind, not just in name. A password
	// gates whether a reachable command may run. Requires gates whether the
	// command is reachable, or shown in help and tab completion, at all.
	//
	// That distinction matters for a command whose reason to exist depends
	// on a feature being on. When EnableCLIAuthentication is false, the
	// right behavior for "password change" is not to exist, rather than to
	// exist and refuse.
	Requires string `yaml:"requires"`

	// AllowedRoles is the set of role names allowed to run this command,
	// empty by default.
	//
	// An empty list means no role gate, and Authorized always returns true.
	// A non-empty list refuses a session unless the logged in user holds at
	// least one role in it, or holds the deployment's bypass role. That is
	// deny by default, with no other exception.
	//
	// This is checked at the same point EffectivePasswordHash is, and is
	// independent of it. A command can carry a password, a role list, both,
	// or neither, and both are enforced when both are set.
	AllowedRoles []string `yaml:"allowed_roles"`

	// Pageable opts this command into output capture, "| include", "|
	// exclude", and "| begin" filtering, and the "--More--" pager. It is
	// false by default.
	//
	// Piping a command that is not Pageable is a real error at the command
	// line, "%... does not support output filtering or paging", not a
	// silent no-op.
	//
	// This is opt-in rather than universal because output capture works by
	// redirecting the process wide os.Stdout for the duration of one
	// RunFunc call. That is only safe for a handler whose entire output is
	// produced up front.
	//
	// A handler that prompts partway through, "totp enable" or "password
	// change" for instance, MUST never set this. Its prompt would be
	// swallowed into the capture buffer instead of reaching the terminal,
	// where someone needs to see it before typing a blind response.
	//
	// Marking every report style command is the shipped convention,
	// matching real Cisco and HP, which paginate display commands and
	// nothing else.
	Pageable bool `yaml:"pageable"`

	// Subcommands holds the commands nested under this one, keyed by
	// command word. A command with subcommands and no Run is a container:
	// typing it alone is an incomplete command, not a failure.
	Subcommands commandMap `yaml:"subcommands"`

	// RunFunc is the handler this command dispatches to. It carries no yaml
	// tag, because a Go function has no YAML representation. LoadTree fills
	// it in from Run; a command built in Go sets it directly.
	RunFunc HandlerFunc `yaml:"-"`
	// PasswordRateLimiter gates repeated attempts against PasswordHash, the
	// same way CommandLevel.RateLimiter gates a Command Level's password.
	//
	// This is nil until main.go wires one in, after loading, for every
	// command with a non-empty PasswordHash. A nil limiter behaves exactly
	// like a disabled one, so this field is safe to read before that
	// wiring happens.
	PasswordRateLimiter *auth.RateLimiter `yaml:"-"`

	// DefIndex is this command's position among its siblings, in the order
	// its source YAML mapping reads. UnmarshalYAML sets it, since a Go map
	// has no order to recover afterward.
	//
	// It is what ListOptions.Alphabetical false sorts by, and is not decoded
	// from YAML.
	//
	// A Command built directly in Go leaves it at zero, which is harmless:
	// every hand built command in a test tree ties at 0, and every loaded
	// tree sets it.
	DefIndex int `yaml:"-"`

	// IsCommonCommand is true for a command that came from the common tree
	// file, help, "?", exit, and end, merged into every level unless
	// SkipCommonMerge.
	//
	// LoadTreeStructure sets it once, right after loading that tree and
	// before merging it anywhere.
	//
	// It is what ListOptions.MergeCommon false checks to decide which group
	// a command belongs to. It is not decoded from YAML: a tree file has no
	// way to mark its own commands common, since only LoadTreeStructure
	// knows which tree it loaded as the common one.
	IsCommonCommand bool `yaml:"-"`
}

// effectivePasswordHash - This function decides which of the two hashes a
// Command or a CommandLevel carries governs. A vendor defined hash always
// wins over an ordinary one when both are set, so a deployment cannot
// shadow a vendor secret by writing an ordinary hash beside it.
//
// Command and CommandLevel both expose this through their own
// EffectivePasswordHash method. The rule lives here, once, so the two can
// never drift apart.
func effectivePasswordHash(vendorDefined, ordinary string) string {
	if vendorDefined != "" {
		return vendorDefined
	}
	return ordinary
}

// userSettablePassword - This function decides whether an end user may set
// or change a PasswordHash at all.
//
// Anything carrying a vendor defined hash is never user settable,
// whatever userSettable says. A vendor defined secret is never end user
// changeable.
//
// Otherwise userSettable is followed directly. nil, meaning the tree file
// never set it, resolves to true.
//
// Command and CommandLevel both expose this through their own method. The
// rule lives here once so the two cannot drift.
func userSettablePassword(vendorDefined string, userSettable *bool) bool {
	if vendorDefined != "" {
		return false
	}
	if userSettable != nil {
		return *userSettable
	}
	return true
}

// EffectivePasswordHash returns whichever hash gates this command right
// now: VendorDefinedPasswordHash when it is set, otherwise PasswordHash.
// The two are never both meaningful at once, see
// VerifyVendorDefinedSecrets in verify.go, which refuses a tree that sets
// both on the same command, so callers checking whether a command is
// password gated at all, and callers verifying a typed candidate against
// the stored secret, both call this rather than reading either field
// directly.
func (c *Command) EffectivePasswordHash() string {
	return effectivePasswordHash(c.VendorDefinedPasswordHash, c.PasswordHash)
}

// UserSettablePassword reports whether an end user is allowed to set or
// change this command's PasswordHash at all. A command carrying a
// VendorDefinedPasswordHash is never user settable, regardless of what
// PasswordUserSettable itself says, matching example/var/tree/README.md's rule
// that a vendor defined secret is never end user changeable. Otherwise
// this follows PasswordUserSettable directly: nil, meaning the tree file
// never set it, resolves to true, today's actual behavior, and an explicit
// true or false is honored as written.
func (c *Command) UserSettablePassword() bool {
	return userSettablePassword(c.VendorDefinedPasswordHash, c.PasswordUserSettable)
}

// ListOptions - This type controls how a listing of more than one command
// name is ordered. SortCommandNames applies it, and every path that prints
// several names funnels through there, so ordering is the same however a
// session asked.
//
// It lives here rather than being read from SystemConfig directly, because
// package command MUST NOT depend on package config. main.go builds one at
// startup and threads it through AppContext.
type ListOptions struct {
	// Alphabetical, when true, the default, sorts a listing by command
	// name. When false, a listing instead follows DefIndex, the order
	// commands are defined in their tree file, own commands before common
	// commands regardless of MergeCommon, since interleaving two separate
	// files' own definition order the way MergeCommon's alphabetical form
	// does has no sensible meaning.
	Alphabetical bool

	// MergeCommon, when true, the default, sorts a common command, help,
	// "?", exit, end, into a listing's normal alphabetical position among
	// every other command, matching what real Cisco and HP devices do.
	// When false, every common command is appended after every other
	// command instead, alphabetical among themselves. This only has an
	// effect when Alphabetical is also true. Definition order always
	// separates the two groups regardless of this setting.
	MergeCommon bool
}

// DefaultListOptions - This function returns the ordering this project's
// defaults, and real Cisco and HP behavior, both use: alphabetical, with
// common commands merged into their normal alphabetical position. See
// ListOptions's doc comment for what each field means, and
// config.DefaultSystemConfig for where these same two defaults are set for
// a real deployment.
func DefaultListOptions() ListOptions {
	return ListOptions{Alphabetical: true, MergeCommon: true}
}

// ResolveResult - This type is what Resolve() returns, everything a caller
// needs to know about what a line of typed tokens referred to.
type ResolveResult struct {
	FullName  []string // resolved command tokens, e.g. ["show", "running-config"]. This is the real command; "no" is never part of it.
	Command   *Command // directives and handler for the final matched command, nil if none matched
	Args      []string // leftover tokens once no further command match is possible
	Ambiguous []string // candidate names, set only when a token prefix matches more than one command
	AmbigAt   int      // index into the original tokens where the ambiguity occurred
	Negated   bool     // true if the line started with "no", see Resolve()'s doc comment

	// AmbiguousTree is the tree map Ambiguous's candidate names were drawn
	// from, set alongside Ambiguous, nil whenever Ambiguous is. A caller
	// that wants to print those names in something other than Resolve()'s
	// own plain alphabetical order, see SortCommandNames, needs this to
	// look each one's Command back up by name, which is why Resolve()
	// hands it back rather than leaving every such caller to re-walk
	// FullName down from its own copy of the root tree to reconstruct the
	// same map.
	AmbiguousTree map[string]*Command

	// RunnableAsIs is true when Command is both set and directly
	// executable exactly as typed, pressing Enter right now would run it,
	// matching real Cisco and HP's "<cr>" notation. This means
	// Command.RunFunc is not nil, and Args, with one single trailing empty
	// string stripped first, since that is only ever the synthetic
	// "nothing typed yet here" placeholder completer.OnChange and
	// HelpForPath's callers append, never a real argument, satisfies both
	// Command.MinArgs and Command.MaxArgs. See runnableAsIs, which
	// computes this the same way regardless of which of Resolve()'s
	// several return points is taken.
	RunnableAsIs bool
}

// AppContext - This type carries the shared dependencies every command
// handler needs. It is constructed once, at startup, and passed explicitly
// into each handler at call time.
//
// Handlers register themselves from init, which runs before main has built
// the real State, Logger, Session, or tree, so there is nothing to close
// over at that point. Passing the context in also means a handler's
// signature shows exactly what it depends on.
type AppContext struct {
	// State is deployment specific session data. It is typed as any
	// because this package is the reusable framework and must not know
	// what a deployment tracks. A handler in cmd/product type asserts it
	// to its own concrete type. Nothing in cmd/core touches State.
	State any

	// Logger is the shared logger used for debug level tracing.
	Logger *log.Logger

	// Session is the current login and Command Level state. It is always
	// non-nil, so CommandLevel and CommandLevelEnteredAt always have
	// somewhere to live whether or not AuthRequired is in use. Check
	// Session.Authenticated, never Session == nil, to know whether a real
	// login happened.
	Session *auth.Session

	// Users is the loaded user database, so a handler can read the current
	// user's record without that one value being threaded through
	// separately. This is nil when AuthRequired is off.
	Users auth.Users

	// UsersFile is the path Users was loaded from, so a handler that
	// changes a record can call auth.SaveUsers against the same file. This
	// is empty when AuthRequired is off, the same as Users.
	UsersFile string

	// StartupConfigFile is the saved configuration path, so the
	// "write memory", "erase startup-config", and "show startup-config"
	// commands can reach it without cmd/product depending on package
	// config. This is never empty; it has a real default.
	StartupConfigFile string

	// VendorConfigFile names the one file that holds a VendorRestricted
	// Command Level's unredacted state. That state MUST never live in
	// StartupConfigFile, or anywhere "show running-config" or
	// "show startup-config" could print it. This is never empty; it has a
	// real default.
	VendorConfigFile string

	// ProductName is the deployment's display name. DetailedHelp uses it
	// to build the man page header line. An empty value, such as in a hand
	// built test context, is treated as "RouterCLI".
	ProductName string

	// TOTPIssuer is the name shown in a user's authenticator app beside
	// their account name.
	//
	// This is a separate field from ProductName rather than the two
	// sharing one value, so renaming a deployment never silently relabels
	// an already enrolled user's authenticator entry.
	TOTPIssuer string

	// TOTPMaxAttempts is how many times a handler such as totp enable lets
	// a session retype a rejected code before giving up.
	TOTPMaxAttempts int

	// DaemonClient is what a handler calls to touch shared state, instead
	// of reading or writing State or Levels directly.
	//
	// This is never nil in a running program. With no daemon configured it
	// is a standalone client wrapping the same State and Levels this
	// AppContext already holds, so a handler behaves the same either way.
	// Only a hand built test context can leave it nil.
	DaemonClient DaemonClient

	// PasswordPolicy is the set of rules a new password must satisfy,
	// passed through to auth.ValidatePassword by the password change
	// handler.
	PasswordPolicy auth.PasswordPolicy

	// PasswordChangeMaxAttempts is how many times the password change
	// handler lets a session retry, covering both re-authenticating and
	// typing a new password that failed confirmation or PasswordPolicy.
	PasswordChangeMaxAttempts int

	// AuthProvider is the backend the password change handler
	// re-authenticates against before allowing a change. It is the same
	// value the session's original login used, so a password change is
	// checked against whichever backend owns the account rather than
	// always assuming the local database.
	//
	// This is nil when EnableCLIAuthentication is off, since there is then
	// no CLI login for any backend to have checked.
	AuthProvider auth.Provider

	// Levels is every Command Level the deployment defines, loaded from
	// example/var/tree/tree_structure.yaml. It covers both levels reached by
	// swapping the root frame and nested, stacking modes such as config.
	//
	// Every level here was loaded, merged with its parent chain, and
	// merged with the common tree at startup, so a broken tree file stops
	// the program immediately rather than surfacing the first time someone
	// types the command that needed it.
	Levels *TreeStructure

	// Audit records what commands ran. It is nil safe.
	Audit Auditor

	// Translator resolves user facing message keys to the active
	// language's text. A handler calls ctx.Translator.T("key") instead of
	// hardcoding English, so a new language is a new catalog file rather
	// than a source change.
	Translator *i18n.Translator

	// Position is the session's current stack of entered Command Levels.
	// This is what determines which commands are reachable right now. Both
	// the read loop and the completer resolve against
	// Position.Current().Tree, not against Levels.
	Position *CommandLevelStack

	// Negated is true for exactly the duration of one RunFunc call that
	// was reached through a leading "no". A handler reads it at the top of
	// its body rather than stashing it, since it means nothing once
	// RunFunc returns.
	Negated bool

	// ListOptions controls how HelpText and HelpForPath order a listing of
	// more than one command name. It is built once at startup.
	//
	// Its zero value is not the shipped default. DefaultListOptions is, so
	// a hand built context needs to set this explicitly.
	ListOptions ListOptions

	// Roles is the deployment's declared set of role names, loaded from
	// example/var/tree/roles.yaml at startup, see LoadRoles. This is nil for a
	// deployment that never sets AllowedRoles anywhere in its tree and
	// never ships a roles.yaml at all, in which case Authorized always
	// returns true, since there is nothing to check against.
	// CurrentUserRoles and Authorized are the two functions that read this
	// field; a handler wanting to check authorization itself should call
	// Authorized rather than reading Roles directly.
	Roles *RoleSet

	// RolesFile is config.SystemConfig.RolesFile, copied here once at
	// startup, the same reason UsersFile and StartupConfigFile are, so the
	// new admin Command Level's "erase users" and
	// "restore-factory-defaults" commands, see example/cmd/core/cmd_admin.go, know
	// which file on disk to restore a fresh copy of roles.yaml over,
	// without cmd/core needing any dependency on package config either.
	RolesFile string

	// AuthRequired is config.SystemConfig.AuthRequired, copied here once
	// at startup, the same reason RolesFile is, so Authorized in roles.go
	// can tell whether this deployment has authentication turned on at all
	// without package command needing any dependency on package config
	// either. This is what keeps a project's out of the box, zero setup
	// experience wide open: a Command or CommandLevel's AllowedRoles list,
	// admin's allowed_roles in this project's shipped tree for instance,
	// is only ever enforced once a deployment turns AuthRequired on, the
	// same moment identity itself starts to exist for any session at all.
	AuthRequired bool

	// DefaultsDir is config.SystemConfig.DefaultsDir, copied here once at
	// startup, the directory "erase users" and "restore-factory-defaults"
	// restore a skeleton file from, matched to a live file's base name,
	// rather than deleting to nothing. See
	// config.SystemConfig.DefaultsDir's doc comment.
	DefaultsDir string

	// ReloadScheduler tracks at most one pending, delayed "reload
	// <seconds>" or "reboot <seconds>", see example/cmd/core/cmd_admin.go and
	// PendingReload's doc comment in reload.go. main.go constructs this
	// once, at startup, and its own runLoop selects on
	// ReloadScheduler.FireChannel() alongside reading the next typed line,
	// so a scheduled reload can end the session even while the loop is
	// otherwise blocked waiting on interactive input. This is nil only in
	// a hand built *AppContext, a test for instance, that never sets it;
	// "reload <seconds>" and "reboot <seconds>" both refuse outright
	// rather than panicking when that happens, see reload.not_supported.
	ReloadScheduler *PendingReload

	// PageLines is the page size for this session, in lines. It is nil
	// until "terminal length <n>" is typed, and is never persisted.
	//
	// Once set, the pager uses it instead of detecting the terminal
	// height. Zero disables the pager, which is the Cisco convention for
	// "never pause".
	PageLines *int

	// DefaultPageLines is the fallback page size. It is used only when
	// PageLines is unset and the terminal height cannot be read, such as
	// with piped or redirected stdin.
	DefaultPageLines int

	// PagingEnabled turns the interactive "--More--" pause on or off for
	// the whole deployment. It is independent of PageLines, so a
	// deployment can keep output filtering available while never blocking
	// a session on a keypress.
	PagingEnabled bool

	// FilterMode is the live, mutable match mode "| include", "| exclude",
	// and "| begin" patterns are checked with, substring by default,
	// matching config.SystemConfig.FilterMatchMode's startup value,
	// changed at runtime through "terminal filter-mode <substring|regex>".
	// See paging.FilterMode.
	FilterMode paging.FilterMode

	// MaxFilterChainDepth is config.SystemConfig.MaxFilterChainDepth,
	// copied here once at startup, the most "|..." stages one typed line
	// may chain together, checked by paging.ParseStages. A value of zero
	// disables output filtering entirely for this deployment.
	MaxFilterChainDepth int

	// TerminalWidth is the live, per session override for how wide a line
	// this session's terminal is, nil until "terminal width <n>" is typed,
	// matching real Cisco's session scoped, never persisted terminal
	// width. Nothing in this package reads TerminalWidth today; it exists
	// so a handler that formats output to a fixed width, wrapping a long
	// line or laying out columns for instance, has one place to find the
	// session's override instead of every implementation inventing its own
	// field for the same idea. See example/cmd/session/cmd_terminal.go's "terminal
	// width" handler, the only place this is ever written.
	TerminalWidth *int

	// DefaultTerminalWidth is the fallback width used only when
	// TerminalWidth is unset and the real terminal width cannot be
	// read, such as piped or redirected stdin. It mirrors the role
	// DefaultPageLines plays for PageLines.
	//
	// No configuration field sets this. It starts at zero and is
	// changed only by a persisted "width <n>" setting replayed from
	// startup-config.
	DefaultTerminalWidth int

	// HistoryFile is the history file path, copied here once at startup and
	// already resolved to its final form.
	//
	// This is the same file readline is opened against, and readline appends
	// each submitted line to it immediately, so this always names a file in
	// sync with what the session has typed. "show history" is the only
	// reader.
	HistoryFile string

	// HistorySize is the live, per session override for
	// "terminal history size <n>", nil until that command is typed. It is
	// the same session scoped, never persisted shape PageLines and
	// TerminalWidth use, and it governs how many recent lines
	// "show history" prints back.
	//
	// It does not govern Up and Down arrow recall, which is where it differs
	// from PageLines and TerminalWidth. That recall limit is read by an
	// unsynchronized background goroutine the readline library runs for the
	// life of the instance, so main.go seeds it once at construction and
	// never reassigns it. Reassigning it from outside that goroutine was a
	// real data race, not a theoretical one.
	//
	// The "terminal history size" handler is the only writer.
	HistorySize *int

	// DefaultHistorySize is config.SystemConfig.DefaultHistorySize, copied
	// here once at startup, the fallback EffectiveHistorySize returns
	// whenever HistorySize is unset.
	DefaultHistorySize int

	// ReauthGracePeriod is how long a Command Level's last successful
	// authentication stays valid. A session that leaves a level and comes
	// back within this window is not asked again, the same shape sudo
	// uses. Zero, the default, turns this off.
	//
	// This is for a session stepping back into a level it just left, not a
	// way to stay trusted all day. ElevationTimeout covers the separate
	// case of demoting a session that has sat idle while elevated.
	ReauthGracePeriod time.Duration

	// AuthBypassTrustWindow is how long a credential check against a level
	// marked GrantAuthBypass, admin in the shipped tree, also waives the
	// prompt for every other level. ReauthGracePeriod is narrower; it
	// waives a level's prompt only for itself.
	//
	// Zero turns this off. Every level always prompts.
	//
	// It exists so a saved configuration can replay without a prompt at
	// each gated level. The live check happens once, at the bypass level,
	// never inside the replayed text.
	AuthBypassTrustWindow time.Duration

	// ReplayingStartupConfig is true only while a saved startup-config is
	// being replayed at boot. LoadStartupConfig sets it and always resets
	// it through a defer. Nothing a live session types ever sets it.
	//
	// While true, entering a password protected Command Level needs no
	// prompt, and the entry does not set LastAuthenticatedAt, so the
	// waiver grants nothing further.
	//
	// The trust here is different from ReauthGracePeriod and
	// AuthBypassTrustWindow. Those waive a prompt for a session that
	// already answered one. This one runs before any session exists. The
	// trust comes from the operating system having allowed the process to
	// run and to read the file.
	ReplayingStartupConfig bool
}

// EffectiveHistorySize - This function returns how many recent lines
// "show history" should print.
//
// It follows the same override, fallback shape EffectivePageLines uses:
// ctx.HistorySize once "terminal history size" has been typed, exactly as
// given including zero, and ctx.DefaultHistorySize otherwise.
//
// It has no effect on readline's Up and Down arrow recall, which is fixed
// when the session's instance is built.
func EffectiveHistorySize(ctx *AppContext) int {
	if ctx.HistorySize != nil {
		return *ctx.HistorySize
	}
	return ctx.DefaultHistorySize
}

// Auditor - This type is the minimal interface AppContext needs from an
// audit log, so this package can avoid importing package auditlog
// directly. Any type with these methods satisfies it.
//
// WouldLog and ForceLog exist so main.go's runLoop can snapshot whether a
// command should be logged before running it, and still log it even if the
// command's side effect was to turn logging off, such as "audit-log
// disable". See auditlog.AuditLog's doc comments for the full reasoning.
type Auditor interface {
	Log(username, command string, success bool)
	WouldLog() bool
	ForceLog(username, command string, success bool)
}

// HandlerFunc - This type is the signature every command handler must
// implement, regardless of which file it lives in. args are the leftover
// tokens after the command itself resolved, already validated against
// MinArgs, MaxArgs, and MaxArgLength by the time this is called.
type HandlerFunc func(ctx *AppContext, args []string) error

// CommandLevel and TreeStructure describe the whole collection of commands
// a CLI offers, organized into Command Levels. A Command Level is every
// command reachable at one position in that structure.
//
// A project declares all of its levels in one manifest,
// example/var/tree/tree_structure.yaml, rather than fixing them in Go. Both kinds
// of level are described the same way there: privilege steps such as
// operator then technician, each gated by its own secret, and ordinary
// nested modes such as configure terminal that carry no password.
//
// Declaring a level does not create its enter and exit commands. Those are
// hand written cmd_*.go files, the same as any other command. EnterCommand
// and ExitCommand in the manifest are metadata naming what those files are
// expected to have registered, which VerifyCommandLevels checks against
// the real registry at startup.
//
// What a level's enter command prints, logs, or audits is that file's
// decision, not an opinion this package holds.

// CommandLevel - This type describes one named Command Level in the
// project's Tree Structure. Every field except Name, Tree, and RateLimiter
// is decoded directly from YAML through its own tag, the same direct-tag
// pattern auth.User and config.SystemConfig already use, rather than
// through a separate mirror type.
type CommandLevel struct {
	// Name identifies this level. It is what Session.CommandLevel is set
	// to while a session is inside it, for a level reached by swapping the
	// root frame through EnterCommandLevel and ExitCommandLevel, and it is
	// what other levels reference through Parent. This is not decoded from
	// YAML at all. LoadTreeStructure sets it to a level's key under the
	// manifest's "trees:" map after decoding, the same way auth.LoadUsers
	// sets User.Username from that file's map key.
	Name string `yaml:"-"`

	// TreeFile is this level's command tree, see LoadTree, what it
	// contributes on top of, or instead of, its parent's tree, depending
	// on InheritParent.
	TreeFile string `yaml:"tree_file"`

	// IsBase marks the one level every session starts in, see
	// TreeStructure.Base, the root CommandLevelStack frame. Exactly one
	// level across the whole manifest must set this. It is unrelated to
	// Parent: a nested level such as config also has a Parent set, its
	// own, for example "exec", but is not the base. The base level itself
	// is the only one with an empty Parent.
	IsBase bool `yaml:"is_base"`

	// Parent is the Name of the level a session's current mode must be in
	// to reach this one, checked through RequireCurrentCommandLevel by
	// whichever hand-written cmd_*.go file registers this level's enter
	// command. It is empty only for the base level itself, which by
	// definition has nowhere before it.
	Parent string `yaml:"parent"`

	// InheritParent, when true, means this level's effective command tree
	// is its own TreeFile's commands merged on top of everything reachable
	// in Parent's tree, recursively up the chain, matching a real Cisco or
	// HP privileged exec, which keeps every user exec command available
	// once elevated, on top of what elevation itself unlocks. If false,
	// this level's tree is only its own TreeFile's commands, and the
	// parent's commands are not carried forward at all. This is ignored
	// without a Parent.
	InheritParent bool `yaml:"inherit_parent"`

	// EnterCommand is the name of the command that moves a session from
	// Parent into this level, declared metadata a hand-written cmd_*.go
	// file is expected to have registered under this exact name, see
	// VerifyCommandLevels, which checks that this is true. It is empty
	// only for the base level, which nothing enters. Every other level
	// must set this, so it can be verified.
	EnterCommand string `yaml:"enter_command"`

	// ExitCommand is the name of the command that moves a session back
	// from this level to Parent without ending the session, the
	// generalized form of "disable". This may be empty for a level with no
	// dedicated exit command of its own. It can still be left by the
	// generic, always available "exit" or "end", it just has no specially
	// named command for stepping back one level while staying connected.
	// This is only meaningful alongside EnterCommand.
	ExitCommand string `yaml:"exit_command"`

	// PasswordHash is the secret required to run EnterCommand
	// successfully: a bcrypt hash, or an empty string when no password is
	// needed. It can be changed at runtime, see
	// example/cmd/core/cmd_password_manager.go, which sets this field on whichever
	// CommandLevel the current session is in. It is seeded from the
	// manifest's password_hash entry at load time, which is optional. This
	// is distinct from Command.PasswordHash in command.go, which gates one
	// specific command rather than entry into a whole level. A project can
	// use either, both, or neither.
	PasswordHash string `yaml:"password_hash"`

	// VendorDefinedPasswordHash is a bcrypt hash baked in by whoever built
	// this product, gating EnterCommand exactly the way PasswordHash does,
	// but never meant to be seen or changed by an ordinary end user, only
	// known out of band by support staff or a sales engineer. See
	// EffectivePasswordHash and UserSettablePassword below for exactly how
	// this interacts with PasswordHash, and example/var/tree/README.md for the
	// full explanation, including the rules the configuration checker
	// enforces: a level setting VendorDefinedPasswordHash MUST also set
	// Hidden true, and MUST NOT set PasswordUserSettable true or set
	// PasswordHash at the same time.
	VendorDefinedPasswordHash string `yaml:"vendor_defined_password_hash"`

	// PasswordUserSettable controls whether "password manager", see
	// example/cmd/core/cmd_password_manager.go, is allowed to change this level's
	// PasswordHash at all. This is a *bool, not a plain bool, so that
	// leaving it out of a tree_structure.yaml entry entirely, the
	// overwhelming common case today, keeps today's actual behavior, true,
	// rather than silently locking every existing level's password the
	// moment this field was added. See UserSettablePassword below for how
	// a nil value, an explicit true, and an explicit false are each
	// resolved, and why VendorDefinedPasswordHash being set always wins
	// regardless of what this field says.
	PasswordUserSettable *bool `yaml:"password_user_settable"`

	// Hidden marks this level as one that should stay out of ordinary
	// discovery, the CommandLevel counterpart to Command.Hidden.
	//
	// It has no runtime effect inside this package. A level is reachable
	// only through its EnterCommand, and whatever tree file exposes that
	// command is what controls help and tab completion.
	//
	// It exists so the configuration checker has something concrete to
	// require: a level carrying a vendor secret, or marked VendorRestricted,
	// MUST set this, recording in the manifest that the matching
	// EnterCommand was also marked hidden in its tree file.
	Hidden bool `yaml:"hidden"`

	// VendorRestricted hides a level's configuration state completely,
	// including the "<HIDDEN>" placeholder an ordinary secret still gets.
	//
	// A placeholder line is itself a disclosure. It tells a session that a
	// locked tree exists, and if the real hash ever leaks it can be cracked
	// offline. So a level marked this way appears in neither
	// "show running-config" nor "show startup-config".
	//
	// Its state goes to VendorConfigFile instead, which neither show command
	// reads and "erase startup-config" does not remove.
	//
	// VerifyVendorDefinedSecrets enforces the pairing in both directions:
	// this requires Hidden, and a VendorDefinedPasswordHash requires this.
	VendorRestricted bool `yaml:"vendor_restricted"`

	// GrantAuthBypass marks a level whose EnterCommand requires a real,
	// freshly typed credential, never satisfiable from inside replayed
	// text, and whose recent success is trusted widely enough to waive
	// every other level's password for AuthBypassTrustWindow afterward.
	//
	// admin is the level this is meant for. It exists so a whole saved
	// configuration can be replayed accurately.
	//
	// A level with this set had better require a real credential of its
	// own. Otherwise it grants a free pass to every other level's password
	// for nothing in return, which is the pass the hash shaped mistake this
	// mechanism exists to avoid.
	GrantAuthBypass bool `yaml:"grant_auth_bypass"`

	// ShowSecrets marks a level where "show running-config" and
	// "show startup-config" may print an ordinary PasswordHash in full
	// rather than the "<HIDDEN>" placeholder they use everywhere else.
	//
	// A bare hash is still offline crackable material, so it is not shown
	// to anyone who can reach exec. Only a session already sitting at a
	// level built for this sees it.
	//
	// This has no effect inside package command. It is a signal a
	// deployment's rendering code reads off the current level, rather than
	// hardcoding a level name.
	//
	// It has nothing to do with VendorDefinedPasswordHash. A level carrying
	// one of those is always VendorRestricted, and such a level's state
	// never prints in either command regardless of this.
	ShowSecrets bool `yaml:"show_secrets"`

	// AllowedRoles, empty by default, is the set of role names allowed to
	// run EnterCommand for this level. This is the CommandLevel
	// counterpart to Command.AllowedRoles above, checked by
	// EnterCommandLevel at the same point EffectivePasswordHash is already
	// checked, and independent of it the same way. The new admin Command
	// Level this project ships is the one level whose own AllowedRoles is
	// set by default, gated to the deployment's bypass role, see
	// RoleSet.BypassRole, so only the account seeded with that role can
	// reach it out of the box. See example/var/tree/roles.yaml and
	// example/var/tree/README.md.
	AllowedRoles []string `yaml:"allowed_roles"`

	// PromptSuffix is appended to the prompt while this level, or a mode
	// pushed from it such as config interface on top of config, is
	// current, for example "(config)". This is purely manifest data, read
	// generically by whatever hand-written cmd_*.go file enters this
	// level, see EnterCommandLevel, cmd_configure.go, and
	// cmd_interface.go.
	PromptSuffix string `yaml:"prompt_suffix"`

	// SkipCommonMerge, when true, means this level's Tree does not get
	// help, exit, and end merged in from the common tree. The default,
	// false, merges common in, since every level in this project wants
	// that. This is an opt out rather than something every level has to
	// ask for.
	SkipCommonMerge bool `yaml:"skip_common"`

	// Tree is this level's fully resolved command tree: its own commands,
	// merged with the parent chain if InheritParent, merged with the
	// common tree exactly once unless SkipCommonMerge. It is populated by
	// LoadTreeStructure, and nil until then. It is not decoded from YAML.
	Tree map[string]*Command `yaml:"-"`

	// RateLimiter gates repeated EnterCommand attempts against
	// PasswordHash. See auth.RateLimiter's doc comment for the general
	// mechanism, and EnterCommandLevel below for how it is checked. This
	// is nil until main.go wires one in after LoadTreeStructure returns. A
	// nil RateLimiter behaves exactly like a disabled one, so this field
	// is safe to read before that wiring happens, it just means rate
	// limiting is not active yet. It is not decoded from YAML.
	RateLimiter *auth.RateLimiter `yaml:"-"`

	// LastAuthenticatedAt records when PasswordHash was last verified
	// against a live typed candidate by EnterCommandLevel. It stays unset
	// until that first happens.
	//
	// Nothing that merely sets what PasswordHash equals ever sets this.
	// Storing a secret is not proof that anyone typed it.
	//
	// EnterCommandLevel checks it against ReauthGracePeriod before prompting
	// again, so a session that authenticated recently is let back in. That
	// is a short memory of an answered prompt, the same shape sudo uses, not
	// a blanket bypass.
	//
	// It is not decoded from YAML and not persisted. A fresh process starts
	// every level unset.
	LastAuthenticatedAt time.Time `yaml:"-"`

	// Aliases holds this level's runtime defined command aliases, keyed by
	// the name a session types, each mapping to the tokens it expands to.
	//
	// It starts nil, the right state for a level nobody has defined one
	// against, and is only ever written by the "alias" command. It is never
	// decoded from YAML: an alias is a runtime concept, the same as Cisco's
	// "alias exec", which is itself a command typed at a prompt.
	//
	// This is keyed per level rather than one shared map for the whole tree.
	// Cisco and HP keep "alias exec" and "alias configure" as separate
	// namespaces, and RouterCLI's tree is not fixed to two levels, so
	// scoping it per level is what lets a short name make sense in one place
	// without shadowing something unrelated in another.
	Aliases map[string][]string `yaml:"-"`
}

// EffectivePasswordHash returns whichever hash gates EnterCommand for this
// level right now: VendorDefinedPasswordHash when it is set, otherwise
// PasswordHash. The two are never both meaningful at once, see
// VerifyVendorDefinedSecrets in verify.go, which refuses a tree that sets
// both on the same level, so EnterCommandLevel, and anything else asking
// whether a level is password gated at all, call this rather than reading
// either field directly.
func (cl *CommandLevel) EffectivePasswordHash() string {
	return effectivePasswordHash(cl.VendorDefinedPasswordHash, cl.PasswordHash)
}

// UserSettablePassword reports whether "password manager" is allowed to
// change this level's PasswordHash at all. A level carrying a
// VendorDefinedPasswordHash is never user settable, regardless of what
// PasswordUserSettable itself says, matching example/var/tree/README.md's rule
// that a vendor defined secret is never end user changeable. Otherwise
// this follows PasswordUserSettable directly: nil, meaning the manifest
// never set it, resolves to true, today's actual behavior, and an explicit
// true or false is honored as written.
func (cl *CommandLevel) UserSettablePassword() bool {
	return userSettablePassword(cl.VendorDefinedPasswordHash, cl.PasswordUserSettable)
}

// TreeStructure - This type is the whole loaded, validated manifest, every
// Command Level in the project, indexed by name for fast lookup, for
// entering or exiting a level, a hand-written cmd_*.go file pulling a
// nested mode's tree, or "password manager" finding the current level's
// record, alongside the ordered slice a caller might want, for example to
// print every level's status.
type TreeStructure struct {
	ByName map[string]*CommandLevel
	Order  []*CommandLevel // load order from the manifest, base level first
}

// treeStructureFile - This type is the top-level shape of the tree
// structure manifest. Everything lives under a single "trees:" key. This
// is the only wrapper type this file needs. CommandLevel itself decodes
// directly through its own yaml tags, see its own doc comment, rather than
// through a separate mirror type.
type treeStructureFile struct {
	Trees map[string]CommandLevel `yaml:"trees"`
}

// ----------------------------------------------------------------------
// Initialization Functions
// ----------------------------------------------------------------------

// NewCommandLevelStack - This function constructs a CommandLevelStack with
// a single root frame. rootName and rootSuffix are usually the manifest's
// base Command Level's Name and an empty string, but are not hardcoded
// here, since a different project might call its base level something
// else, or give it a PromptSuffix of its own.
func NewCommandLevelStack(rootName, rootSuffix string, rootTree map[string]*Command) *CommandLevelStack {
	return &CommandLevelStack{
		frames: []CommandLevelFrame{{Name: rootName, PromptSuffix: rootSuffix, Tree: rootTree}},
	}
}
