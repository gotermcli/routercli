// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package i18n

import "testing"

func testCatalogs() map[string]Catalog {
	return map[string]Catalog{
		"en": {
			"greeting":      "Hello",
			"welcome_named": "Welcome, %s",
			"only_in_en":    "English only string",
		},
		"fr": {
			"greeting": "Bonjour",
			// deliberately missing "welcome_named" and "only_in_en" to
			// exercise the fallback chain
		},
	}
}

// TestTranslatorUsesCurrentLanguage - This test verifies that T resolves a
// key against the active language's catalog when that key exists there.
func TestTranslatorUsesCurrentLanguage(t *testing.T) {
	tr := New(testCatalogs(), "fr", "en")
	if got := tr.T("greeting"); got != "Bonjour" {
		t.Errorf("T(\"greeting\") = %q, want %q", got, "Bonjour")
	}
}

// TestTranslatorFallsBackToDefaultLanguage - This test verifies that a key
// missing from the active language's catalog, "welcome_named" here, which
// does not exist in fr, falls back to the default language.
func TestTranslatorFallsBackToDefaultLanguage(t *testing.T) {
	tr := New(testCatalogs(), "fr", "en")
	if got := tr.T("welcome_named", "Bob"); got != "Welcome, Bob" {
		t.Errorf("T(\"welcome_named\", \"Bob\") = %q, want %q", got, "Welcome, Bob")
	}
}

// TestTranslatorFallsBackToBracketedKeyWhenTrulyMissing - This test
// verifies that a key present in neither the active nor the default
// language's catalog falls back to the bracketed key itself, a visible
// placeholder rather than an empty string.
func TestTranslatorFallsBackToBracketedKeyWhenTrulyMissing(t *testing.T) {
	tr := New(testCatalogs(), "fr", "en")
	if got := tr.T("nonexistent_key"); got != "[[nonexistent_key]]" {
		t.Errorf("T(\"nonexistent_key\") = %q, want the bracketed key as a visible placeholder", got)
	}
}

// TestTranslatorMissingKeyWithArgsDoesNotCorruptPlaceholder - This test
// verifies that the bracketed placeholder has no format verbs of its own,
// so passing args alongside a missing key must not run it through Sprintf,
// which would otherwise produce an ugly "[[key]]%!(EXTRA...)" suffix
// instead of the clean placeholder.
func TestTranslatorMissingKeyWithArgsDoesNotCorruptPlaceholder(t *testing.T) {
	tr := New(testCatalogs(), "en", "en")
	got := tr.T("nonexistent_key", "unexpected", 42)
	if got != "[[nonexistent_key]]" {
		t.Errorf("T with a missing key and args = %q, want clean %q", got, "[[nonexistent_key]]")
	}
}

// TestTranslatorConstructorFallsBackForUnknownLanguage - This test verifies
// that requesting a language that was never loaded neither errors nor
// panics, and behaves as though the default language was requested.
func TestTranslatorConstructorFallsBackForUnknownLanguage(t *testing.T) {
	tr := New(testCatalogs(), "de", "en")
	if tr.CurrentLanguage() != "en" {
		t.Errorf("CurrentLanguage() = %q, want fallback to \"en\"", tr.CurrentLanguage())
	}
	if got := tr.T("greeting"); got != "Hello" {
		t.Errorf("T(\"greeting\") = %q, want %q (default language)", got, "Hello")
	}
}

// TestSetLanguageSwitchesActiveCatalog - This test verifies that
// SetLanguage changes which catalog T resolves keys against.
func TestSetLanguageSwitchesActiveCatalog(t *testing.T) {
	tr := New(testCatalogs(), "en", "en")
	if got := tr.T("greeting"); got != "Hello" {
		t.Fatalf("sanity check failed: T(\"greeting\") = %q before switching", got)
	}

	if err := tr.SetLanguage("fr"); err != nil {
		t.Fatalf("SetLanguage(\"fr\") returned unexpected error: %v", err)
	}
	if got := tr.T("greeting"); got != "Bonjour" {
		t.Errorf("after SetLanguage(\"fr\"), T(\"greeting\") = %q, want %q", got, "Bonjour")
	}
}

// TestSetLanguageRejectsUnloadedLanguage - This test verifies that
// SetLanguage rejects a language with no loaded catalog, and leaves the
// active language unchanged rather than switching to a language with
// nothing in it.
func TestSetLanguageRejectsUnloadedLanguage(t *testing.T) {
	tr := New(testCatalogs(), "en", "en")
	err := tr.SetLanguage("de")
	if err == nil {
		t.Fatal("expected an error for an unloaded language, got nil")
	}
	if tr.CurrentLanguage() != "en" {
		t.Errorf("CurrentLanguage() = %q after a failed SetLanguage, want unchanged \"en\"", tr.CurrentLanguage())
	}
}

// TestAvailableLanguagesSortedAndComplete - This test verifies that
// AvailableLanguages lists every loaded catalog's language code, in sorted
// order.
func TestAvailableLanguagesSortedAndComplete(t *testing.T) {
	tr := New(testCatalogs(), "en", "en")
	got := tr.AvailableLanguages()
	want := []string{"en", "fr"}
	if len(got) != len(want) {
		t.Fatalf("AvailableLanguages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AvailableLanguages()[%d] = %q, want %q (should be sorted)", i, got[i], want[i])
		}
	}
}

// TestTranslatorFormatsWithSprintfVerbs - This test verifies that T
// applies args through Sprintf when a catalog entry contains a format
// verb.
func TestTranslatorFormatsWithSprintfVerbs(t *testing.T) {
	tr := New(testCatalogs(), "en", "en")
	if got := tr.T("welcome_named", "Alice"); got != "Welcome, Alice" {
		t.Errorf("T with args = %q, want %q", got, "Welcome, Alice")
	}
}

// TestTranslatorNoArgsDoesNotTouchPercentSigns - This test verifies that a
// catalog string containing a literal "%" is returned unmodified when T is
// called with zero args. Running it through Sprintf regardless would
// either produce a formatting error or misinterpret the "%" as a verb, so
// T skips Sprintf entirely when no args are given.
func TestTranslatorNoArgsDoesNotTouchPercentSigns(t *testing.T) {
	catalogs := map[string]Catalog{"en": {"has_percent": "100% done"}}
	tr := New(catalogs, "en", "en")
	if got := tr.T("has_percent"); got != "100% done" {
		t.Errorf("T(\"has_percent\") = %q, want %q unmodified", got, "100% done")
	}
}

// TestNilTranslatorIsSafe - This test verifies that T is safe to call on a
// nil *Translator, returning the bracketed key with or without args,
// rather than panicking.
func TestNilTranslatorIsSafe(t *testing.T) {
	var tr *Translator
	if got := tr.T("some_key"); got != "[[some_key]]" {
		t.Errorf("nil Translator.T(\"some_key\") = %q, want %q", got, "[[some_key]]")
	}
	if got := tr.T("with_arg", "X"); got != "[[with_arg]]" {
		t.Errorf("nil Translator.T with args = %q, want the bracketed key ignoring args", got)
	}
}
