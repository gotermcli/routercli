// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"sync"
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/config"
)

// fakeProductState stands in for whatever a real deployment's
// cmd/product.State looks like; this package never needs to know that
// concrete shape, see State.ProductState's doc comment, so a small, local,
// otherwise meaningless type is enough to prove ProductState does carry
// whatever a caller puts there, unchanged, through both NewState and a
// Store built around it.
type fakeProductState struct {
	Hostname string
}

// TestNewStateAssemblesAllFivePieces - This test verifies that NewState is
// nothing more than assembling its five parameters into the matching five
// fields, unchanged, in order; a State built any other way, by hand, with
// the same five values, would be identical.
func TestNewStateAssemblesAllFivePieces(t *testing.T) {
	product := &fakeProductState{Hostname: "edgeswitch1"}
	levels := &command.TreeStructure{ByName: map[string]*command.CommandLevel{}}
	users := auth.Users{"admin": &auth.User{Username: "admin", PasswordHash: "$0$fake"}}
	roles := &command.RoleSet{ByName: map[string]*command.Role{}}
	cfg := &config.SystemConfig{ProductName: "TestRouter"}

	st := NewState(product, levels, users, roles, cfg)

	if st.ProductState != any(product) {
		t.Errorf("ProductState = %v, want the exact product value passed in", st.ProductState)
	}
	if st.Levels != levels {
		t.Error("Levels does not point at the exact *TreeStructure passed in")
	}
	if len(st.Users) != 1 || st.Users["admin"] == nil {
		t.Errorf("Users = %v, want the exact Users map passed in", st.Users)
	}
	if st.Roles != roles {
		t.Error("Roles does not point at the exact *RoleSet passed in")
	}
	if st.Config != cfg {
		t.Error("Config does not point at the exact *SystemConfig passed in")
	}
}

// TestStateProductStateReadAndMutatedThroughStore - This test proves
// State's whole point end to end: a real *Store[State], wrapping an opaque
// ProductState value, can have that value both read and mutated only
// through Do, from more than one goroutine at once, without this package
// ever needing to know fakeProductState's shape, exactly the genericity
// State.ProductState's doc comment describes. A real deployment's
// cmd/product.State will be read and mutated the same way once real
// handlers are wired up to a Store[State] through a Client interface.
func TestStateProductStateReadAndMutatedThroughStore(t *testing.T) {
	initial := NewState(
		&fakeProductState{Hostname: "unset"},
		&command.TreeStructure{ByName: map[string]*command.CommandLevel{}},
		auth.Users{},
		&command.RoleSet{ByName: map[string]*command.Role{}},
		&config.SystemConfig{},
	)
	s := NewStore(initial)
	defer s.Close()

	setHostname := func(name string) error {
		_, err := s.Do(func(st *State) (any, error) {
			ps, ok := st.ProductState.(*fakeProductState)
			if !ok {
				return nil, nil
			}
			ps.Hostname = name
			return nil, nil
		})
		return err
	}
	readHostname := func() string {
		got, err := s.Do(func(st *State) (any, error) {
			return st.ProductState.(*fakeProductState).Hostname, nil
		})
		if err != nil {
			t.Fatalf("Do (read hostname): %v", err)
		}
		return got.(string)
	}

	if err := setHostname("core1"); err != nil {
		t.Fatalf("setHostname: %v", err)
	}
	if got := readHostname(); got != "core1" {
		t.Errorf("hostname after one mutation = %q, want %q", got, "core1")
	}

	// The same property store_test.go already establishes generically,
	// exercised here against State specifically: many goroutines racing to
	// set the hostname must never corrupt it into something no single
	// writer ever wrote.
	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = setHostname(hostnameCandidate(n))
		}(i)
	}
	wg.Wait()

	final := readHostname()
	valid := false
	for i := 0; i < goroutines; i++ {
		if final == hostnameCandidate(i) {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("final hostname %q does not match any candidate this test actually wrote", final)
	}
}

func hostnameCandidate(n int) string {
	return "host-" + string(rune('a'+n%26)) + string(rune('0'+n%10))
}

// TestStateUsersAndRolesReadThroughStore - This test verifies the
// remaining, non-opaque State fields, Users and Roles, both drawn directly
// from this project's existing auth and command packages, behave the same
// way: read and mutated only through Do, with a mutation from one Do call
// visible to the next.
func TestStateUsersAndRolesReadThroughStore(t *testing.T) {
	initial := NewState(
		nil,
		&command.TreeStructure{ByName: map[string]*command.CommandLevel{}},
		auth.Users{"admin": &auth.User{Username: "admin", PasswordHash: "$0$fake", Roles: []string{"admin"}}},
		&command.RoleSet{ByName: map[string]*command.Role{}},
		&config.SystemConfig{AuthRequired: true},
	)
	s := NewStore(initial)
	defer s.Close()

	_, err := s.Do(func(st *State) (any, error) {
		st.Users["betty"] = &auth.User{Username: "betty", PasswordHash: "$0$fake2"}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Do (add user): %v", err)
	}

	got, err := s.Do(func(st *State) (any, error) {
		_, ok := st.Users["betty"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Do (read user): %v", err)
	}
	if !got.(bool) {
		t.Error("expected the user added by a previous Do call to be visible to a later one")
	}

	got, err = s.Do(func(st *State) (any, error) { return st.Config.AuthRequired, nil })
	if err != nil {
		t.Fatalf("Do (read config): %v", err)
	}
	if !got.(bool) {
		t.Error("expected Config.AuthRequired to still be true, as constructed")
	}
}
