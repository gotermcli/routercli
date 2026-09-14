// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"sync"
	"time"
)

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// RateLimiter - A password prompt with no limit is a password prompt an
// attacker can sit and guess at.
//
// This is a sliding window limiter with a lockout. After maxAttempts
// failures inside window, further attempts are refused for lockout.
//
// The three values match Cisco's "login block-for ... attempts ... within ..."
// directive rather than inventing new terms, since that is what an
// operator coming from real network gear already knows.
//
// A maxAttempts at or below zero disables limiting entirely: Allow always
// returns true and the record calls do nothing. A nil *RateLimiter behaves
// the same, so a caller never needs a nil check.
//
// Safe for concurrent use. A limiter attached to a Command or CommandLevel
// is shared state.
type RateLimiter struct {
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
	now         func() time.Time

	mu          sync.Mutex
	failures    []time.Time
	lockedUntil time.Time
}

// KeyedRateLimiter - Login is the one place rate limiting must be scoped
// per identity. A single shared limiter across every username would let
// anyone lock out any other user by failing that user's password a few
// times, which is its own denial of service.
//
// This is a RateLimiter per key, created on first use.
//
// Command Level and per-command passwords do not need this. Each of those
// is a single shared secret rather than a per-user one, so a plain
// RateLimiter is enough.
type KeyedRateLimiter struct {
	maxAttempts int
	window      time.Duration
	lockout     time.Duration

	mu       sync.Mutex
	limiters map[string]*RateLimiter
}

// Session - This type tracks one CLI session's authentication state.
type Session struct {
	// Username is empty until a successful login. It is used for audit log
	// entries only.
	Username string

	// Authenticated is true once the login prompt, or an equivalent
	// caller, has verified a password. With AuthRequired false the login
	// prompt never runs and this stays false for the whole session.
	//
	// This field is informational. It does not gate which commands a
	// session can run. Command reachability is a property of the tree
	// structure and of whatever password_hash a deployment sets on a
	// Command Level or an individual command, both decoupled from this.
	Authenticated bool

	// CommandLevel names the Command Level this session is currently in.
	//
	// main.go sets it to the base level at startup, since NewSession does
	// not know the base level's name. It is then updated by whichever
	// cmd_*.go file calls EnterCommandLevel or ExitCommandLevel.
	//
	// This is meaningful only for root swap levels. A nested mode such as
	// config or config-if never touches it and is tracked through the
	// CommandLevelStack instead. The two are different axes.
	//
	// It lives in package auth rather than package command so that package
	// command, which already imports auth, can depend on it without an
	// import cycle. Session needs only the name, not the type.
	CommandLevel string

	// CommandLevelEnteredAt records when CommandLevel last changed.
	// runLoop uses it with ElevationTimeout to revert to the base level
	// once that much time has passed, the CLI equivalent of a privileged
	// mode timeout.
	CommandLevelEnteredAt time.Time

	// HostUsername is the operating system account the process is running
	// as, and HostConnectedAt is when this session started. Both are
	// filled in whether or not a CLI login happened, so "show users" can
	// report something for an unauthenticated session.
	HostUsername    string
	HostConnectedAt time.Time
}

// User - This type represents one entry in the user database.
type User struct {
	// Username is the account name. It is the map key in Users, so it is
	// not stored again in the YAML body.
	Username string `yaml:"-"`

	// PasswordHash is the stored credential in "$id$encoded" form.
	// VerifyPassword dispatches on that id.
	PasswordHash string `yaml:"password"`

	// TOTPSecret is the second factor secret, empty for an account with
	// none enrolled.
	TOTPSecret string `yaml:"totp_secret,omitempty"`

	// Roles is the set of role names this account has been assigned,
	// checked by command.Authorized against a Command or CommandLevel's
	// AllowedRoles list. It is empty until an administrator runs
	// "account roles add".
	//
	// Package auth has no notion of what a role gates. It carries the names
	// only.
	Roles []string `yaml:"roles,omitempty"`

	// MustChangePassword forces a session logging in as this account
	// straight into the password change flow, before anything else runs.
	//
	// It is set when "account create" either prompted for the first
	// password or generated one, never when a pre-computed hash was
	// imported, since an imported hash is presumed to already be the
	// intended credential. Any successful password change clears it.
	MustChangePassword bool `yaml:"must_change_password,omitempty"`
}

// Users - This type is the in-memory form of the whole user database.
type Users map[string]*User

// yamlUsersFile - This type is the on-disk shape. Everything sits under a
// "users:" key, the same top-level key convention the tree YAML loader
// uses, so both configuration files read consistently if someone opens
// both at once.
type yamlUsersFile struct {
	Users map[string]User `yaml:"users"`
}

// PasswordPolicy - This type is the set of rules a new password must
// satisfy, checked by ValidatePassword.
//
// It mirrors the Password settings in SystemConfig field for field, but is
// declared here rather than imported, because package auth MUST NOT depend
// on package config. main.go builds one from the loaded configuration and
// carries it on AppContext.
type PasswordPolicy struct {
	MinLength           int
	RequireUppercase    bool
	RequireNumbers      bool
	RequireSpecialChars bool
}

// PasswordViolation - This type names one way a candidate password failed
// to satisfy a PasswordPolicy, or MaxPasswordLength, see ValidatePassword.
// It carries no message of its own,; package auth has no i18n awareness
// anywhere else either, see login.go's promptText, so a caller such as
// example/cmd/core/cmd_password.go maps each violation to its own translated
// message.
type PasswordViolation string

// The complete set of PasswordViolation values ValidatePassword can
// return. TooShort and TooLong are checked unconditionally; the three
// composition violations only when the matching PasswordPolicy field
// requests them.
const (
	PasswordViolationTooShort         PasswordViolation = "too_short"
	PasswordViolationTooLong          PasswordViolation = "too_long"
	PasswordViolationNeedsUppercase   PasswordViolation = "needs_uppercase"
	PasswordViolationNeedsNumber      PasswordViolation = "needs_number"
	PasswordViolationNeedsSpecialChar PasswordViolation = "needs_special_char"
)

// Provider - This is the seam a backend that checks a username and
// password plugs into. Only LocalProvider exists today, bcrypt hashes in a
// Users database. An LDAP or RADIUS backend would implement this same
// interface without VerifyLogin, PromptLogin, or the password change
// reauthentication step needing to change.
//
// Authenticate reports whether password is correct for username.
//
// A non-nil error means the check could not be completed, a network
// failure reaching a directory for instance, which is different from ok
// being false for a wrong password. A caller treats either as a failed
// login, since neither should let a session through.
type Provider interface {
	Authenticate(username, password string) (bool, error)
}

// LocalProvider - This type is the Provider backed by this project's
// example/etc/users.yaml, or whatever a project renames its own UsersFile to,
// checking a candidate password against the matching User's PasswordHash
// with VerifyPassword. This is what every deployment used before Provider
// existed at all, so it stays the default entry in
// config.DefaultSystemConfig's AuthProviders list, keeping every existing
// deployment's behavior unchanged.
type LocalProvider struct {
	Users Users
}

// ----------------------------------------------------------------------
// Initialization Functions
// ----------------------------------------------------------------------

// NewLocalProvider - This function constructs a LocalProvider checking
// candidate passwords against users.
func NewLocalProvider(users Users) *LocalProvider {
	return &LocalProvider{Users: users}
}

// NewRateLimiter - This function constructs a RateLimiter. maxAttempts at
// or below zero disables rate limiting entirely. See RateLimiter's doc
// comment.
func NewRateLimiter(maxAttempts int, window, lockout time.Duration) *RateLimiter {
	return &RateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		lockout:     lockout,
		now:         time.Now,
	}
}

// NewSession - This function returns an empty, unauthenticated session
// with CommandLevel unset.
//
// main.go sets CommandLevel to the base level's name right after. This
// function cannot: package auth has no knowledge of package command, and
// threading a base level name through here would couple the two.
func NewSession() *Session {
	return &Session{}
}
