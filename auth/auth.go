// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auth

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// ----------------------------------------------------------------------
// Define Storage Format
// ----------------------------------------------------------------------

// Password hashes are stored as "$<id>$<encoded>". The crypt ID convention
// used here is 0 for plaintext and 6 for bcrypt. A project registering its
// own PasswordHasher, see RegisterPasswordHasher below, picks its own
// unused id.
const (
	// cryptIDPlaintext - This constant stores the password with no hashing
	// at all. This exists only for local development and testing
	// convenience, and must never be used for a real deployment.
	// HashPassword never produces this. Only VerifyPassword understands
	// it.
	cryptIDPlaintext = "0"

	// cryptIDBcrypt names bcrypt rather than something like HMAC-SHA512.
	// A general purpose hash has no work factor, so it is cheap to brute
	// force at scale, which is exactly wrong for password storage. bcrypt
	// has a tunable cost.
	//
	// The salt is not stored separately; bcrypt embeds it. This is the
	// shipped default, not something this package requires.
	cryptIDBcrypt = "6"

	// bcryptCost - This constant is the bcrypt work factor RouterCLI's
	// shipped bcryptHasher uses by default, broken out as a named constant
	// so it can be adjusted over time.
	bcryptCost = bcrypt.DefaultCost
)

// ----------------------------------------------------------------------
// Define Crypto Agility
// ----------------------------------------------------------------------

// PasswordHasher - This interface is one algorithm that can produce and
// check a password's stored form.
//
// RouterCLI ships bcrypt as the default, but nothing in this package, or
// in anything calling HashPassword or VerifyPassword, is written against
// bcrypt specifically.
//
// A deployment wanting a different algorithm implements this, calls
// RegisterPasswordHasher at startup, and optionally
// SetDefaultPasswordHasher to make new hashes use it.
//
// Every stored hash keeps verifying whatever the current default is, since
// VerifyPassword dispatches on the id embedded in the stored value rather
// than on what HashPassword would produce now.
type PasswordHasher interface {
	// CryptID returns this hasher's storage format identifier, the "id"
	// segment of a stored "$id$encoded" password. This must be stable for
	// the life of a deployment; changing it for an algorithm already in
	// use would strand every password already hashed under the old id.
	CryptID() string

	// Hash returns plaintext hashed and encoded for storage, the part that
	// goes after "$id$". It does not include the "$id$" prefix itself;
	// HashPassword adds that.
	Hash(plaintext string) (string, error)

	// Verify reports whether candidate matches encoded, this hasher's
	// encoded form with the surrounding "$id$" already stripped off.
	Verify(encoded, candidate string) bool

	// Dummy returns a fixed valid encoded value in this hasher's format,
	// for feeding back into Verify purely to burn the same CPU time a real
	// comparison would.
	//
	// LocalProvider.Authenticate calls it when a username does not exist,
	// so timing never reveals whether one does. Whatever plaintext it may
	// decode to is never compared against anything a user could type.
	Dummy() string
}

// passwordHashers is the registry RegisterPasswordHasher adds to and
// VerifyPassword reads from, keyed by each hasher's CryptID. Populated at
// package init with RouterCLI's shipped bcrypt and plaintext
// implementations below, so a project that registers nothing of its own
// still has working password hashing.
var passwordHashers = map[string]PasswordHasher{}

// defaultPasswordHasher is which registered PasswordHasher HashPassword
// uses to produce a brand new hash. Starts as RouterCLI's bcryptHasher,
// see init below, changed only by a direct call to
// SetDefaultPasswordHasher.
var defaultPasswordHasher PasswordHasher

func init() {
	RegisterPasswordHasher(plaintextHasher{})
	RegisterPasswordHasher(NewBcryptHasher(bcryptCost))
	if err := SetDefaultPasswordHasher(cryptIDBcrypt); err != nil {
		// Unreachable: the line directly above this one just registered
		// exactly this id. Panicking here would only ever catch a
		// programming error in this file itself, never a runtime or
		// deployment condition.
		panic(err)
	}
}

// RegisterPasswordHasher - This function adds h to the set VerifyPassword
// can dispatch to, keyed by h.CryptID(). Calling this a second time for an
// id that is already registered replaces the previous entry, which lets a
// project override RouterCLI's shipped bcryptHasher, a different cost for
// instance, by registering a new one under the same "6" id, though
// registering a different algorithm entirely under a new, unused id is the
// more common reason to call this.
func RegisterPasswordHasher(h PasswordHasher) {
	passwordHashers[h.CryptID()] = h
}

// SetDefaultPasswordHasher - This function changes which registered
// PasswordHasher HashPassword uses to produce a brand new hash from here
// on. It returns an error, and changes nothing, if cryptID has not been
// registered through RegisterPasswordHasher first. This has no effect on
// verifying any password already hashed under a different id;
// VerifyPassword always dispatches on the id already embedded in the
// stored value, never on whichever hasher is currently the default.
func SetDefaultPasswordHasher(cryptID string) error {
	h, ok := passwordHashers[cryptID]
	if !ok {
		return fmt.Errorf("no PasswordHasher registered for crypt id %q; call RegisterPasswordHasher first", cryptID)
	}
	defaultPasswordHasher = h
	return nil
}

// NewBcryptHasher - This function returns RouterCLI's shipped
// PasswordHasher, bcrypt at the given cost. RegisterPasswordHasher is
// called with cost bcryptCost automatically at package init; a project
// wanting a different cost calls this directly and registers the result
// itself, under the same "6" id to replace the default outright, or under
// a new id to offer both side by side.
func NewBcryptHasher(cost int) PasswordHasher {
	return bcryptHasher{cost: cost}
}

// bcryptHasher - This type is RouterCLI's shipped PasswordHasher,
// unchanged in behavior from this package's original, single algorithm
// design. See NewBcryptHasher above.
type bcryptHasher struct {
	cost int
}

func (b bcryptHasher) CryptID() string { return cryptIDBcrypt }

func (b bcryptHasher) Hash(plaintext string) (string, error) {
	encoded, err := bcrypt.GenerateFromPassword([]byte(plaintext), b.cost)
	if err != nil {
		return "", fmt.Errorf("error hashing password: %w", err)
	}
	return string(encoded), nil
}

func (b bcryptHasher) Verify(encoded, candidate string) bool {
	return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(candidate)) == nil
}

// Dummy returns dummyBcryptHash's encoded half, defined alongside its full
// doc comment in login.go, with the "$6$" this package's storage format
// would wrap it in already stripped off, since Dummy only ever returns the
// encoded half, matching Hash and Verify above.
func (b bcryptHasher) Dummy() string {
	_, encoded, _ := splitPasswordString(dummyBcryptHash)
	return encoded
}

// plaintextHasher - This type backs cryptIDPlaintext. Its own Hash method
// always fails, matching cryptIDPlaintext's doc comment: HashPassword must
// never produce this format itself, it exists only for a "$0$..." entry
// typed by hand directly into example/etc/users.yaml for local development and
// testing.
type plaintextHasher struct{}

func (plaintextHasher) CryptID() string { return cryptIDPlaintext }

func (plaintextHasher) Hash(string) (string, error) {
	return "", errors.New(`the plaintext crypt id must never be produced by HashPassword; it exists only for a "$0$..." entry entered by hand for local development and testing, never for a real deployment`)
}

func (plaintextHasher) Verify(encoded, candidate string) bool {
	return encoded == candidate
}

// Dummy returns a fixed placeholder. Plaintext comparison has no real work
// factor to protect regardless, so this exists only to satisfy
// PasswordHasher, not because timing matters here the way it does for a
// real algorithm.
func (plaintextHasher) Dummy() string {
	return "not-a-real-password"
}

// ----------------------------------------------------------------------
// Public Functions - Auth
// ----------------------------------------------------------------------

// HashPassword - This function hashes a plaintext password with the
// current default PasswordHasher and returns it in the "$<id>$<encoded>"
// storage format used by example/etc/users.yaml. The default is bcrypt unless a
// deployment has called SetDefaultPasswordHasher.
func HashPassword(plaintext string) (string, error) {
	encoded, err := defaultPasswordHasher.Hash(plaintext)
	if err != nil {
		return "", err
	}
	return "$" + defaultPasswordHasher.CryptID() + "$" + encoded, nil
}

// IsPlaintextHash - This function reports whether the stored password is
// in the plaintext, "$0$...", storage format rather than a real hash. It
// returns false for anything that does not parse as a "$id$encoded"
// string.
func IsPlaintextHash(stored string) bool {
	id, _, ok := splitPasswordString(stored)
	return ok && id == cryptIDPlaintext
}

// IsRecognizedHash - "password manager hash <hash>" accepts an already
// hashed secret, restoring one from saved configuration text rather than a
// typed password. There is no plaintext candidate, so VerifyPassword
// cannot sanity check it, but accepting any string unchecked would let an
// obvious mistake through, such as a plaintext password typed into a field
// meant for a hash.
//
// This reports whether stored is shaped like a "$id$encoded" hash whose id
// is registered, without verifying it against anything.
//
// This is not the same check as IsPlaintextHash. The plaintext form is
// itself a registered id, so a value in that form still reports true here.
// A caller wanting to reject it too calls IsPlaintextHash as well.
func IsRecognizedHash(stored string) bool {
	id, _, ok := splitPasswordString(stored)
	if !ok {
		return false
	}
	_, ok = passwordHashers[id]
	return ok
}

// VerifyPassword - This function checks a plaintext candidate against a
// stored "$id$encoded" hash, dispatching to whichever PasswordHasher is
// registered for that id.
//
// An unrecognized id is a verification failure, not an error. That covers
// both a corrupt or tampered stored value and a hash produced by an
// algorithm no longer registered. Either MUST deny access rather than
// crash the process or let something through.
func VerifyPassword(stored, candidate string) bool {
	id, encoded, ok := splitPasswordString(stored)
	if !ok {
		return false
	}
	h, ok := passwordHashers[id]
	if !ok {
		return false
	}
	return h.Verify(encoded, candidate)
}

// ----------------------------------------------------------------------
// Private Functions - Auth
// ----------------------------------------------------------------------

// splitPasswordString - This function splits a "$id$encoded" string into
// its two parts.
func splitPasswordString(stored string) (id, encoded string, ok bool) {
	if !strings.HasPrefix(stored, "$") {
		return "", "", false
	}
	parts := strings.SplitN(stored[1:], "$", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
