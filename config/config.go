// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------
// Public Methods - Duration
// ----------------------------------------------------------------------

// AsDuration - This method converts the configuration duration back to a
// time.Duration for use with the standard library.
func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}

// IsZero - This method reports whether the duration is disabled, meaning
// zero.
func (d Duration) IsZero() bool {
	return d == 0
}

// String - This method implements fmt.Stringer. It prints the duration
// using Go's duration syntax, for example "10m0s".
func (d Duration) String() string {
	return time.Duration(d).String()
}

// durationKindName - This function names a yaml.Kind for use in
// UnmarshalYAML's error message, so a wrongly shaped duration value points
// at what was found, instead of just a generic parse failure.
func durationKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind %d", int(k))
	}
}

// UnmarshalYAML - This method implements yaml.Unmarshaler. It parses a
// scalar value such as "10m" or "30s" with time.ParseDuration. An empty
// value, a plain "0", or an explicit YAML null all mean disabled, and
// unmarshal to zero.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar, got %s node", durationKindName(node.Kind))
	}

	if node.Tag == "!!null" {
		*d = 0
		return nil
	}

	s := node.Value

	if s == "" || s == "0" {
		*d = 0
		return nil
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}

	if parsed < 0 {
		return fmt.Errorf("duration must be zero or positive, got %q", s)
	}

	*d = Duration(parsed)
	return nil
}

// MarshalYAML - This method implements yaml.Marshaler. It writes the
// duration back out using Go's duration syntax, so a generated
// configuration file reads the same way a hand-written one would.
func (d Duration) MarshalYAML() (any, error) {
	if d == 0 {
		return "0s", nil
	}
	return time.Duration(d).String(), nil
}

// ----------------------------------------------------------------------
// Public Functions - System Config
// ----------------------------------------------------------------------

// DefaultSystemConfig - This function returns the behavior settings
// routercli uses when no configuration file is found: timeouts, limits,
// feature switches, and the password policy.
//
// Every path is left empty. A directory layout is a deployment's decision,
// not something this library should have an opinion about, and a default
// naming directories the library does not ship produces a confusing missing
// file error rather than a useful one.
//
// A deployment sets its own paths in its configuration file. See
// example/etc/routercli.yaml for a worked set, and BaseDir for how relative
// paths are resolved.
func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		ProductName:                 "RouterCLI",
		PreventEscape:               false,
		LogLevel:                    0,
		HistoryFile:                 "",
		AuditLogFile:                "",
		AuditLogEnabled:             false,
		CurrentLanguage:             "en",
		DefaultLanguage:             "en",
		LanguageDir:                 "",
		TreeStructure:               "",
		CommonTreeFile:              "",
		RolesFile:                   "",
		DefaultsDir:                 "",
		StartupConfigFile:           "",
		VendorConfigFile:            "",
		AuthRequired:                false,
		UsersFile:                   "",
		LoginMaxAttempts:            3,
		TOTPIssuer:                  "RouterCLI",
		TOTPMaxAttempts:             3,
		PasswordMinLength:           10,
		PasswordRequireUppercase:    false,
		PasswordRequireNumbers:      false,
		PasswordRequireSpecialChars: false,
		PasswordChangeMaxAttempts:   3,
		EnableHostAuthentication:    false,
		EnableCLIAuthentication:     true,
		EnableTOTPAuthentication:    true,
		AuthProviders:               []AuthProviderConfig{{Name: "local", Type: "local"}},
		CLIAuthProvider:             "local",
		AlphabeticalCommandOrder:    true,
		MergeCommonCommands:         true,
		PagingEnabled:               true,
		DefaultPageLines:            24,
		DefaultHistorySize:          500,
		FilterMatchMode:             "substring",
		MaxFilterChainDepth:         2,
		// This defaults to a real value where SessionIdleTimeout,
		// ElevationTimeout, and ReauthGracePeriod all default to zero.
		// Those three each narrow behavior that already works, so opting in
		// is the right default.
		//
		// This one is the opposite. Zero would leave admin unable to do the
		// job it exists for, letting a saved configuration paste back in
		// without a prompt at every gated level. Five minutes is long enough
		// to paste a sizeable configuration by hand, short enough that it is
		// never mistaken for a standing bypass.
		AuthBypassTrustWindow: Duration(5 * time.Minute),
	}
}

// LoadSystemConfigOrDefaults - This function is LoadSystemConfig except
// that an empty configFile, or one naming a file that does not exist,
// returns DefaultSystemConfig with no error.
//
// This is for the built in default path, which the operator never named,
// so a deployment can run before it has written a configuration file.
//
// A file that does exist is still parsed and validated in full, so a
// malformed one is an error either way.
func LoadSystemConfigOrDefaults(configFile string) (SystemConfig, error) {
	if configFile == "" {
		return DefaultSystemConfig(), nil
	}
	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		return DefaultSystemConfig(), nil
	}
	return LoadSystemConfig(configFile)
}

// resolvePaths - This method rewrites every relative path in the
// configuration so it resolves against BaseDir rather than against whatever
// directory the binary was started from.
//
// Without it, a deployment whose data lives beside its configuration file has
// to be launched from exactly the right directory, and fails with a confusing
// missing file error anywhere else.
//
// configFile is the path the configuration was read from. A relative BaseDir
// resolves against that file's directory, so one configuration file works from
// any working directory. An empty BaseDir leaves every path untouched, which
// is the behavior a deployment that never sets it already has.
//
// An absolute path is never rewritten. DaemonSocketPath is included, since a
// deployment may reasonably keep its socket beside its other runtime state,
// and an absolute path such as /var/run/routerclid.sock still passes through.
func (c *SystemConfig) resolvePaths(configFile string) {
	if c.BaseDir == "" {
		return
	}

	base := c.BaseDir
	if !filepath.IsAbs(base) {
		base = filepath.Join(filepath.Dir(configFile), base)
	}
	base = filepath.Clean(base)
	c.BaseDir = base

	for _, p := range []*string{
		&c.HistoryFile,
		&c.AuditLogFile,
		&c.LanguageDir,
		&c.TreeStructure,
		&c.CommonTreeFile,
		&c.StartupConfigFile,
		&c.VendorConfigFile,
		&c.UsersFile,
		&c.RolesFile,
		&c.DefaultsDir,
		&c.DaemonSocketPath,
	} {
		if *p == "" || filepath.IsAbs(*p) {
			continue
		}
		*p = filepath.Join(base, *p)
	}
}

// LoadSystemConfig - This function reads the YAML configuration file and
// decodes it into a SystemConfig.
//
// configFile MUST name a file that exists and MUST parse cleanly. A
// missing file is an error, not a silent fall back to defaults, because a
// typo in --config must never start the program on settings nobody asked
// for.
//
// An unknown YAML key is an error too, so a misspelled property is caught
// at startup rather than ignored. The result is passed through validate.
//
// Use LoadSystemConfigOrDefaults where a missing file is acceptable.
func LoadSystemConfig(configFile string) (SystemConfig, error) {
	cfg := DefaultSystemConfig()

	if configFile == "" {
		return cfg, errors.New("no configuration file was named")
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return cfg, fmt.Errorf("error opening configuration file %q: %w", configFile, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			// The file is empty, or contains comments only.
			return cfg, nil
		}
		return cfg, fmt.Errorf("error parsing configuration file %q: %w", configFile, err)
	}

	// A configuration file is expected to be a single top-level YAML
	// mapping, so decoding a second document here means the file contains
	// more than one, and that extra document is rejected.
	var extra SystemConfig
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return cfg, fmt.Errorf("configuration file %q contains multiple YAML documents", configFile)
		}
		return cfg, fmt.Errorf("error parsing configuration file %q: %w", configFile, err)
	}

	cfg.resolvePaths(configFile)

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid configuration file %q: %w", configFile, err)
	}

	return cfg, nil
}

// ----------------------------------------------------------------------
// Private Methods - System Config
// ----------------------------------------------------------------------

// validate - This method checks the key fields of a SystemConfig after it
// has been decoded from YAML, so a mistake such as an out of range
// LogLevel or a half-configured rate limiter is caught at startup rather
// than causing confusing behavior later. See LoadSystemConfig, which calls
// this after decoding.
func (c SystemConfig) validate() error {
	switch c.LogLevel {
	case 0, 1, 3, 5:
	default:
		return fmt.Errorf("LogLevel must be one of 0, 1, 3, or 5, got %d", c.LogLevel)
	}

	if c.LoginMaxAttempts < 1 {
		return fmt.Errorf("LoginMaxAttempts must be a positive integer, got %d", c.LoginMaxAttempts)
	}

	if c.TOTPMaxAttempts < 1 {
		return fmt.Errorf("TOTPMaxAttempts must be a positive integer, got %d", c.TOTPMaxAttempts)
	}

	if c.PasswordMinLength < 1 {
		return fmt.Errorf("PasswordMinLength must be a positive integer, got %d", c.PasswordMinLength)
	}

	if c.PasswordChangeMaxAttempts < 1 {
		return fmt.Errorf("PasswordChangeMaxAttempts must be a positive integer, got %d", c.PasswordChangeMaxAttempts)
	}

	// MergeCommonCommands true means merge every common command into its
	// normal alphabetical position, which only means anything once
	// AlphabeticalCommandOrder has produced one alphabetical order to merge
	// into.
	//
	// With AlphabeticalCommandOrder false there is no single combined order
	// across a level's tree file and CommonTreeFile, so this combination is
	// rejected rather than silently falling back to appended order.
	if !c.AlphabeticalCommandOrder && c.MergeCommonCommands {
		return errors.New("MergeCommonCommands cannot be true while AlphabeticalCommandOrder is false, there is no single combined definition order to merge common commands into")
	}

	if c.DefaultPageLines < 1 {
		return fmt.Errorf("DefaultPageLines must be a positive integer, got %d", c.DefaultPageLines)
	}

	if c.DefaultHistorySize < 0 {
		return fmt.Errorf("DefaultHistorySize must be zero or positive, got %d", c.DefaultHistorySize)
	}

	switch c.FilterMatchMode {
	case "substring", "regex":
	default:
		return fmt.Errorf("FilterMatchMode must be 'substring' or 'regex', got %q", c.FilterMatchMode)
	}

	if c.MaxFilterChainDepth < 0 {
		return fmt.Errorf("MaxFilterChainDepth must be zero or positive, got %d", c.MaxFilterChainDepth)
	}

	if c.SessionIdleTimeout < 0 {
		return fmt.Errorf("SessionIdleTimeout must be zero or positive, got %s", c.SessionIdleTimeout)
	}

	if c.ElevationTimeout < 0 {
		return fmt.Errorf("ElevationTimeout must be zero or positive, got %s", c.ElevationTimeout)
	}

	if c.ReauthGracePeriod < 0 {
		return fmt.Errorf("ReauthGracePeriod must be zero or positive, got %s", c.ReauthGracePeriod)
	}

	if c.AuthBypassTrustWindow < 0 {
		return fmt.Errorf("AuthBypassTrustWindow must be zero or positive, got %s", c.AuthBypassTrustWindow)
	}

	// AttemptWindow and LockoutDuration are only meaningful as a pair.
	// Setting one without the other produces a configuration that looks
	// like it opted in to rate limiting but does nothing.
	//
	// A nonzero window with a zero lockout expires every lockout instantly.
	// A nonzero lockout with a zero window is never triggered at all.
	//
	// So the half configured state is rejected at startup rather than
	// silently doing nothing.
	if err := validateAttemptWindowPair("Login", c.LoginAttemptWindow, c.LoginLockoutDuration); err != nil {
		return err
	}
	if err := validateAttemptWindowPair("CommandLevel", c.CommandLevelAttemptWindow, c.CommandLevelLockoutDuration); err != nil {
		return err
	}
	if err := validateAttemptWindowPair("CommandPassword", c.CommandPasswordAttemptWindow, c.CommandPasswordLockoutDuration); err != nil {
		return err
	}

	if err := c.validateAuthSources(); err != nil {
		return err
	}

	return nil
}

// validateAuthSources - This method checks the authentication settings: an
// interactive login, a trusted host identity, or both, plus an optional
// TOTP step up on either.
//
// A bad combination is rejected here so it fails at startup rather than in
// a more confusing way once the program is running.
func (c SystemConfig) validateAuthSources() error {
	if c.AuthRequired && !c.EnableHostAuthentication && !c.EnableCLIAuthentication {
		return errors.New("AuthRequired is true but neither EnableHostAuthentication nor EnableCLIAuthentication is set, so there is no way to establish a session's identity")
	}

	if c.EnableTOTPAuthentication && !c.EnableHostAuthentication && !c.EnableCLIAuthentication {
		return errors.New("EnableTOTPAuthentication cannot stand alone, it requires EnableHostAuthentication or EnableCLIAuthentication to also be true")
	}

	seenNames := make(map[string]bool, len(c.AuthProviders))
	for _, p := range c.AuthProviders {
		if p.Name == "" {
			return errors.New("AuthProviders entry has an empty name")
		}
		if p.Type == "" {
			return fmt.Errorf("AuthProviders entry %q has an empty type", p.Name)
		}
		if seenNames[p.Name] {
			return fmt.Errorf("AuthProviders has more than one entry named %q", p.Name)
		}
		seenNames[p.Name] = true
	}

	if c.EnableCLIAuthentication {
		if c.CLIAuthProvider == "" {
			return errors.New("EnableCLIAuthentication is true but CLIAuthProvider is empty")
		}
		if !seenNames[c.CLIAuthProvider] {
			return fmt.Errorf("CLIAuthProvider %q does not match any entry in AuthProviders", c.CLIAuthProvider)
		}
	}

	return nil
}

// validateAttemptWindowPair - This function enforces that an AttemptWindow
// and LockoutDuration pair is either both zero, meaning rate limiting for
// that area is not opted in to the windowed behavior at all, or both
// nonzero, meaning fully configured. See validate's comment for why a
// half-configured pair is rejected rather than silently doing nothing.
func validateAttemptWindowPair(prefix string, window, lockout Duration) error {
	if (window == 0) != (lockout == 0) {
		return fmt.Errorf("%sAttemptWindow and %sLockoutDuration must either both be set or both be left at zero (got window=%s, lockout=%s)", prefix, prefix, window, lockout)
	}
	return nil
}
