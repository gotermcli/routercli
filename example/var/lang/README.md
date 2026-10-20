# Language Catalog

Each file in the language directory (by default `var/lang/`) is a catalog of
text for a specific language. The main configuration file (`etc/routercli.yaml`)
allows one to establish the default language and the current language used by
RouterCLI.

Each piece of text in a language catalog file is represented as a flat key /
value pair, meaning there is no nesting. Catalog entries can contain format
verbs (%s, %d, %q, etc.) and any arguments passed to Translator.T() are applied
with fmt.Sprintf, so a catalog entry's verb count and order must match what the
calling code passes.

This structure enables all text displayed (system messages, command
descriptions, and command help) to a user to be translated into different
languages.

The name for each language file in the catalog, found in `var/lang/` should use
the standard i18n language code (e.g., `en` for English, `fr` for French, etc),
since that name becomes the code a person types into `language set <code>`.
Nothing in the loader checks this against a real list of codes, so it works as
a project convention rather than something enforced at startup.


## Default Language

RouterCLI supports a default language for all descriptions and help text
(see DefaultLanguage in etc/routercli.yaml). This enables all other language
catalogs to fall back to the default language for any key it does not have.

This means that the default language catalog file MUST always be the most
complete catalog in var/lang/. If a key is missing from both the current
language and the default language, Translator.T() returns the key itself,
bracketed (e.g. `[[show.desc]]`), instead of blank text.


## Literal Text in YAML Files

While it is possible to define a command's description and help text directly as
a literal in the `desc:` and `help:` fields in the corresponding YAML file
(var/tree/level_*.yaml), it is best practice to instead use a `desc_key:` /
`help_key:` field pointing at a Language Catalog entry.


## Text for Runtime Errors

Runtime messages like errors, confirmations, and other text produced directly by
a command's Go handler (e.g. "language.set.confirm" in cmd/core/cmd_language.go) work
differently. These have no literal option at all, and are always looked up via
a Language Catalog entry.


## Non-Existing Catalog

If `var/lang/` does not exist, RouterCLI still runs, because the Language
Catalog is optional and Translator.T() falls back to returning
`[[key]]` for everything. A catalog file that exists but fails to parse, however,
is a hard error at startup. NOTE: Some runtime messages and errors will use the
Language Catalog, so if it is not defined these will show a bracketed key.


## Language Selection

The current language can be set at runtime, so a user can easily switch between
languages, without needing to edit `etc/routercli.yaml` or restart RouterCLI:

- `language list` shows every loaded language, marking the active one.
- `language set <code>` switches to a different loaded language (e.g. `language
  set fr`).
