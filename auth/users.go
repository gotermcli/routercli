// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------
// Public Functions - Users
// ----------------------------------------------------------------------

// LoadUsers - This function reads a user database from a YAML file at
// path. A user with an empty PasswordHash is a hard error at load time. An
// account nobody can ever log in to is almost certainly a mistake, not
// intent, and it is better to fail loudly at startup than have someone
// file a bug report about a login that just does not work.
//
// Unknown YAML keys are also a hard error, the same way
// config.LoadSystemConfig treats them for its own configuration file. A
// misspelled field name in this file would otherwise be silently dropped
// rather than erroring, which is a worse mistake here than almost anywhere
// else in this project, since it would look like a secret was configured
// when it was not.
func LoadUsers(path string) (Users, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading users file %q: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var raw yamlUsersFile
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			// The file is empty, or contains comments only. An empty user
			// database is unusual but not itself an error. LoadUsers's
			// caller only runs at all when AuthRequired is true, so an
			// operator who gets this far almost certainly wants a real
			// error surfaced once they try to log in against zero users,
			// not a startup crash over an empty file specifically.
			return Users{}, nil
		}
		return nil, fmt.Errorf("error parsing users file %q: %w", path, err)
	}

	// A users file is expected to be a single top-level YAML mapping, the
	// same as config.LoadSystemConfig, so a second document here is
	// rejected.
	var extra yamlUsersFile
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("users file %q contains multiple YAML documents", path)
		}
		return nil, fmt.Errorf("error parsing users file %q: %w", path, err)
	}

	users := make(Users, len(raw.Users))
	for name, u := range raw.Users {
		if u.PasswordHash == "" {
			return nil, fmt.Errorf("user %q in %q has no password set", name, path)
		}
		uu := u
		uu.Username = name
		users[name] = &uu
	}
	return users, nil
}

// SaveUsers - This function writes users back to path, under the same
// "users:" key LoadUsers reads. It is what lets a running session persist
// a change, rather than needing an administrator to hand edit the file and
// restart.
//
// The write is atomic. A temporary file in the same directory is renamed
// over path, so an interrupted write or a full disk never leaves a
// half-written file for the next startup.
//
// The whole file is rewritten from users, not only the changed entry.
// Comments and formatting in a hand edited file are not preserved. Keeping
// the write unconditionally whole keeps the file's shape predictable.
func SaveUsers(path string, users Users) error {
	raw := yamlUsersFile{Users: make(map[string]User, len(users))}
	for name, u := range users {
		raw.Users[name] = *u
	}

	data, err := yaml.Marshal(&raw)
	if err != nil {
		return fmt.Errorf("error encoding users file %q: %w", path, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".users-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("error creating temporary file for users file %q: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // No-op once the rename below succeeds.

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("error writing users file %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("error writing users file %q: %w", path, err)
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("error setting permissions on users file %q: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("error saving users file %q: %w", path, err)
	}
	return nil
}
