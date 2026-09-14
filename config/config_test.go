// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func tempDirFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	return path
}

// TestLoadSystemConfigValid - This test verifies that a config file
// setting every field loads into a SystemConfig that matches exactly,
// field for field.
func TestLoadSystemConfigValid(t *testing.T) {
	content := "# a comment\n\n" +
		"PreventEscape: true\n" +
		"LogLevel: 3\n" +
		"HistoryFile: /tmp/hist\n" +
		"AuditLogFile: /tmp/audit.log\n" +
		"AuditLogEnabled: true\n" +
		"CurrentLanguage: fr\n" +
		"DefaultLanguage: en\n" +
		"LanguageDir: custom-lang\n" +
		"TreeStructure: custom-tree-structure.yaml\n" +
		"CommonTreeFile: custom-common.yaml\n" +
		"AuthRequired: true\n" +
		"UsersFile: custom-users.yaml\n" +
		"LoginMaxAttempts: 5\n" +
		"TOTPIssuer: MyCompany\n" +
		"TOTPMaxAttempts: 4\n" +
		"SessionIdleTimeout: 10m\n" +
		"ElevationTimeout: 30s\n"

	path := tempDirFile(t, "routercli.yaml", content)

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
	}

	want := SystemConfig{
		ProductName:        "RouterCLI",
		PreventEscape:      true,
		LogLevel:           3,
		HistoryFile:        "/tmp/hist",
		AuditLogFile:       "/tmp/audit.log",
		AuditLogEnabled:    true,
		CurrentLanguage:    "fr",
		DefaultLanguage:    "en",
		LanguageDir:        "custom-lang",
		SessionIdleTimeout: Duration(10 * time.Minute),
		ElevationTimeout:   Duration(30 * time.Second),
		TreeStructure:      "custom-tree-structure.yaml",
		CommonTreeFile:     "custom-common.yaml",
		StartupConfigFile:  "",
		VendorConfigFile:   "",
		AuthRequired:       true,
		UsersFile:          "custom-users.yaml",
		LoginMaxAttempts:   5,
		TOTPIssuer:         "MyCompany",
		TOTPMaxAttempts:    4,

		PasswordMinLength:         10,
		PasswordChangeMaxAttempts: 3,

		EnableCLIAuthentication:  true,
		EnableTOTPAuthentication: true,
		AuthProviders:            []AuthProviderConfig{{Name: "local", Type: "local"}},
		CLIAuthProvider:          "local",

		AlphabeticalCommandOrder: true,
		MergeCommonCommands:      true,

		PagingEnabled:       true,
		DefaultPageLines:    24,
		DefaultHistorySize:  500,
		FilterMatchMode:     "substring",
		MaxFilterChainDepth: 2,

		AuthBypassTrustWindow: Duration(5 * time.Minute),

		RolesFile:   "",
		DefaultsDir: "",
	}

	if !systemConfigsEqual(cfg, want) {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

// systemConfigsEqual - This function compares two SystemConfig values
// field for field, standing in for a plain == comparison now that
// AuthProviders is a slice. A struct with a slice field is not comparable
// with == at all in Go, a compile error rather than a runtime one,
// regardless of whether that particular slice happens to be nil.
// reflect.DeepEqual is the standard way around that, and treats two
// nil-vs-empty AuthProviders slices as equal, matching what every test in
// this file comparing "the zero value never set" against "explicitly set
// to empty" already expects.
func systemConfigsEqual(a, b SystemConfig) bool {
	return reflect.DeepEqual(a, b)
}

// TestLoadSystemConfigUnknownKey - This test verifies that a config file
// containing a key that does not match any SystemConfig field is rejected,
// rather than silently ignored, thanks to strict unknown key parsing.
func TestLoadSystemConfigUnknownKey(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "NotReal: foo\n")

	if _, err := LoadSystemConfig(path); err == nil {
		t.Fatal("expected an error for an unknown YAML key, got nil")
	}
}

// TestLoadSystemConfigInvalidYAML - This test verifies that a config file
// with YAML that does not parse at all, not merely a value that fails
// validation, is rejected.
func TestLoadSystemConfigInvalidYAML(t *testing.T) {
	cases := []string{
		"JustOneWordNoValue\n",
		"HistoryFile: [unclosed\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for invalid YAML %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidBoolean - This test verifies that a value
// that does not parse as YAML's boolean form, for a bool field, is
// rejected.
func TestLoadSystemConfigInvalidBoolean(t *testing.T) {
	cases := []string{
		"AuditLogEnabled: maybe\n",
		"PreventEscape: maybe\n",
		"AuthRequired: nottrue\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for invalid boolean value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidLogLevel - This test verifies that a LogLevel
// value that does not even parse as an integer, or a negative one, is
// rejected.
func TestLoadSystemConfigInvalidLogLevel(t *testing.T) {
	cases := []string{
		"LogLevel: abc\n",
		"LogLevel: -1\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for LogLevel value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidLoginMaxAttempts - This test verifies that a
// LoginMaxAttempts value that does not parse as an integer, is zero, or is
// negative is rejected, since zero or fewer attempts would leave login
// unusable rather than merely unlimited.
func TestLoadSystemConfigInvalidLoginMaxAttempts(t *testing.T) {
	cases := []string{
		"LoginMaxAttempts: abc\n",
		"LoginMaxAttempts: 0\n",
		"LoginMaxAttempts: -1\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for LoginMaxAttempts value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidTOTPMaxAttempts - This test verifies that a
// TOTPMaxAttempts value that does not parse as an integer, is zero, or is
// negative is rejected, the same validation rule LoginMaxAttempts already
// uses, since zero or fewer attempts would leave totp enable and totp
// disable unable to offer even a single retry.
func TestLoadSystemConfigInvalidTOTPMaxAttempts(t *testing.T) {
	cases := []string{
		"TOTPMaxAttempts: abc\n",
		"TOTPMaxAttempts: 0\n",
		"TOTPMaxAttempts: -1\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for TOTPMaxAttempts value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidPasswordMinLength - This test verifies that a
// PasswordMinLength value that does not parse as an integer, is zero, or
// is negative is rejected, the same validation rule LoginMaxAttempts and
// TOTPMaxAttempts already use.
func TestLoadSystemConfigInvalidPasswordMinLength(t *testing.T) {
	cases := []string{
		"PasswordMinLength: abc\n",
		"PasswordMinLength: 0\n",
		"PasswordMinLength: -1\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for PasswordMinLength value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInvalidPasswordChangeMaxAttempts - This test
// verifies that a PasswordChangeMaxAttempts value that does not parse as
// an integer, is zero, or is negative is rejected, the same validation
// rule LoginMaxAttempts and TOTPMaxAttempts already use, since zero or
// fewer attempts would leave the password change command unable to offer
// even a single retry.
func TestLoadSystemConfigInvalidPasswordChangeMaxAttempts(t *testing.T) {
	cases := []string{
		"PasswordChangeMaxAttempts: abc\n",
		"PasswordChangeMaxAttempts: 0\n",
		"PasswordChangeMaxAttempts: -1\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for PasswordChangeMaxAttempts value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigPasswordComplexityFlags - This test verifies that
// all three PasswordRequire* composition flags parse independently, each
// true without the others being pulled along by accident.
func TestLoadSystemConfigPasswordComplexityFlags(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    SystemConfig
	}{
		{
			name:    "uppercase only",
			content: "PasswordRequireUppercase: true\n",
			want:    SystemConfig{PasswordRequireUppercase: true},
		},
		{
			name:    "numbers only",
			content: "PasswordRequireNumbers: true\n",
			want:    SystemConfig{PasswordRequireNumbers: true},
		},
		{
			name:    "special chars only",
			content: "PasswordRequireSpecialChars: true\n",
			want:    SystemConfig{PasswordRequireSpecialChars: true},
		},
		{
			name:    "all three together",
			content: "PasswordRequireUppercase: true\nPasswordRequireNumbers: true\nPasswordRequireSpecialChars: true\n",
			want:    SystemConfig{PasswordRequireUppercase: true, PasswordRequireNumbers: true, PasswordRequireSpecialChars: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tempDirFile(t, "routercli.yaml", tc.content)

			cfg, err := LoadSystemConfig(path)
			if err != nil {
				t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
			}
			if cfg.PasswordRequireUppercase != tc.want.PasswordRequireUppercase {
				t.Errorf("PasswordRequireUppercase = %v, want %v", cfg.PasswordRequireUppercase, tc.want.PasswordRequireUppercase)
			}
			if cfg.PasswordRequireNumbers != tc.want.PasswordRequireNumbers {
				t.Errorf("PasswordRequireNumbers = %v, want %v", cfg.PasswordRequireNumbers, tc.want.PasswordRequireNumbers)
			}
			if cfg.PasswordRequireSpecialChars != tc.want.PasswordRequireSpecialChars {
				t.Errorf("PasswordRequireSpecialChars = %v, want %v", cfg.PasswordRequireSpecialChars, tc.want.PasswordRequireSpecialChars)
			}
		})
	}
}

// TestLoadSystemConfigAuthProvidersParsesFromYAML - This test verifies
// that an AuthProviders list, and CLIAuthProvider naming one of its
// entries, load from YAML correctly, replacing the single default "local"
// entry entirely rather than being appended to it, the same
// whole-value-replaces-default behavior every other field in this file
// already has.
func TestLoadSystemConfigAuthProvidersParsesFromYAML(t *testing.T) {
	content := "AuthProviders:\n" +
		"  - name: local\n" +
		"    type: local\n" +
		"  - name: corp-ldap\n" +
		"    type: ldap\n" +
		"CLIAuthProvider: corp-ldap\n"

	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
	}

	want := []AuthProviderConfig{{Name: "local", Type: "local"}, {Name: "corp-ldap", Type: "ldap"}}
	if len(cfg.AuthProviders) != len(want) {
		t.Fatalf("AuthProviders = %+v, want %+v", cfg.AuthProviders, want)
	}
	for i := range want {
		if cfg.AuthProviders[i] != want[i] {
			t.Errorf("AuthProviders[%d] = %+v, want %+v", i, cfg.AuthProviders[i], want[i])
		}
	}

	if cfg.CLIAuthProvider != "corp-ldap" {
		t.Errorf("CLIAuthProvider = %q, want %q", cfg.CLIAuthProvider, "corp-ldap")
	}
}

// TestLoadSystemConfigCLIAuthProviderUnknownNameIsError - This test
// verifies that LoadSystemConfig, not only validate in isolation, rejects
// a CLIAuthProvider that does not match any AuthProviders entry, since a
// typo here would otherwise silently authenticate against the wrong
// backend, or none at all, rather than failing loudly at startup.
func TestLoadSystemConfigCLIAuthProviderUnknownNameIsError(t *testing.T) {
	content := "CLIAuthProvider: does-not-exist\n"
	path := tempDirFile(t, "routercli.yaml", content)

	if _, err := LoadSystemConfig(path); err == nil {
		t.Fatal("expected an error for a CLIAuthProvider naming an unknown AuthProviders entry, got nil")
	}
}

// TestLoadSystemConfigValidTimeouts - This test verifies that a
// well-formed duration string for both timeout fields parses into the
// expected Duration.
func TestLoadSystemConfigValidTimeouts(t *testing.T) {
	content := "SessionIdleTimeout: 1h30m\n" +
		"ElevationTimeout: 10s\n"

	path := tempDirFile(t, "routercli.yaml", content)

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
	}

	if cfg.SessionIdleTimeout != Duration(90*time.Minute) {
		t.Errorf("SessionIdleTimeout = %v, want 90m", cfg.SessionIdleTimeout)
	}

	if cfg.ElevationTimeout != Duration(10*time.Second) {
		t.Errorf("ElevationTimeout = %v, want 10s", cfg.ElevationTimeout)
	}
}

// TestLoadSystemConfigDurationZeroVariants - This test verifies that every
// way YAML can spell zero or nothing at all for a duration field, a
// missing value, null, a tilde, an empty string, or an explicit "0"/"0s",
// all resolve to a zero Duration rather than an error.
func TestLoadSystemConfigDurationZeroVariants(t *testing.T) {
	cases := []string{
		"SessionIdleTimeout: 0\n",
		"SessionIdleTimeout: 0s\n",
		"SessionIdleTimeout:\n",
		"SessionIdleTimeout: null\n",
		"SessionIdleTimeout: ~\n",
		"SessionIdleTimeout: \"\"\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		cfg, err := LoadSystemConfig(path)
		if err != nil {
			t.Fatalf("LoadSystemConfig(%q) returned unexpected error: %v", content, err)
		}

		if cfg.SessionIdleTimeout != 0 {
			t.Errorf("SessionIdleTimeout = %v for content %q, want 0", cfg.SessionIdleTimeout, content)
		}
	}
}

// TestLoadSystemConfigInvalidTimeouts - This test verifies that a duration
// field given a value that does not parse as a duration, a negative value,
// a bare number with no unit, or the wrong YAML shape entirely is
// rejected.
func TestLoadSystemConfigInvalidTimeouts(t *testing.T) {
	cases := []string{
		"SessionIdleTimeout: notaduration\n",
		"SessionIdleTimeout: 5\n",
		"SessionIdleTimeout: -5m\n",
		"ElevationTimeout: 10\n",
		"ElevationTimeout: -1s\n",
		"SessionIdleTimeout: {}\n",
		"SessionIdleTimeout: []\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for timeout value in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigMultipleDocuments - This test verifies that a config
// file containing more than one "---" separated YAML document is rejected,
// rather than silently loading only the first one.
func TestLoadSystemConfigMultipleDocuments(t *testing.T) {
	content := "HistoryFile: /tmp/hist\n" +
		"---\n" +
		"HistoryFile: /tmp/other\n"

	path := tempDirFile(t, "routercli.yaml", content)

	if _, err := LoadSystemConfig(path); err == nil {
		t.Fatal("expected an error for multiple YAML documents, got nil")
	}
}

// TestLoadSystemConfigMissingFileIsAnError - This test verifies that
// LoadSystemConfig refuses a path with no file on disk. An operator who
// names a file explicitly, through --config, must be told the name is
// wrong rather than silently started on defaults.
func TestLoadSystemConfigMissingFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	if _, err := LoadSystemConfig(path); err == nil {
		t.Fatal("expected an error for a missing configuration file, got nil")
	}
}

// TestLoadSystemConfigOrDefaultsMissingFileUsesDefaults - This test
// verifies that the lenient variant still falls back to
// DefaultSystemConfig for a path with no file on disk, since a project
// need not ship a configuration file at all.
func TestLoadSystemConfigOrDefaultsMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	cfg, err := LoadSystemConfigOrDefaults(path)
	if err != nil {
		t.Fatalf("a missing config file should fall back to defaults, not error: %v", err)
	}

	want := DefaultSystemConfig()
	if !systemConfigsEqual(cfg, want) {
		t.Errorf("cfg = %+v, want default %+v", cfg, want)
	}
}

// TestLoadSystemConfigOrDefaultsStillRejectsAnInvalidFile - This test
// verifies that the lenient variant only forgives a file that is absent. A
// file that exists but does not parse is still an error.
func TestLoadSystemConfigOrDefaultsStillRejectsAnInvalidFile(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "NotARealProperty: 1\n")

	if _, err := LoadSystemConfigOrDefaults(path); err == nil {
		t.Fatal("expected an error for an unknown property, got nil")
	}
}

// TestLoadSystemConfigEmptyPathUsesDefaults - This test verifies that an
// empty path string, distinct from a missing file at a real path, also
// falls back to DefaultSystemConfig.
func TestLoadSystemConfigEmptyPathUsesDefaults(t *testing.T) {
	cfg, err := LoadSystemConfigOrDefaults("")
	if err != nil {
		t.Fatalf("an empty path should fall back to defaults, not error: %v", err)
	}

	want := DefaultSystemConfig()
	if !systemConfigsEqual(cfg, want) {
		t.Errorf("cfg = %+v, want default %+v", cfg, want)
	}
}

// TestLoadSystemConfigEmptyFileUsesDefaults - This test verifies that a
// config file that exists but is completely empty falls back to
// DefaultSystemConfig rather than erroring.
func TestLoadSystemConfigEmptyFileUsesDefaults(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "")

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("an empty config file should fall back to defaults, not error: %v", err)
	}

	want := DefaultSystemConfig()
	if !systemConfigsEqual(cfg, want) {
		t.Errorf("cfg = %+v, want default %+v", cfg, want)
	}
}

// TestLoadSystemConfigCommentsOnlyUsesDefaults - This test verifies that a
// config file containing only a comment, with no actual YAML content, also
// falls back to DefaultSystemConfig.
func TestLoadSystemConfigCommentsOnlyUsesDefaults(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "# only a comment\n")

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("a comments-only config file should fall back to defaults, not error: %v", err)
	}

	want := DefaultSystemConfig()
	if !systemConfigsEqual(cfg, want) {
		t.Errorf("cfg = %+v, want default %+v", cfg, want)
	}
}

// TestLoadSystemConfigPartialConfigPreservesDefaults - This test verifies
// that a config file setting only one field leaves every other field at
// its default value, rather than zeroing them out.
func TestLoadSystemConfigPartialConfigPreservesDefaults(t *testing.T) {
	content := "HistoryFile: /custom/hist\n"
	path := tempDirFile(t, "routercli.yaml", content)

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
	}

	if cfg.HistoryFile != "/custom/hist" {
		t.Errorf("HistoryFile = %q, want %q", cfg.HistoryFile, "/custom/hist")
	}

	if cfg.TreeStructure != "" {
		t.Errorf("TreeStructure = %q, want default %q", cfg.TreeStructure, "")
	}

	if cfg.LoginMaxAttempts != 3 {
		t.Errorf("LoginMaxAttempts = %d, want default %d", cfg.LoginMaxAttempts, 3)
	}

	if cfg.SessionIdleTimeout != 0 {
		t.Errorf("SessionIdleTimeout = %v, want default 0", cfg.SessionIdleTimeout)
	}

	if cfg.ElevationTimeout != 0 {
		t.Errorf("ElevationTimeout = %v, want default 0", cfg.ElevationTimeout)
	}
}

// TestDefaultSystemConfigValues - This test verifies every individual
// field DefaultSystemConfig sets, pinning down the actual shipped default
// for each one rather than only checking the struct as a whole.
func TestDefaultSystemConfigValues(t *testing.T) {
	cfg := DefaultSystemConfig()

	if cfg.PreventEscape {
		t.Error("PreventEscape default = true, want false")
	}

	if cfg.LogLevel != 0 {
		t.Errorf("LogLevel default = %d, want 0", cfg.LogLevel)
	}

	if cfg.HistoryFile != "" {
		t.Errorf("HistoryFile default = %q, want %q", cfg.HistoryFile, "")
	}

	if cfg.AuditLogFile != "" {
		t.Errorf("AuditLogFile default = %q, want %q", cfg.AuditLogFile, "")
	}

	if cfg.AuditLogEnabled {
		t.Error("AuditLogEnabled default = true, want false")
	}

	if cfg.CurrentLanguage != "en" {
		t.Errorf("CurrentLanguage default = %q, want %q", cfg.CurrentLanguage, "en")
	}

	if cfg.DefaultLanguage != "en" {
		t.Errorf("DefaultLanguage default = %q, want %q", cfg.DefaultLanguage, "en")
	}

	if cfg.LanguageDir != "" {
		t.Errorf("LanguageDir default = %q, want %q", cfg.LanguageDir, "")
	}

	if cfg.SessionIdleTimeout != 0 {
		t.Errorf("SessionIdleTimeout default = %v, want 0", cfg.SessionIdleTimeout)
	}

	if cfg.ElevationTimeout != 0 {
		t.Errorf("ElevationTimeout default = %v, want 0", cfg.ElevationTimeout)
	}

	if cfg.AuthBypassTrustWindow != Duration(5*time.Minute) {
		t.Errorf("AuthBypassTrustWindow default = %v, want 5m", cfg.AuthBypassTrustWindow)
	}

	if cfg.TreeStructure != "" {
		t.Errorf("TreeStructure default = %q, want %q", cfg.TreeStructure, "")
	}

	if cfg.CommonTreeFile != "" {
		t.Errorf("CommonTreeFile default = %q, want %q", cfg.CommonTreeFile, "")
	}

	if cfg.StartupConfigFile != "" {
		t.Errorf("StartupConfigFile default = %q, want %q", cfg.StartupConfigFile, "")
	}

	if cfg.VendorConfigFile != "" {
		t.Errorf("VendorConfigFile default = %q, want %q", cfg.VendorConfigFile, "")
	}

	if cfg.AuthRequired {
		t.Error("AuthRequired default = true, want false")
	}

	if cfg.UsersFile != "" {
		t.Errorf("UsersFile default = %q, want %q", cfg.UsersFile, "")
	}

	if cfg.LoginMaxAttempts != 3 {
		t.Errorf("LoginMaxAttempts default = %d, want 3", cfg.LoginMaxAttempts)
	}

	if cfg.ProductName != "RouterCLI" {
		t.Errorf("ProductName default = %q, want %q", cfg.ProductName, "RouterCLI")
	}

	if cfg.TOTPIssuer != "RouterCLI" {
		t.Errorf("TOTPIssuer default = %q, want %q", cfg.TOTPIssuer, "RouterCLI")
	}

	if cfg.TOTPMaxAttempts != 3 {
		t.Errorf("TOTPMaxAttempts default = %d, want 3", cfg.TOTPMaxAttempts)
	}

	if cfg.PasswordMinLength != 10 {
		t.Errorf("PasswordMinLength default = %d, want 10", cfg.PasswordMinLength)
	}

	if cfg.PasswordRequireUppercase {
		t.Error("PasswordRequireUppercase default = true, want false")
	}

	if cfg.PasswordRequireNumbers {
		t.Error("PasswordRequireNumbers default = true, want false")
	}

	if cfg.PasswordRequireSpecialChars {
		t.Error("PasswordRequireSpecialChars default = true, want false")
	}

	if cfg.PasswordChangeMaxAttempts != 3 {
		t.Errorf("PasswordChangeMaxAttempts default = %d, want 3", cfg.PasswordChangeMaxAttempts)
	}

	if cfg.EnableHostAuthentication {
		t.Error("EnableHostAuthentication default = true, want false")
	}

	if !cfg.EnableCLIAuthentication {
		t.Error("EnableCLIAuthentication default = false, want true")
	}

	if !cfg.EnableTOTPAuthentication {
		t.Error("EnableTOTPAuthentication default = false, want true (matches the per-user TOTPSecret check that already existed before this setting did)")
	}

	if want := []AuthProviderConfig{{Name: "local", Type: "local"}}; len(cfg.AuthProviders) != len(want) || cfg.AuthProviders[0] != want[0] {
		t.Errorf("AuthProviders default = %+v, want %+v", cfg.AuthProviders, want)
	}

	if cfg.CLIAuthProvider != "local" {
		t.Errorf("CLIAuthProvider default = %q, want %q", cfg.CLIAuthProvider, "local")
	}

	if !cfg.AlphabeticalCommandOrder {
		t.Error("AlphabeticalCommandOrder default = false, want true (matches real Cisco and HP)")
	}

	if !cfg.MergeCommonCommands {
		t.Error("MergeCommonCommands default = false, want true (matches real Cisco and HP)")
	}

	if !cfg.PagingEnabled {
		t.Error("PagingEnabled default = false, want true (matches real Cisco and HP)")
	}

	if cfg.DefaultPageLines != 24 {
		t.Errorf("DefaultPageLines default = %d, want 24", cfg.DefaultPageLines)
	}

	if cfg.DefaultHistorySize != 500 {
		t.Errorf("DefaultHistorySize default = %d, want 500", cfg.DefaultHistorySize)
	}

	if cfg.FilterMatchMode != "substring" {
		t.Errorf("FilterMatchMode default = %q, want %q", cfg.FilterMatchMode, "substring")
	}

	if cfg.MaxFilterChainDepth != 2 {
		t.Errorf("MaxFilterChainDepth default = %d, want 2", cfg.MaxFilterChainDepth)
	}
}

// TestDefaultSystemConfigIsValid - This test verifies that
// DefaultSystemConfig's output always passes validate, since a project
// that changes nothing must always start successfully.
func TestDefaultSystemConfigIsValid(t *testing.T) {
	if err := DefaultSystemConfig().validate(); err != nil {
		t.Fatalf("DefaultSystemConfig().validate() = %v, want nil", err)
	}
}

// TestSystemConfigValidate - This test table drives validate across
// LogLevel, LoginMaxAttempts, TOTPMaxAttempts, PasswordMinLength,
// PasswordChangeMaxAttempts, SessionIdleTimeout, ElevationTimeout,
// ReauthGracePeriod, AuthBypassTrustWindow, DefaultPageLines,
// DefaultHistorySize, FilterMatchMode, and MaxFilterChainDepth, covering
// the boundary between an accepted value and a rejected one for each
// field.
func TestSystemConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*SystemConfig)
		wantErr bool
	}{
		{
			name: "valid zero log level",
			mutate: func(c *SystemConfig) {
				c.LogLevel = 0
			},
			wantErr: false,
		},
		{
			name: "valid documented log level 5",
			mutate: func(c *SystemConfig) {
				c.LogLevel = 5
			},
			wantErr: false,
		},
		{
			name: "negative log level",
			mutate: func(c *SystemConfig) {
				c.LogLevel = -1
			},
			wantErr: true,
		},
		{
			name: "valid login max attempts one",
			mutate: func(c *SystemConfig) {
				c.LoginMaxAttempts = 1
			},
			wantErr: false,
		},
		{
			name: "valid login max attempts ten",
			mutate: func(c *SystemConfig) {
				c.LoginMaxAttempts = 10
			},
			wantErr: false,
		},
		{
			name: "zero login max attempts",
			mutate: func(c *SystemConfig) {
				c.LoginMaxAttempts = 0
			},
			wantErr: true,
		},
		{
			name: "negative login max attempts",
			mutate: func(c *SystemConfig) {
				c.LoginMaxAttempts = -1
			},
			wantErr: true,
		},
		{
			name: "valid totp max attempts one",
			mutate: func(c *SystemConfig) {
				c.TOTPMaxAttempts = 1
			},
			wantErr: false,
		},
		{
			name: "valid totp max attempts ten",
			mutate: func(c *SystemConfig) {
				c.TOTPMaxAttempts = 10
			},
			wantErr: false,
		},
		{
			name: "zero totp max attempts",
			mutate: func(c *SystemConfig) {
				c.TOTPMaxAttempts = 0
			},
			wantErr: true,
		},
		{
			name: "negative totp max attempts",
			mutate: func(c *SystemConfig) {
				c.TOTPMaxAttempts = -1
			},
			wantErr: true,
		},
		{
			name: "valid password min length one",
			mutate: func(c *SystemConfig) {
				c.PasswordMinLength = 1
			},
			wantErr: false,
		},
		{
			name: "valid password min length twenty",
			mutate: func(c *SystemConfig) {
				c.PasswordMinLength = 20
			},
			wantErr: false,
		},
		{
			name: "zero password min length",
			mutate: func(c *SystemConfig) {
				c.PasswordMinLength = 0
			},
			wantErr: true,
		},
		{
			name: "negative password min length",
			mutate: func(c *SystemConfig) {
				c.PasswordMinLength = -1
			},
			wantErr: true,
		},
		{
			name: "valid password change max attempts one",
			mutate: func(c *SystemConfig) {
				c.PasswordChangeMaxAttempts = 1
			},
			wantErr: false,
		},
		{
			name: "valid password change max attempts ten",
			mutate: func(c *SystemConfig) {
				c.PasswordChangeMaxAttempts = 10
			},
			wantErr: false,
		},
		{
			name: "zero password change max attempts",
			mutate: func(c *SystemConfig) {
				c.PasswordChangeMaxAttempts = 0
			},
			wantErr: true,
		},
		{
			name: "negative password change max attempts",
			mutate: func(c *SystemConfig) {
				c.PasswordChangeMaxAttempts = -1
			},
			wantErr: true,
		},
		{
			name: "valid zero session idle timeout",
			mutate: func(c *SystemConfig) {
				c.SessionIdleTimeout = 0
			},
			wantErr: false,
		},
		{
			name: "valid positive session idle timeout",
			mutate: func(c *SystemConfig) {
				c.SessionIdleTimeout = Duration(10 * time.Minute)
			},
			wantErr: false,
		},
		{
			name: "negative session idle timeout",
			mutate: func(c *SystemConfig) {
				c.SessionIdleTimeout = Duration(-time.Second)
			},
			wantErr: true,
		},
		{
			name: "valid zero elevation timeout",
			mutate: func(c *SystemConfig) {
				c.ElevationTimeout = 0
			},
			wantErr: false,
		},
		{
			name: "valid positive elevation timeout",
			mutate: func(c *SystemConfig) {
				c.ElevationTimeout = Duration(5 * time.Second)
			},
			wantErr: false,
		},
		{
			name: "negative elevation timeout",
			mutate: func(c *SystemConfig) {
				c.ElevationTimeout = Duration(-time.Second)
			},
			wantErr: true,
		},
		{
			name: "valid zero reauth grace period",
			mutate: func(c *SystemConfig) {
				c.ReauthGracePeriod = 0
			},
			wantErr: false,
		},
		{
			name: "valid positive reauth grace period",
			mutate: func(c *SystemConfig) {
				c.ReauthGracePeriod = Duration(time.Minute)
			},
			wantErr: false,
		},
		{
			name: "negative reauth grace period",
			mutate: func(c *SystemConfig) {
				c.ReauthGracePeriod = Duration(-time.Second)
			},
			wantErr: true,
		},
		{
			name: "valid zero auth bypass trust window",
			mutate: func(c *SystemConfig) {
				c.AuthBypassTrustWindow = 0
			},
			wantErr: false,
		},
		{
			name: "valid positive auth bypass trust window",
			mutate: func(c *SystemConfig) {
				c.AuthBypassTrustWindow = Duration(5 * time.Minute)
			},
			wantErr: false,
		},
		{
			name: "negative auth bypass trust window",
			mutate: func(c *SystemConfig) {
				c.AuthBypassTrustWindow = Duration(-time.Second)
			},
			wantErr: true,
		},
		{
			name: "AuthRequired with neither host nor CLI authentication enabled",
			mutate: func(c *SystemConfig) {
				c.AuthRequired = true
				c.EnableHostAuthentication = false
				c.EnableCLIAuthentication = false
			},
			wantErr: true,
		},
		{
			name: "AuthRequired with only host authentication enabled",
			mutate: func(c *SystemConfig) {
				c.AuthRequired = true
				c.EnableHostAuthentication = true
				c.EnableCLIAuthentication = false
			},
			wantErr: false,
		},
		{
			name: "TOTP authentication with neither host nor CLI authentication enabled",
			mutate: func(c *SystemConfig) {
				c.EnableTOTPAuthentication = true
				c.EnableHostAuthentication = false
				c.EnableCLIAuthentication = false
			},
			wantErr: true,
		},
		{
			name: "TOTP authentication alongside host authentication only",
			mutate: func(c *SystemConfig) {
				c.EnableTOTPAuthentication = true
				c.EnableHostAuthentication = true
				c.EnableCLIAuthentication = false
			},
			wantErr: false,
		},
		{
			name: "CLI authentication enabled with an empty CLIAuthProvider",
			mutate: func(c *SystemConfig) {
				c.EnableCLIAuthentication = true
				c.CLIAuthProvider = ""
			},
			wantErr: true,
		},
		{
			name: "CLIAuthProvider naming a provider that does not exist",
			mutate: func(c *SystemConfig) {
				c.EnableCLIAuthentication = true
				c.CLIAuthProvider = "does-not-exist"
			},
			wantErr: true,
		},
		{
			name: "AuthProviders entry with an empty name",
			mutate: func(c *SystemConfig) {
				c.AuthProviders = []AuthProviderConfig{{Name: "", Type: "local"}}
			},
			wantErr: true,
		},
		{
			name: "AuthProviders entry with an empty type",
			mutate: func(c *SystemConfig) {
				c.AuthProviders = []AuthProviderConfig{{Name: "local", Type: ""}}
			},
			wantErr: true,
		},
		{
			name: "AuthProviders with two entries sharing the same name",
			mutate: func(c *SystemConfig) {
				c.AuthProviders = []AuthProviderConfig{
					{Name: "local", Type: "local"},
					{Name: "local", Type: "ldap"},
				}
			},
			wantErr: true,
		},
		{
			name: "AlphabeticalCommandOrder true, MergeCommonCommands true, everything merged",
			mutate: func(c *SystemConfig) {
				c.AlphabeticalCommandOrder = true
				c.MergeCommonCommands = true
			},
			wantErr: false,
		},
		{
			name: "AlphabeticalCommandOrder true, MergeCommonCommands false, common commands appended",
			mutate: func(c *SystemConfig) {
				c.AlphabeticalCommandOrder = true
				c.MergeCommonCommands = false
			},
			wantErr: false,
		},
		{
			name: "AlphabeticalCommandOrder false, MergeCommonCommands false, both in tree file order",
			mutate: func(c *SystemConfig) {
				c.AlphabeticalCommandOrder = false
				c.MergeCommonCommands = false
			},
			wantErr: false,
		},
		{
			name: "AlphabeticalCommandOrder false, MergeCommonCommands true, nothing to merge into",
			mutate: func(c *SystemConfig) {
				c.AlphabeticalCommandOrder = false
				c.MergeCommonCommands = true
			},
			wantErr: true,
		},
		{
			name: "valid default page lines one",
			mutate: func(c *SystemConfig) {
				c.DefaultPageLines = 1
			},
			wantErr: false,
		},
		{
			name: "zero default page lines",
			mutate: func(c *SystemConfig) {
				c.DefaultPageLines = 0
			},
			wantErr: true,
		},
		{
			name: "negative default page lines",
			mutate: func(c *SystemConfig) {
				c.DefaultPageLines = -1
			},
			wantErr: true,
		},
		{
			name: "valid default history size zero",
			mutate: func(c *SystemConfig) {
				c.DefaultHistorySize = 0
			},
			wantErr: false,
		},
		{
			name: "valid default history size five hundred",
			mutate: func(c *SystemConfig) {
				c.DefaultHistorySize = 500
			},
			wantErr: false,
		},
		{
			name: "negative default history size",
			mutate: func(c *SystemConfig) {
				c.DefaultHistorySize = -1
			},
			wantErr: true,
		},
		{
			name: "valid filter match mode substring",
			mutate: func(c *SystemConfig) {
				c.FilterMatchMode = "substring"
			},
			wantErr: false,
		},
		{
			name: "valid filter match mode regex",
			mutate: func(c *SystemConfig) {
				c.FilterMatchMode = "regex"
			},
			wantErr: false,
		},
		{
			name: "unrecognized filter match mode",
			mutate: func(c *SystemConfig) {
				c.FilterMatchMode = "fuzzy"
			},
			wantErr: true,
		},
		{
			name: "valid max filter chain depth zero, filtering disabled",
			mutate: func(c *SystemConfig) {
				c.MaxFilterChainDepth = 0
			},
			wantErr: false,
		},
		{
			name: "valid max filter chain depth ten",
			mutate: func(c *SystemConfig) {
				c.MaxFilterChainDepth = 10
			},
			wantErr: false,
		},
		{
			name: "negative max filter chain depth",
			mutate: func(c *SystemConfig) {
				c.MaxFilterChainDepth = -1
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultSystemConfig()
			tc.mutate(&cfg)

			err := cfg.validate()

			if tc.wantErr && err == nil {
				t.Fatalf("cfg.validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("cfg.validate() = %v, want nil", err)
			}
		})
	}
}

// TestDurationAsDuration - This test verifies that AsDuration returns the
// plain time.Duration a Duration wraps, unchanged.
func TestDurationAsDuration(t *testing.T) {
	d := Duration(150 * time.Millisecond)

	if got := d.AsDuration(); got != 150*time.Millisecond {
		t.Fatalf("AsDuration() = %v, want %v", got, 150*time.Millisecond)
	}
}

// TestDurationIsZero - This test verifies that IsZero reports true only
// for a zero Duration and false for any non-zero one.
func TestDurationIsZero(t *testing.T) {
	if !Duration(0).IsZero() {
		t.Fatal("Duration(0).IsZero() = false, want true")
	}

	if Duration(time.Second).IsZero() {
		t.Fatal("Duration(time.Second).IsZero() = true, want false")
	}
}

// TestDurationString - This test verifies that String renders a Duration
// the same way time.Duration's String does, for a zero value, a whole
// unit, and a sub-second value.
func TestDurationString(t *testing.T) {
	cases := []struct {
		in   Duration
		want string
	}{
		{
			in:   Duration(0),
			want: "0s",
		},
		{
			in:   Duration(time.Minute),
			want: "1m0s",
		},
		{
			in:   Duration(150 * time.Millisecond),
			want: "150ms",
		},
	}

	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("Duration(%v).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestDurationUnmarshalYAML - This test table drives UnmarshalYAML across
// every accepted duration spelling: minutes, combined hours and minutes,
// milliseconds, and every variant of zero YAML allows.
func TestDurationUnmarshalYAML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Duration
	}{
		{
			name: "minutes",
			in:   "10m",
			want: Duration(10 * time.Minute),
		},
		{
			name: "hours and minutes",
			in:   "1h30m",
			want: Duration(90 * time.Minute),
		},
		{
			name: "milliseconds",
			in:   "500ms",
			want: Duration(500 * time.Millisecond),
		},
		{
			name: "plain zero",
			in:   "0",
			want: 0,
		},
		{
			name: "zero seconds",
			in:   "0s",
			want: 0,
		},
		{
			name: "zero minutes",
			in:   "0m",
			want: 0,
		},
		{
			name: "zero hours",
			in:   "0h",
			want: 0,
		},
		{
			name: "null",
			in:   "null",
			want: 0,
		},
		{
			name: "tilde",
			in:   "~",
			want: 0,
		},
		{
			name: "empty quoted string",
			in:   `""`,
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d Duration

			if err := yaml.Unmarshal([]byte(tc.in), &d); err != nil {
				t.Fatalf("yaml.Unmarshal(%q) returned error: %v", tc.in, err)
			}

			if d != tc.want {
				t.Fatalf("unmarshaled %q = %v, want %v", tc.in, d, tc.want)
			}
		})
	}
}

// TestDurationUnmarshalYAMLErrorCases - This test verifies that a value
// with no unit, a value of the wrong YAML kind entirely, such as a
// boolean, a mapping, or a sequence, is rejected rather than silently
// coerced.
func TestDurationUnmarshalYAMLErrorCases(t *testing.T) {
	for _, content := range []string{
		"notaduration",
		"5",
		"1.5",
		"true",
		"{}",
		"[]",
	} {
		var d Duration

		if err := yaml.Unmarshal([]byte(content), &d); err == nil {
			t.Errorf("yaml.Unmarshal(%q) = nil error, want error", content)
		}
	}
}

// TestDurationMarshalYAML - This test verifies that MarshalYAML renders a
// Duration back out as the same duration string time.Duration's String
// would produce.
func TestDurationMarshalYAML(t *testing.T) {
	cases := []struct {
		name string
		in   Duration
		want string
	}{
		{
			name: "zero",
			in:   Duration(0),
			want: "0s",
		},
		{
			name: "ten minutes",
			in:   Duration(10 * time.Minute),
			want: "10m0s",
		},
		{
			name: "hour and minutes",
			in:   Duration(90 * time.Minute),
			want: "1h30m0s",
		},
		{
			name: "milliseconds",
			in:   Duration(500 * time.Millisecond),
			want: "500ms",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := yaml.Marshal(tc.in)
			if err != nil {
				t.Fatalf("yaml.Marshal(%v) returned error: %v", tc.in, err)
			}

			gotStr := strings.TrimSpace(string(got))
			if gotStr != tc.want {
				t.Fatalf("yaml.Marshal(%v) = %q, want %q", tc.in, gotStr, tc.want)
			}
		})
	}
}

// TestDurationYAMLRoundTrip - This test verifies that marshaling a
// Duration and unmarshaling the result back produces the exact same value,
// for a handful of representative durations.
func TestDurationYAMLRoundTrip(t *testing.T) {
	for _, d := range []Duration{
		0,
		Duration(10 * time.Minute),
		Duration(90 * time.Minute),
		Duration(500 * time.Millisecond),
	} {
		data, err := yaml.Marshal(d)
		if err != nil {
			t.Fatalf("yaml.Marshal(%v) returned error: %v", d, err)
		}

		var out Duration
		if err := yaml.Unmarshal(data, &out); err != nil {
			t.Fatalf("yaml.Unmarshal(%q) returned error: %v", data, err)
		}

		if out != d {
			t.Fatalf("round trip %v = %v, want %v", d, out, d)
		}
	}
}

// TestSystemConfigYAMLRoundTrip - This test verifies that marshaling a
// fully populated SystemConfig and unmarshaling the result back produces
// the exact same struct, including a TOTPIssuer value containing a literal
// "#" that could otherwise be misread as a YAML comment.
func TestSystemConfigYAMLRoundTrip(t *testing.T) {
	in := SystemConfig{
		ProductName:           "AcmeRouter",
		PreventEscape:         true,
		LogLevel:              3,
		HistoryFile:           "/tmp/hist",
		AuditLogFile:          "/tmp/audit.log",
		AuditLogEnabled:       true,
		CurrentLanguage:       "fr",
		DefaultLanguage:       "en",
		LanguageDir:           "custom-lang",
		SessionIdleTimeout:    Duration(10 * time.Minute),
		ElevationTimeout:      Duration(30 * time.Second),
		ReauthGracePeriod:     Duration(45 * time.Second),
		AuthBypassTrustWindow: Duration(2 * time.Minute),
		TreeStructure:         "custom-tree-structure.yaml",
		CommonTreeFile:        "custom-common.yaml",
		AuthRequired:          true,
		UsersFile:             "custom-users.yaml",
		LoginMaxAttempts:      5,
		TOTPIssuer:            "My # Company",
		TOTPMaxAttempts:       4,

		PasswordMinLength:           14,
		PasswordRequireUppercase:    true,
		PasswordRequireNumbers:      true,
		PasswordRequireSpecialChars: true,
		PasswordChangeMaxAttempts:   5,

		EnableHostAuthentication: true,
		EnableCLIAuthentication:  true,
		EnableTOTPAuthentication: true,
		AuthProviders: []AuthProviderConfig{
			{Name: "local", Type: "local"},
			{Name: "corp-ldap", Type: "ldap"},
		},
		CLIAuthProvider: "local",

		PagingEnabled:       false,
		DefaultPageLines:    40,
		DefaultHistorySize:  1000,
		FilterMatchMode:     "regex",
		MaxFilterChainDepth: 5,

		RolesFile:   "custom-roles.yaml",
		DefaultsDir: "custom-defaults",
	}

	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml.Marshal(SystemConfig) returned error: %v", err)
	}

	var out SystemConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("yaml.Unmarshal(%q) returned error: %v", data, err)
	}

	if !systemConfigsEqual(out, in) {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

// TestLoadSystemConfigEmptyMappingUsesDefaults - This test verifies that a
// config file containing only "{}", an explicit empty mapping rather than
// an empty file, also falls back to DefaultSystemConfig.
func TestLoadSystemConfigEmptyMappingUsesDefaults(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "{}\n")

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("empty mapping should use defaults, got error: %v", err)
	}

	if !systemConfigsEqual(cfg, DefaultSystemConfig()) {
		t.Fatalf("cfg = %+v, want default %+v", cfg, DefaultSystemConfig())
	}
}

// TestLoadSystemConfigWhitespaceOnlyUsesDefaults - This test verifies that
// a config file containing nothing but blank lines also falls back to
// DefaultSystemConfig.
func TestLoadSystemConfigWhitespaceOnlyUsesDefaults(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "   \n  \n")

	cfg, err := LoadSystemConfig(path)
	if err != nil {
		t.Fatalf("whitespace-only file should use defaults, got error: %v", err)
	}

	if !systemConfigsEqual(cfg, DefaultSystemConfig()) {
		t.Fatalf("cfg = %+v, want default %+v", cfg, DefaultSystemConfig())
	}
}

// TestLoadSystemConfigDirectoryAsConfigFileErrors - This test verifies
// that pointing the config path at a directory instead of a file returns
// an error, along with the default config rather than a partially read
// one.
func TestLoadSystemConfigDirectoryAsConfigFileErrors(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadSystemConfig(dir)
	if err == nil {
		t.Fatal("expected an error when config path is a directory, got nil")
	}

	if !systemConfigsEqual(cfg, DefaultSystemConfig()) {
		t.Errorf("cfg = %+v, want default %+v on read error", cfg, DefaultSystemConfig())
	}
}

// TestLoadSystemConfigTopLevelNonMappingErrors - This test verifies that a
// config file whose top-level YAML value is a string, number, boolean, or
// list, rather than a mapping of field names to values, is rejected.
func TestLoadSystemConfigTopLevelNonMappingErrors(t *testing.T) {
	cases := []string{
		`"just a string"`,
		`123`,
		`true`,
		`- a`,
		`[]`,
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content+"\n")

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for top-level non-mapping %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigInlineAndTrailingComments - This test verifies that
// a config file mixing inline, trailing, and standalone comments among its
// real values still parses those values correctly.
func TestLoadSystemConfigInlineAndTrailingComments(t *testing.T) {
	content := `
# top comment
HistoryFile: /tmp/hist # history
LogLevel: 3
SessionIdleTimeout: 10m # idle
# another comment
ElevationTimeout: 5s
`

	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("LoadSystemConfig returned error: %v", err)
	}

	if cfg.HistoryFile != "/tmp/hist" {
		t.Errorf("HistoryFile = %q, want /tmp/hist", cfg.HistoryFile)
	}

	if cfg.LogLevel != 3 {
		t.Errorf("LogLevel = %d, want 3", cfg.LogLevel)
	}

	if cfg.SessionIdleTimeout != Duration(10*time.Minute) {
		t.Errorf("SessionIdleTimeout = %v, want 10m", cfg.SessionIdleTimeout)
	}

	if cfg.ElevationTimeout != Duration(5*time.Second) {
		t.Errorf("ElevationTimeout = %v, want 5s", cfg.ElevationTimeout)
	}
}

// TestLoadSystemConfigQuotedValues - This test verifies that quoted string
// and duration values parse the same as their unquoted forms, including a
// TOTPIssuer value that contains a literal "#" only safe from being read
// as a comment because it is quoted.
func TestLoadSystemConfigQuotedValues(t *testing.T) {
	content := `
HistoryFile: "/tmp/hist"
AuditLogFile: "/tmp/audit.log"
CurrentLanguage: "fr"
SessionIdleTimeout: "10m"
TOTPIssuer: "My # Company"
`

	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("LoadSystemConfig returned error: %v", err)
	}

	if cfg.HistoryFile != "/tmp/hist" {
		t.Errorf("HistoryFile = %q, want /tmp/hist", cfg.HistoryFile)
	}

	if cfg.AuditLogFile != "/tmp/audit.log" {
		t.Errorf("AuditLogFile = %q, want /tmp/audit.log", cfg.AuditLogFile)
	}

	if cfg.CurrentLanguage != "fr" {
		t.Errorf("CurrentLanguage = %q, want fr", cfg.CurrentLanguage)
	}

	if cfg.SessionIdleTimeout != Duration(10*time.Minute) {
		t.Errorf("SessionIdleTimeout = %v, want 10m", cfg.SessionIdleTimeout)
	}

	if cfg.TOTPIssuer != "My # Company" {
		t.Errorf("TOTPIssuer = %q, want My # Company", cfg.TOTPIssuer)
	}
}

// TestLoadSystemConfigEmptyStringOverridesDefault - This test verifies
// that a field explicitly set to an empty string in the config file is
// honored as an empty string, not treated as unset and left at its
// non-empty default.
func TestLoadSystemConfigEmptyStringOverridesDefault(t *testing.T) {
	content := `
HistoryFile: ""
AuditLogFile: ""
`

	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("LoadSystemConfig returned error: %v", err)
	}

	if cfg.HistoryFile != "" {
		t.Errorf("HistoryFile = %q, want empty string", cfg.HistoryFile)
	}

	if cfg.AuditLogFile != "" {
		t.Errorf("AuditLogFile = %q, want empty string", cfg.AuditLogFile)
	}
}

// TestLoadSystemConfigMinimumValidValues - This test verifies that the
// lowest value each validated field accepts, LogLevel 0, LoginMaxAttempts
// 1, and a zero duration for both timeouts, loads without error.
func TestLoadSystemConfigMinimumValidValues(t *testing.T) {
	content := `
LogLevel: 0
LoginMaxAttempts: 1
SessionIdleTimeout: 0
ElevationTimeout: 0s
`

	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("minimum valid values returned error: %v", err)
	}

	if cfg.LogLevel != 0 {
		t.Errorf("LogLevel = %d, want 0", cfg.LogLevel)
	}

	if cfg.LoginMaxAttempts != 1 {
		t.Errorf("LoginMaxAttempts = %d, want 1", cfg.LoginMaxAttempts)
	}

	if cfg.SessionIdleTimeout != 0 {
		t.Errorf("SessionIdleTimeout = %v, want 0", cfg.SessionIdleTimeout)
	}

	if cfg.ElevationTimeout != 0 {
		t.Errorf("ElevationTimeout = %v, want 0", cfg.ElevationTimeout)
	}
}

// TestLoadSystemConfigValidLogLevelValues - This test table drives every
// documented LogLevel value, 0, 1, 3, and 5, confirming each one loads and
// round trips onto SystemConfig unchanged.
func TestLoadSystemConfigValidLogLevelValues(t *testing.T) {
	for _, lv := range []int{0, 1, 3, 5} {
		t.Run(fmt.Sprintf("log level %d", lv), func(t *testing.T) {
			content := fmt.Sprintf("LogLevel: %d\n", lv)

			cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
			if err != nil {
				t.Fatalf("LogLevel %d returned error: %v", lv, err)
			}

			if cfg.LogLevel != lv {
				t.Fatalf("LogLevel = %d, want %d", cfg.LogLevel, lv)
			}
		})
	}
}

// TestLoadSystemConfigNonScalarStringFieldErrors - This test verifies that
// a string field given a mapping, a list, or another non-scalar YAML value
// is rejected, rather than being coerced or silently ignored.
func TestLoadSystemConfigNonScalarStringFieldErrors(t *testing.T) {
	cases := []string{
		"HistoryFile: {}\n",
		"HistoryFile: []\n",
		"UsersFile: {name: x}\n",
		"TOTPIssuer: [a, b]\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for non-scalar string field in %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigCaseSensitiveKeys - This test verifies that a field
// name spelled with the wrong case is treated as an unknown key and
// rejected, rather than matched case insensitively.
func TestLoadSystemConfigCaseSensitiveKeys(t *testing.T) {
	cases := []string{
		"preventEscape: true\n",
		"preventescape: true\n",
		"loglevel: 3\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an unknown-key error for %q, got nil", content)
		}
	}
}

// TestLoadSystemConfigMultipleDocumentsSecondInvalid - This test verifies
// that the multiple document rejection fires even when only the second
// document is malformed, confirming the whole file is read and checked
// rather than stopping after a valid first document.
func TestLoadSystemConfigMultipleDocumentsSecondInvalid(t *testing.T) {
	content := "HistoryFile: /tmp/hist\n" +
		"---\n" +
		"HistoryFile: [unclosed\n"

	path := tempDirFile(t, "routercli.yaml", content)

	if _, err := LoadSystemConfig(path); err == nil {
		t.Fatal("expected an error for invalid second YAML document, got nil")
	}
}

// TestLoadSystemConfigAdditionalValidTimeouts - This test table drives a
// further spread of accepted duration values across both timeout fields,
// including a zero written with an explicit unit.
func TestLoadSystemConfigAdditionalValidTimeouts(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantIdle Duration
		wantElev Duration
	}{
		{
			name:     "zero minutes idle",
			content:  "SessionIdleTimeout: 0m\n",
			wantIdle: 0,
			wantElev: 0,
		},
		{
			name:     "zero hours elevation",
			content:  "ElevationTimeout: 0h\n",
			wantIdle: 0,
			wantElev: 0,
		},
		{
			name:     "ninety minutes idle",
			content:  "SessionIdleTimeout: 90m\n",
			wantIdle: Duration(90 * time.Minute),
			wantElev: 0,
		},
		{
			name:     "minute and seconds elevation",
			content:  "ElevationTimeout: 1m30s\n",
			wantIdle: 0,
			wantElev: Duration(90 * time.Second),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", tc.content))
			if err != nil {
				t.Fatalf("LoadSystemConfig(%q) returned error: %v", tc.content, err)
			}

			if cfg.SessionIdleTimeout != tc.wantIdle {
				t.Errorf("SessionIdleTimeout = %v, want %v", cfg.SessionIdleTimeout, tc.wantIdle)
			}

			if cfg.ElevationTimeout != tc.wantElev {
				t.Errorf("ElevationTimeout = %v, want %v", cfg.ElevationTimeout, tc.wantElev)
			}
		})
	}
}

// TestLoadSystemConfigRejectsUndocumentedLogLevelValues - This test table
// drives LogLevel values outside the documented set, 0, 1, 3, and 5,
// confirming each one is rejected rather than silently accepted.
func TestLoadSystemConfigRejectsUndocumentedLogLevelValues(t *testing.T) {
	for _, lv := range []int{2, 4, 6, -1} {
		t.Run(fmt.Sprintf("log level %d", lv), func(t *testing.T) {
			content := fmt.Sprintf("LogLevel: %d\n", lv)
			path := tempDirFile(t, "routercli.yaml", content)

			if _, err := LoadSystemConfig(path); err == nil {
				t.Fatalf("LogLevel %d: expected error if documented values are 0, 1, 3, 5, got nil", lv)
			}
		})
	}
}

// TestDurationUnmarshalYAMLRejectsNegative - This test verifies that
// UnmarshalYAML itself, not just SystemConfig's validate step, rejects a
// negative duration string.
func TestDurationUnmarshalYAMLRejectsNegative(t *testing.T) {
	var d Duration

	if err := yaml.Unmarshal([]byte("-5m"), &d); err == nil {
		t.Fatal("expected an error for negative duration, got nil")
	}
}

// ----------------------------------------------------------------------
//
// Rate limiting fields: LoginMaxAttempts and the AttemptWindow and
// LockoutDuration fields for login, Command Level, and command
// password rate limiting
//
// ----------------------------------------------------------------------

// TestLoadSystemConfigRateLimitFieldsDefaultToDisabled - This test
// verifies that every AttemptWindow and LockoutDuration field defaults to
// zero, meaning windowed rate limiting is opt-in, while LoginMaxAttempts
// defaults to 3.
func TestLoadSystemConfigRateLimitFieldsDefaultToDisabled(t *testing.T) {
	cfg := DefaultSystemConfig()
	pairs := []struct {
		name    string
		window  Duration
		lockout Duration
	}{
		{"Login", cfg.LoginAttemptWindow, cfg.LoginLockoutDuration},
		{"CommandLevel", cfg.CommandLevelAttemptWindow, cfg.CommandLevelLockoutDuration},
		{"CommandPassword", cfg.CommandPasswordAttemptWindow, cfg.CommandPasswordLockoutDuration},
	}
	for _, p := range pairs {
		if p.window != 0 || p.lockout != 0 {
			t.Errorf("%s: window=%v lockout=%v, want both zero by default (windowed rate limiting is opt-in)", p.name, p.window, p.lockout)
		}
	}
	if cfg.CommandLevelMaxAttempts != 0 {
		t.Errorf("CommandLevelMaxAttempts default = %d, want 0, meaning unlimited", cfg.CommandLevelMaxAttempts)
	}
	if cfg.CommandPasswordMaxAttempts != 0 {
		t.Errorf("CommandPasswordMaxAttempts default = %d, want 0, meaning unlimited", cfg.CommandPasswordMaxAttempts)
	}
	if cfg.LoginMaxAttempts != 3 {
		t.Errorf("LoginMaxAttempts default = %d, want 3 (unchanged from before this feature existed)", cfg.LoginMaxAttempts)
	}
}

// TestLoadSystemConfigRateLimitFieldsParseCorrectly - This test verifies
// that a config file setting every login, Command Level, and command
// password rate limiting field loads each one onto SystemConfig correctly.
func TestLoadSystemConfigRateLimitFieldsParseCorrectly(t *testing.T) {
	content := "LoginMaxAttempts: 5\n" +
		"LoginAttemptWindow: 2m\n" +
		"LoginLockoutDuration: 5m\n" +
		"CommandLevelMaxAttempts: 3\n" +
		"CommandLevelAttemptWindow: 90s\n" +
		"CommandLevelLockoutDuration: 10m\n" +
		"CommandPasswordMaxAttempts: 4\n" +
		"CommandPasswordAttemptWindow: 1m\n" +
		"CommandPasswordLockoutDuration: 15m\n"
	cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", content))
	if err != nil {
		t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
	}

	if cfg.LoginMaxAttempts != 5 || cfg.LoginAttemptWindow != Duration(2*time.Minute) || cfg.LoginLockoutDuration != Duration(5*time.Minute) {
		t.Errorf("Login* fields = %d/%v/%v, want 5/2m/5m", cfg.LoginMaxAttempts, cfg.LoginAttemptWindow, cfg.LoginLockoutDuration)
	}
	if cfg.CommandLevelMaxAttempts != 3 || cfg.CommandLevelAttemptWindow != Duration(90*time.Second) || cfg.CommandLevelLockoutDuration != Duration(10*time.Minute) {
		t.Errorf("CommandLevel* fields = %d/%v/%v, want 3/90s/10m", cfg.CommandLevelMaxAttempts, cfg.CommandLevelAttemptWindow, cfg.CommandLevelLockoutDuration)
	}
	if cfg.CommandPasswordMaxAttempts != 4 || cfg.CommandPasswordAttemptWindow != Duration(time.Minute) || cfg.CommandPasswordLockoutDuration != Duration(15*time.Minute) {
		t.Errorf("CommandPassword* fields = %d/%v/%v, want 4/1m/15m", cfg.CommandPasswordMaxAttempts, cfg.CommandPasswordAttemptWindow, cfg.CommandPasswordLockoutDuration)
	}
}

// TestLoadSystemConfigHalfConfiguredAttemptWindowPairIsError - This test
// -This test is the direct test of validate's reasoning. An AttemptWindow
// set without its matching LockoutDuration, or the other way around, looks
// like rate limiting was opted into, but does nothing, since a zero
// duration lockout expires instantly and a lockout with no window is never
// triggered. This project's fail loudly instead of silently doing nothing
// convention means this half-configured state should be a hard error at
// startup, not a config that quietly provides no protection.
func TestLoadSystemConfigHalfConfiguredAttemptWindowPairIsError(t *testing.T) {
	cases := []string{
		"LoginAttemptWindow: 2m\n",
		"LoginLockoutDuration: 5m\n",
		"CommandLevelAttemptWindow: 2m\n",
		"CommandLevelLockoutDuration: 5m\n",
		"CommandPasswordAttemptWindow: 2m\n",
		"CommandPasswordLockoutDuration: 5m\n",
	}
	for _, content := range cases {
		t.Run(content, func(t *testing.T) {
			path := tempDirFile(t, "routercli.yaml", content)
			if _, err := LoadSystemConfig(path); err == nil {
				t.Errorf("expected an error for half-configured pair in %q, got nil", content)
			}
		})
	}
}

// TestLoadSystemConfigFullyConfiguredAttemptWindowPairIsNotError - This
// test verifies that setting both AttemptWindow and its matching
// LockoutDuration together, the fully opted in state, loads without error.
func TestLoadSystemConfigFullyConfiguredAttemptWindowPairIsNotError(t *testing.T) {
	content := "LoginAttemptWindow: 2m\nLoginLockoutDuration: 5m\n"
	path := tempDirFile(t, "routercli.yaml", content)
	if _, err := LoadSystemConfig(path); err != nil {
		t.Errorf("expected no error for a fully configured window and lockout pair, got: %v", err)
	}
}

// TestLoadSystemConfigBothZeroAttemptWindowPairIsNotError - This test
// verifies that the default state, both AttemptWindow and LockoutDuration
// left unset, remains valid. This is what not opted in looks like, not an
// error.
func TestLoadSystemConfigBothZeroAttemptWindowPairIsNotError(t *testing.T) {
	path := tempDirFile(t, "routercli.yaml", "")
	if _, err := LoadSystemConfig(path); err != nil {
		t.Errorf("expected no error when every rate limit field is left at its default, got: %v", err)
	}
}

// TestLoadSystemConfigAlphabeticalCommandOrderAndMergeCommonCommands -This
// test verifies every one of the four combinations these two command
// listing ordering flags can produce, three of them a valid config file,
// and confirms which of the three that is loads with the given fields set,
// matching DefaultSystemConfig's true, true defaults when both are left
// unset, see TestDefaultSystemConfigValues. The fourth combination,
// AlphabeticalCommandOrder false with MergeCommonCommands true, is not a
// valid config file at all; see TestLoadSystemConfigAlphabeticalCommandOrd
// erFalseWithMergeCommonCommandsTrueIsError below for that one.
func TestLoadSystemConfigAlphabeticalCommandOrderAndMergeCommonCommands(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		wantAlphabetical bool
		wantMergeCommon  bool
	}{
		{"both left unset, everything merged into one alphabetical list", "", true, true},
		{"MergeCommonCommands off only, non-common then common, both groups alphabetical", "MergeCommonCommands: false\n", true, false},
		{"both off, non-common then common, both groups in their own tree file order", "AlphabeticalCommandOrder: false\nMergeCommonCommands: false\n", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadSystemConfig(tempDirFile(t, "routercli.yaml", tc.content))
			if err != nil {
				t.Fatalf("LoadSystemConfig returned unexpected error: %v", err)
			}
			if cfg.AlphabeticalCommandOrder != tc.wantAlphabetical {
				t.Errorf("AlphabeticalCommandOrder = %v, want %v", cfg.AlphabeticalCommandOrder, tc.wantAlphabetical)
			}
			if cfg.MergeCommonCommands != tc.wantMergeCommon {
				t.Errorf("MergeCommonCommands = %v, want %v", cfg.MergeCommonCommands, tc.wantMergeCommon)
			}
		})
	}
}

// TestLoadSystemConfigAlphabeticalCommandOrderFalseWithMergeCommonCommandsTrueIsError
// - This test verifies the one combination of these two flags that is not
// valid: AlphabeticalCommandOrder false leaves no single combined
// definition order across a level's tree file and CommonTreeFile for
// MergeCommonCommands true to merge common commands into, so
// LoadSystemConfig rejects it as a hard error at startup rather than
// silently falling back to the appended form, the same way an unrecognized
// CLIAuthProvider name is rejected outright instead of falling back to
// something plausible. Setting AlphabeticalCommandOrder false with no
// explicit MergeCommonCommands value is enough to trigger this, since
// MergeCommonCommands defaults to true.
func TestLoadSystemConfigAlphabeticalCommandOrderFalseWithMergeCommonCommandsTrueIsError(t *testing.T) {
	cases := []string{
		"AlphabeticalCommandOrder: false\n",
		"AlphabeticalCommandOrder: false\nMergeCommonCommands: true\n",
	}

	for _, content := range cases {
		path := tempDirFile(t, "routercli.yaml", content)

		if _, err := LoadSystemConfig(path); err == nil {
			t.Errorf("expected an error for AlphabeticalCommandOrder false with MergeCommonCommands true in %q, got nil", content)
		}
	}
}

// TestBaseDirResolvesRelativePathsAgainstTheConfigFile - This test verifies
// that a relative BaseDir resolves against the directory holding the
// configuration file, not the working directory, so one configuration file
// works whatever directory the binary is started from.
func TestBaseDirResolvesRelativePathsAgainstTheConfigFile(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("failed to create the etc directory: %v", err)
	}
	cfgPath := filepath.Join(etc, "routercli.yaml")
	body := "BaseDir: \"..\"\nTreeStructure: \"var/tree/tree_structure.yaml\"\nUsersFile: \"etc/users.yaml\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write the configuration file: %v", err)
	}

	cfg, err := LoadSystemConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load the configuration: %v", err)
	}

	wantTree := filepath.Join(root, "var", "tree", "tree_structure.yaml")
	if cfg.TreeStructure != wantTree {
		t.Errorf("TreeStructure = %q, want %q", cfg.TreeStructure, wantTree)
	}
	wantUsers := filepath.Join(root, "etc", "users.yaml")
	if cfg.UsersFile != wantUsers {
		t.Errorf("UsersFile = %q, want %q", cfg.UsersFile, wantUsers)
	}
}

// TestBaseDirLeavesAbsolutePathsAlone - This test verifies that an absolute
// path is never rewritten, so a deployment keeping its socket or its logs
// outside the tree gets what it asked for.
func TestBaseDirLeavesAbsolutePathsAlone(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "routercli.yaml")
	body := "BaseDir: \".\"\nAuditLogFile: \"/var/log/routercli-audit.log\"\nHistoryFile: \"var/log/history.log\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write the configuration file: %v", err)
	}

	cfg, err := LoadSystemConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load the configuration: %v", err)
	}

	if cfg.AuditLogFile != "/var/log/routercli-audit.log" {
		t.Errorf("an absolute path was rewritten: %q", cfg.AuditLogFile)
	}
	if want := filepath.Join(root, "var", "log", "history.log"); cfg.HistoryFile != want {
		t.Errorf("HistoryFile = %q, want %q", cfg.HistoryFile, want)
	}
}

// TestUnsetBaseDirLeavesEveryPathUntouched - This test verifies that a
// configuration which never sets BaseDir behaves exactly as it did before
// BaseDir existed, with every path resolving against the working directory.
func TestUnsetBaseDirLeavesEveryPathUntouched(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "routercli.yaml")
	body := "TreeStructure: \"var/tree/tree_structure.yaml\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write the configuration file: %v", err)
	}

	cfg, err := LoadSystemConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load the configuration: %v", err)
	}

	if cfg.TreeStructure != "var/tree/tree_structure.yaml" {
		t.Errorf("with no BaseDir the path should be untouched, got %q", cfg.TreeStructure)
	}
}

// TestBaseDirIsItselfResolvedToAnAbsolutePath - This test verifies that
// BaseDir is normalised alongside the paths it resolves, rather than being
// left as whatever relative form the file wrote.
//
// Every other path field comes back absolute, so leaving this one as ".."
// would make a loaded SystemConfig inconsistent with itself, and anything
// inspecting the configuration, a diagnostic dump for instance, would report
// a base that means nothing without knowing which file it came from.
func TestBaseDirIsItselfResolvedToAnAbsolutePath(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("failed to create the etc directory: %v", err)
	}
	cfgPath := filepath.Join(etc, "routercli.yaml")
	body := "BaseDir: \"..\"\nTreeStructure: \"var/tree/tree_structure.yaml\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write the configuration file: %v", err)
	}

	cfg, err := LoadSystemConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load the configuration: %v", err)
	}

	if cfg.BaseDir != root {
		t.Errorf("BaseDir = %q, want it resolved to %q", cfg.BaseDir, root)
	}
	if !filepath.IsAbs(cfg.BaseDir) {
		t.Errorf("BaseDir = %q, want an absolute path", cfg.BaseDir)
	}
}
