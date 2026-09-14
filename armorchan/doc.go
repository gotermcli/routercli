// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

/*
Package armorchan implements the encrypted, authenticated channel that the
RouterCLI daemon and its CLI clients speak over a local Unix domain
socket. Nothing here is a Cisco or HP concept. It is a small general
purpose transport, usable anywhere two local processes need a server
authenticated, encrypted, ordered channel above a raw connection.

This package depends on nothing beyond the Go standard library. Key
exchange uses crypto/ecdh with X25519. Key derivation uses crypto/hkdf,
RFC 5869. Encryption uses crypto/aes and crypto/cipher with AES-256-GCM.
The handshake that ties these primitives together is modeled on the TLS
1.3 key schedule, RFC 9846.

A hand rolled protocol carries more residual risk than a widely used,
independently audited library, even when it is built from sound standard
library primitives and follows a standards derived shape. Fewer people
have examined this particular arrangement. This package answers that with
known answer, tamper, replay, and nonce reuse tests. An outside review by
someone other than the author is strongly recommended before RouterCLI
relies on this in a real deployment.

# The Handshake

The daemon holds one persisted static X25519 key pair. Its public half is
distributed to clients out of band, most simply as a world readable file
the daemon writes at startup. This package accepts that public key as a
parameter and reads no files itself, which keeps key distribution the
concern of whatever wires this package into a real socket.

Each connecting client generates a fresh ephemeral X25519 key pair for
that one connection. The client's identity does not need proving at this
layer, because SO_PEERCRED on the Unix domain socket already established
which local user is connecting before any byte of this handshake is
exchanged. Authenticating the daemon to the client is the one property
this handshake provides.

The exchange runs as follows:

 1. The client sends its ephemeral public key.
 2. The daemon derives a shared secret from its static private key and
    that ephemeral public key, folds it through HKDF bound to a
    transcript hash covering both public keys and a fixed protocol
    label, and returns its static public key alongside a confirmation
    record encrypted under a key derived from that secret.
 3. The client checks the advertised static public key against the one it
    already holds, then decrypts the confirmation record with its own
    independently derived key.

Successful decryption is the proof. Nothing else on the machine could
produce a record decryptable under a key derived from the private half of
the daemon's known static public key.

# The Channel

A successful handshake produces a Channel holding two independent
AES-256-GCM ciphers and two independent nonce counters, one of each per
direction.

Every nonce is a per direction base value XORed with a monotonically
increasing counter, the same construction TLS 1.3 record protection uses,
RFC 9846 Section 5.3. Nonces are never reused and never randomly
generated. The counter value is also bound into each record's additional
authenticated data, so a record captured and replayed later in the same
connection fails its authentication tag against the receiver's already
advanced counter.

Any authentication failure permanently faults the Channel. A faulted
Channel MUST be treated as fatal to that connection, and the connection
MUST be closed.
*/
package armorchan
