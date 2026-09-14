// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadCatalogs - This reads every *.yaml file in dir and returns them
// keyed by language code, taken from each file name, so example/var/lang/en.yaml
// becomes "en". Each file is a flat map of key to text, with no nesting
// and no wrapper key repeating what the filename already says.
//
// A missing directory is not an error; it returns an empty set. i18n is
// opt in, and a project that has not set up var/lang/ yet should still
// run, with every T call falling back to its bracketed key.
//
// A file that exists but fails to parse is a hard error. A malformed
// catalog is a real mistake, and silently dropping that language would
// hide it.
func LoadCatalogs(dir string) (map[string]Catalog, error) {
	catalogs := make(map[string]Catalog)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return catalogs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error reading language directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		lang := strings.TrimSuffix(entry.Name(), ".yaml")
		path := filepath.Join(dir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("error reading language file %q: %w", path, err)
		}

		var cat Catalog
		if err := yaml.Unmarshal(data, &cat); err != nil {
			return nil, fmt.Errorf("error parsing language file %q: %w", path, err)
		}
		catalogs[lang] = cat
	}

	return catalogs, nil
}
