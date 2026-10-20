// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

//go:build linux

package daemon

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// checkPeerCredential - This function reads conn's peer credential from
// the kernel through SO_PEERCRED, which gives the real UID, GID, and PID
// of the process on the other end.
//
// The kernel sets those at connect time. A connecting process cannot forge
// them by sending different values, and nothing here reads conn's
// application data.
//
// That is why this check can, and MUST, run before any byte of the
// protocol above it is trusted.
func checkPeerCredential(conn *net.UnixConn) (PeerCredential, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return PeerCredential{}, fmt.Errorf("daemon: obtaining a raw connection to read its peer credential: %w", err)
	}

	var cred *unix.Ucred
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ctrlErr != nil {
		return PeerCredential{}, fmt.Errorf("daemon: reading SO_PEERCRED: %w", ctrlErr)
	}
	if sockErr != nil {
		return PeerCredential{}, fmt.Errorf("daemon: reading SO_PEERCRED: %w", sockErr)
	}

	return PeerCredential{UID: cred.Uid, GID: cred.Gid, PID: cred.Pid}, nil
}
