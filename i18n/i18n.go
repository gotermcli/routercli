// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package i18n

import (
	"fmt"
	"sort"
)

// ----------------------------------------------------------------------
// Public Methods
// ----------------------------------------------------------------------

// T - A missing translation should be obvious in the CLI output rather
// than buried in a log nobody reads.
//
// This looks up key in the current language, then the default language,
// and failing both returns the key in double brackets, "[[key]]". args are
// applied with fmt.Sprintf when any are given, so a catalog entry can
// carry %s or %d like any format string.
//
// T is safe on a nil *Translator, returning the bracketed key as though no
// catalogs were loaded. Every call site can therefore call it
// unconditionally, with no nil check for the valid case where i18n was
// never wired up.
func (t *Translator) T(key string, args ...any) string {
	if t == nil {
		return "[[" + key + "]]"
	}
	text, ok := t.lookup(t.currentLang, key)
	if !ok {
		text, ok = t.lookup(t.defaultLang, key)
	}
	if !ok {
		// args is not applied to the placeholder. It has no format verbs,
		// so Sprintf would only append an "%!(EXTRA...)" suffix to what is
		// meant to be a clean marker.
		return "[[" + key + "]]"
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

func (t *Translator) lookup(lang, key string) (string, bool) {
	cat, ok := t.catalogs[lang]
	if !ok {
		return "", false
	}
	text, ok := cat[key]
	return text, ok
}

// SetLanguage - Falling back silently is right for a constructor default,
// but wrong for an explicit request. Someone typing "language set" needs
// to be told either that they got what they asked for or why not.
//
// So this switches the active language, and returns an error naming the
// requested language when no catalog is loaded for it, rather than
// falling back.
func (t *Translator) SetLanguage(lang string) error {
	if _, ok := t.catalogs[lang]; !ok {
		return fmt.Errorf("language %q is not loaded (available: %v)", lang, t.AvailableLanguages())
	}
	t.currentLang = lang
	return nil
}

// CurrentLanguage - This method returns the active language code.
func (t *Translator) CurrentLanguage() string {
	return t.currentLang
}

// AvailableLanguages - This returns every loaded language code, sorted,
// for the "language" command to list and for SetLanguage's error message.
// Sorting matters because Go randomizes map iteration order.
func (t *Translator) AvailableLanguages() []string {
	langs := make([]string, 0, len(t.catalogs))
	for l := range t.catalogs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}
