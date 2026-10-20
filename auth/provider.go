// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"fmt"
)

// ----------------------------------------------------------------------
// Public Methods - LocalProvider
// ----------------------------------------------------------------------

// Authenticate - This method implements Provider for LocalProvider.
//
// A nonexistent username still runs a real comparison, through the default
// PasswordHasher's Dummy value, before returning. Without that, the time
// taken would reveal whether a username exists.
//
// It goes through the default hasher rather than calling bcrypt directly.
// A fixed bcrypt comparison would burn the wrong amount of time once real
// logins are checked against a different algorithm, quietly reopening the
// side channel this closes.
func (p *LocalProvider) Authenticate(username, password string) (bool, error) {
	u, ok := p.Users[username]
	if !ok {
		defaultPasswordHasher.Verify(defaultPasswordHasher.Dummy(), password)
		return false, nil
	}
	return VerifyPassword(u.PasswordHash, password), nil
}

// ----------------------------------------------------------------------
// Public Functions - Provider
// ----------------------------------------------------------------------

// NewProvider - This function builds the Provider a
// config.SystemConfig.AuthProviders entry's Type names, "local" being the
// only recognized value today. An unrecognized Type is an error rather
// than something silently ignored, the same fail loudly convention every
// other malformed setting in this project follows, since a typo'd Type
// would otherwise mean a deployment believes it is checking passwords
// against a backend that does not exist. users is only meaningful for the
// "local" Type; a future Type, an LDAP or a RADIUS backend for instance,
// would take whatever connection details its own config.AuthProviderConfig
// fields eventually carry instead.
func NewProvider(providerType string, users Users) (Provider, error) {
	switch providerType {
	case "local":
		return NewLocalProvider(users), nil
	default:
		return nil, fmt.Errorf("unrecognized authentication provider type %q", providerType)
	}
}
