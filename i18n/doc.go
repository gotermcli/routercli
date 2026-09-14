// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package i18n implements message catalog translation for every user facing
string in RouterCLI, including login prompts, command descriptions, and
runtime messages.

A catalog is a flat YAML file, one per language, keyed by the short
language code taken from its filename. The file var/lang/en.yaml becomes
the catalog for "en".

This package does not use golang.org/x/text/message. That package is
built around ICU style plural and gender rules and compiled catalogs,
which is more machinery than a CLI's short, mostly static strings
require. A flat map of key to string with printf style substitution
covers what RouterCLI needs, stays easy to read and hand edit, and keeps
the dependency footprint small.

# Literal Strings and Catalog Lookups

Every user facing string takes one of two forms.

A literal string, written directly in Go or in a tree YAML file's desc
field, is never translated. What is written is exactly what the user
sees, in every language.

A catalog lookup, through Translator.T in Go or through a tree YAML
file's desc_key, help_key, or arghelp_key field, resolves against the
active catalog.

A tree file entry MAY set both a literal field and its key counterpart.
When both are set the key wins, and the literal is used only when no
Translator is available. Every command file RouterCLI ships uses the key
form.

# Catalog Lookup Order

T looks up a key in the current language, then in the default language,
and finally falls back to the key itself printed in double brackets, for
example "[[show.desc]]".

That bracketed form in a running CLI means one thing: no loaded catalog
has an entry for that key. Check var/lang/<code>.yaml for a typo or a
missing line.

# Keys Instead of Literal Text

A key such as desc_key puts a translated string in two places: the key
name in the tree YAML file, and the English text in var/lang/en.yaml
under that same key. Every other language then needs its own entry under
the same key.

Keying a catalog by the literal English text instead avoids inventing a
key name, but a command with only a plain desc field never calls T at
all, so it is never translatable regardless of what any catalog
contains.

RouterCLI uses named keys because a short key survives an English wording
change. Reword the show.desc line in var/lang/en.yaml and every other
language's show.desc entry stays correctly linked. Keying by literal text
breaks that link as soon as the English wording changes.
*/
package i18n
