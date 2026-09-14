// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"fmt"
	"time"

	osuser "os/user"
)

// ----------------------------------------------------------------------
// Public Functions - Host Authentication
// ----------------------------------------------------------------------

// SessionFromHostIdentity - A deployment reached over SSH has already had
// its Unix account authenticated by sshd, whether routercli is that
// account's login shell or reached through a ForceCommand. Prompting
// again would add nothing.
//
// This builds an authenticated Session from the operating system account
// the process runs as, with no password checked.
//
// Trusting that identity is sound because it is not client controlled. The
// operating system decided which account this process runs as before any
// part of routercli started.
//
// The Session carries the same name on Username and HostUsername. A caller
// that also runs a CLI login overwrites Username with whatever that
// resolves to, keeping HostUsername and HostConnectedAt, since those
// describe how the connection arrived rather than who is using it.
func SessionFromHostIdentity() (*Session, error) {
	u, err := osuser.Current()
	if err != nil {
		return nil, fmt.Errorf("error reading host account identity: %w", err)
	}

	now := time.Now()
	return &Session{
		Username:        u.Username,
		Authenticated:   true,
		HostUsername:    u.Username,
		HostConnectedAt: now,
	}, nil
}
