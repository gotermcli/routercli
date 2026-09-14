// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package i18n

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// Catalog - This type holds one language's worth of translated strings,
// mapping each key to its text.
type Catalog map[string]string

// Translator - This holds every loaded catalog, which language is active,
// and which to fall back to.
//
// defaultLang is set explicitly by whoever constructs the Translator,
// rather than guessed from convention such as the first catalog loaded, so
// there is never ambiguity about what text falls back to.
type Translator struct {
	catalogs    map[string]Catalog
	currentLang string
	defaultLang string
}

// ----------------------------------------------------------------------
// Initialization Functions
// ----------------------------------------------------------------------

// New - This constructs a Translator from already loaded catalogs plus the
// language to start in.
//
// A lang with no loaded catalog falls back to defaultLang rather than
// erroring. An unrecognized language code in a configuration file makes
// for a better startup if the CLI comes up in English than if it refuses
// to start.
//
// A caller that needs to know whether that happened checks
// CurrentLanguage against what it asked for.
func New(catalogs map[string]Catalog, lang, defaultLang string) *Translator {
	t := &Translator{catalogs: catalogs, defaultLang: defaultLang}
	if _, ok := catalogs[lang]; ok {
		t.currentLang = lang
	} else {
		t.currentLang = defaultLang
	}
	return t
}
