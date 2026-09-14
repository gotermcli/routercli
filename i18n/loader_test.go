// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadCatalogsValid - This test verifies that LoadCatalogs reads every
// *.yaml file in a directory into its own catalog, keyed by the language
// code from the file name.
func TestLoadCatalogsValid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en.yaml", "greeting: Hello\nfarewell: Goodbye\n")
	writeFile(t, dir, "fr.yaml", "greeting: Bonjour\n")

	catalogs, err := LoadCatalogs(dir)
	if err != nil {
		t.Fatalf("LoadCatalogs returned unexpected error: %v", err)
	}
	if len(catalogs) != 2 {
		t.Fatalf("got %d catalogs, want 2", len(catalogs))
	}
	if catalogs["en"]["greeting"] != "Hello" {
		t.Errorf("en greeting = %q, want %q", catalogs["en"]["greeting"], "Hello")
	}
	if catalogs["fr"]["greeting"] != "Bonjour" {
		t.Errorf("fr greeting = %q, want %q", catalogs["fr"]["greeting"], "Bonjour")
	}
}

// TestLoadCatalogsIgnoresNonYAMLFiles - This test verifies that a non-YAML
// file sitting in the language directory, such as a README, is skipped
// rather than causing an error.
func TestLoadCatalogsIgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en.yaml", "greeting: Hello\n")
	writeFile(t, dir, "README.md", "not a catalog\n")

	catalogs, err := LoadCatalogs(dir)
	if err != nil {
		t.Fatalf("LoadCatalogs returned unexpected error: %v", err)
	}
	if len(catalogs) != 1 {
		t.Fatalf("got %d catalogs, want 1 (README.md should be ignored)", len(catalogs))
	}
}

// TestLoadCatalogsMissingDirectoryIsNotAnError - This test verifies that a
// language directory that does not exist on disk at all returns an empty
// catalog set rather than an error, matching how a project with i18n never
// wired in at all is meant to work.
func TestLoadCatalogsMissingDirectoryIsNotAnError(t *testing.T) {
	catalogs, err := LoadCatalogs("/nonexistent/lang/dir")
	if err != nil {
		t.Fatalf("a missing language directory should not error, got: %v", err)
	}
	if len(catalogs) != 0 {
		t.Errorf("expected an empty catalog set, got %d catalogs", len(catalogs))
	}
}

// TestLoadCatalogsMalformedYAMLIsAnError - This test verifies that a
// catalog file with invalid YAML returns an error instead of a partial or
// empty catalog.
func TestLoadCatalogsMalformedYAMLIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en.yaml", "this: [is not, valid: yaml structure for a flat map")

	_, err := LoadCatalogs(dir)
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}
