// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"fmt"
	"net"
)

// PeerCredential is the operating system identity Listener.Accept found
// attached to one accepted connection, the real UID, GID, and process ID
// of whatever process is on the other end of the socket. This is kernel
// verified information, not anything the connecting process itself
// claimed; see checkPeerCredential's platform specific doc comment for
// exactly how it is obtained.
type PeerCredential struct {
	UID uint32
	GID uint32
	PID int32
}

// PeerCredentialChecker decides whether an accepted connection's
// PeerCredential is allowed to talk to this daemon at all, the "operating
// system level check first" half of the layered authentication a second,
// independent layer, armorchan's handshake, runs above whatever this check
// allows through, never in place of it.
type PeerCredentialChecker interface {
	// CheckPeer inspects conn's peer credential and returns nil if this
	// daemon should accept it, or a non-nil error, naming why, if it
	// should not. Implementations MUST perform this check using the
	// kernel's record of who holds the other end of conn, never anything
	// read from the connection's application data, since nothing has been
	// read from conn at all at the point this is called.
	CheckPeer(conn *net.UnixConn) error
}

// AllowedUIDs - This type is a PeerCredentialChecker that accepts a
// connection whenever its peer's real UID is a member of the set it was
// constructed with, and refuses every other UID outright.
//
// The requirement is to refuse any connecting process whose real UID is
// not the daemon's account, or another UID explicitly allowed.
type AllowedUIDs map[uint32]bool

// NewAllowedUIDs returns an AllowedUIDs accepting exactly the UIDs passed
// in, most commonly the daemon's effective UID alongside any explicitly
// configured extra accounts a deployment trusts to reach it locally.
func NewAllowedUIDs(uids ...uint32) AllowedUIDs {
	set := make(AllowedUIDs, len(uids))
	for _, uid := range uids {
		set[uid] = true
	}
	return set
}

// CheckPeer implements PeerCredentialChecker.
func (a AllowedUIDs) CheckPeer(conn *net.UnixConn) error {
	cred, err := checkPeerCredential(conn)
	if err != nil {
		return err
	}
	if !a[cred.UID] {
		return &peerNotAllowedError{uid: cred.UID}
	}
	return nil
}

// peerNotAllowedError is returned by AllowedUIDs.CheckPeer for a
// connection whose real UID was successfully read from the kernel but is
// not in the allowed set; kept as its own small type, rather than an ad
// hoc fmt.Errorf, so a caller that wants to tell "refused, wrong account"
// apart from "the platform level credential lookup itself failed" can do
// so directly rather than by inspecting an error string.
type peerNotAllowedError struct {
	uid uint32
}

func (e *peerNotAllowedError) Error() string {
	return fmt.Sprintf("daemon: connection refused, peer UID %d is not in the allowed set", e.uid)
}
