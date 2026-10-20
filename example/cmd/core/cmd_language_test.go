// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package core

import (
	"strings"
	"testing"

	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/i18n"
)

// newLanguageTestContext - This function builds a *command.AppContext with
// a real *i18n.Translator carrying two loaded catalogs, "en" and "fr", the
// minimum needed to exercise language.list and language.set for real,
// since both handlers call methods directly on ctx.Translator rather than
// only through the nil-safe T().
func newLanguageTestContext() *command.AppContext {
	ctx := newTestContext()
	catalogs := map[string]i18n.Catalog{
		"en": {"language.list.header": "Available languages:"},
		"fr": {"language.list.header": "Langues disponibles :"},
	}
	ctx.Translator = i18n.New(catalogs, "en", "en")
	return ctx
}

// TestLanguageListPrintsEveryLoadedLanguage - This test verifies that
// "language list" prints every loaded language code, not merely that it runs.
func TestLanguageListPrintsEveryLoadedLanguage(t *testing.T) {
	ctx := newLanguageTestContext()
	cmd := loadTestCommand(t, "language.list")

	var runErr error
	out := captureStdout(t, func() { runErr = cmd.RunFunc(ctx, nil) })
	if runErr != nil {
		t.Fatalf("language.list handler returned unexpected error: %v", runErr)
	}
	for _, want := range []string{"en", "fr"} {
		if !strings.Contains(out, want) {
			t.Errorf("language.list output = %q, expected it to contain %q", out, want)
		}
	}
}

// TestLanguageSetHandlerSwitchesCurrentLanguage - This test verifies that
// "language set fr" switches ctx.Translator.CurrentLanguage, lowercasing
// whatever case the argument was typed in.
func TestLanguageSetHandlerSwitchesCurrentLanguage(t *testing.T) {
	ctx := newLanguageTestContext()
	cmd := loadTestCommand(t, "language.set")

	if err := cmd.RunFunc(ctx, []string{"FR"}); err != nil {
		t.Fatalf("language.set handler returned unexpected error: %v", err)
	}
	if ctx.Translator.CurrentLanguage() != "fr" {
		t.Errorf("CurrentLanguage() = %q, want %q", ctx.Translator.CurrentLanguage(), "fr")
	}
}

// TestLanguageSetHandlerErrorsOnUnloadedLanguage - This test verifies that
// "language set" with a language code that was never loaded returns an
// error and leaves CurrentLanguage unchanged, rather than switching to
// nothing.
func TestLanguageSetHandlerErrorsOnUnloadedLanguage(t *testing.T) {
	ctx := newLanguageTestContext()
	cmd := loadTestCommand(t, "language.set")

	if err := cmd.RunFunc(ctx, []string{"de"}); err == nil {
		t.Fatal("expected an error for a language code that was never loaded, got nil")
	}
	if ctx.Translator.CurrentLanguage() != "en" {
		t.Errorf("CurrentLanguage() = %q, want unchanged %q", ctx.Translator.CurrentLanguage(), "en")
	}
}
