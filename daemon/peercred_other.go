// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

//go:build !linux

package daemon

import (
	"fmt"
	"net"
	"runtime"
)

// checkPeerCredential has no implementation on this platform. LOCAL_PEERCRED
// is the BSD family equivalent of Linux's SO_PEERCRED, but it has not been
// built and verified here.
//
// This package refuses outright on every platform other than Linux rather
// than accept a connection whose peer it cannot check.
//
// Listen still succeeds anywhere Go supports a Unix domain socket. Only
// Accept, through this function, refuses every connection until a real
// implementation lands.
func checkPeerCredential(conn *net.UnixConn) (PeerCredential, error) {
	return PeerCredential{}, fmt.Errorf("daemon: peer credential checking is not yet implemented on %s, see peercred_linux.go for the only platform currently supported", runtime.GOOS)
}
