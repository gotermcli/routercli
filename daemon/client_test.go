// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"errors"
	"sync"
	"testing"

	"github.com/gotermcli/routercli/auth"
	"github.com/gotermcli/routercli/command"
	"github.com/gotermcli/routercli/config"
)

// newTestStandaloneClient returns a ready to use StandaloneClient whose
// initial state mirrors the same shape state_test.go's tests already
// build, one fakeProductState, one seeded admin user, one empty RoleSet
// and TreeStructure, and a SystemConfig with AuthRequired true, kept small
// and local to this file so this test does not need to reach outside this
// package for a realistic State.
func newTestStandaloneClient(t *testing.T) *StandaloneClient {
	t.Helper()
	initial := NewState(
		&fakeProductState{Hostname: "unset"},
		&command.TreeStructure{ByName: map[string]*command.CommandLevel{}},
		auth.Users{"admin": &auth.User{Username: "admin", PasswordHash: "$0$fake", Roles: []string{"admin"}}},
		&command.RoleSet{ByName: map[string]*command.Role{}},
		&config.SystemConfig{AuthRequired: true},
	)
	return NewStandaloneClient(initial)
}

// TestStandaloneClientMutateProductStateReadsAndWrites - This test
// verifies MutateProductState reaches the exact ProductState value
// NewStandaloneClient was constructed with, and that a mutation one call
// makes is visible to the next, the same "one Store behind it" property
// store_test.go already establishes for Store.Do itself.
func TestStandaloneClientMutateProductStateReadsAndWrites(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	got, err := c.MutateProductState(func(ps any) (any, error) {
		return ps.(*fakeProductState).Hostname, nil
	})
	if err != nil {
		t.Fatalf("MutateProductState (read): %v", err)
	}
	if got.(string) != "unset" {
		t.Errorf("Hostname = %q, want %q", got, "unset")
	}

	_, err = c.MutateProductState(func(ps any) (any, error) {
		ps.(*fakeProductState).Hostname = "core1"
		return nil, nil
	})
	if err != nil {
		t.Fatalf("MutateProductState (write): %v", err)
	}

	got, err = c.MutateProductState(func(ps any) (any, error) {
		return ps.(*fakeProductState).Hostname, nil
	})
	if err != nil {
		t.Fatalf("MutateProductState (read again): %v", err)
	}
	if got.(string) != "core1" {
		t.Errorf("Hostname after mutation = %q, want %q", got, "core1")
	}
}

// TestStandaloneClientMutateLevelsReadsAndWrites - This test verifies
// MutateLevels reaches the real *command.TreeStructure this client was
// constructed with, and that a level added by one call is visible to a
// later one.
func TestStandaloneClientMutateLevelsReadsAndWrites(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	_, err := c.MutateLevels(func(levels *command.TreeStructure) (any, error) {
		levels.ByName["config"] = &command.CommandLevel{Name: "config"}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("MutateLevels (write): %v", err)
	}

	got, err := c.MutateLevels(func(levels *command.TreeStructure) (any, error) {
		_, ok := levels.ByName["config"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("MutateLevels (read): %v", err)
	}
	if !got.(bool) {
		t.Error("expected the level added by a previous MutateLevels call to be visible to a later one")
	}
}

// TestStandaloneClientMutateUsersReadsAndWrites - This test verifies
// MutateUsers reaches the real auth.Users this client was constructed
// with, both the account it started with and one added afterward.
func TestStandaloneClientMutateUsersReadsAndWrites(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	got, err := c.MutateUsers(func(users auth.Users) (any, error) {
		_, ok := users["admin"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("MutateUsers (read seeded admin): %v", err)
	}
	if !got.(bool) {
		t.Error("expected the seeded admin user to already be present")
	}

	_, err = c.MutateUsers(func(users auth.Users) (any, error) {
		users["betty"] = &auth.User{Username: "betty", PasswordHash: "$0$fake2"}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("MutateUsers (add betty): %v", err)
	}

	got, err = c.MutateUsers(func(users auth.Users) (any, error) {
		_, ok := users["betty"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("MutateUsers (read betty): %v", err)
	}
	if !got.(bool) {
		t.Error("expected betty, added by a previous MutateUsers call, to be visible to a later one")
	}
}

// TestStandaloneClientMutateRolesReadsAndWrites - This test verifies
// MutateRoles reaches the real *command.RoleSet this client was
// constructed with, mirroring the same read after write shape the other
// three Mutate methods above are each tested with.
func TestStandaloneClientMutateRolesReadsAndWrites(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	_, err := c.MutateRoles(func(roles *command.RoleSet) (any, error) {
		roles.ByName["operator"] = &command.Role{Name: "operator"}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("MutateRoles (write): %v", err)
	}

	got, err := c.MutateRoles(func(roles *command.RoleSet) (any, error) {
		_, ok := roles.ByName["operator"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("MutateRoles (read): %v", err)
	}
	if !got.(bool) {
		t.Error("expected the role added by a previous MutateRoles call to be visible to a later one")
	}
}

// TestStandaloneClientReplaceStateReplacesStateWholesale - This test
// verifies ReplaceState behaves exactly as its own doc comment describes,
// the entire canonical state is replaced outright, not merged field by
// field, so a value only present in the old state, the seeded admin user
// here, does not survive a ReplaceState call to an otherwise empty new
// State. See ReplaceState's doc comment in client.go for why this method
// carries a different name from command.DaemonClient's, unrelated, trigger
// only Reboot method.
func TestStandaloneClientReplaceStateReplacesStateWholesale(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	newState := NewState(
		&fakeProductState{Hostname: "post-reboot"},
		&command.TreeStructure{ByName: map[string]*command.CommandLevel{}},
		auth.Users{"root": &auth.User{Username: "root", PasswordHash: "$0$fake3"}},
		&command.RoleSet{ByName: map[string]*command.Role{}},
		&config.SystemConfig{AuthRequired: false},
	)

	if err := c.ReplaceState(newState); err != nil {
		t.Fatalf("ReplaceState: %v", err)
	}

	got, err := c.MutateProductState(func(ps any) (any, error) {
		return ps.(*fakeProductState).Hostname, nil
	})
	if err != nil {
		t.Fatalf("MutateProductState after ReplaceState: %v", err)
	}
	if got.(string) != "post-reboot" {
		t.Errorf("Hostname after ReplaceState = %q, want %q", got, "post-reboot")
	}

	got, err = c.MutateUsers(func(users auth.Users) (any, error) {
		_, adminStillPresent := users["admin"]
		_, rootPresent := users["root"]
		return adminStillPresent == false && rootPresent, nil
	})
	if err != nil {
		t.Fatalf("MutateUsers after ReplaceState: %v", err)
	}
	if !got.(bool) {
		t.Error("expected ReplaceState to replace Users wholesale: admin from the old state gone, root from newState present")
	}
}

// TestStandaloneClientCommandDaemonClientExtrasReportErrDaemonNotConfigured
// - This test verifies ListUsers, DisconnectUser, and Reboot, the remote
// command.DaemonClient methods, all report command.ErrDaemonNotConfigured
// against a StandaloneClient, and that FarewellChannel returns a nil
// channel, matching every one of those four methods' own doc comments:
// standalone mode has no daemon and no session registry at all.
func TestStandaloneClientCommandDaemonClientExtrasReportErrDaemonNotConfigured(t *testing.T) {
	c := newTestStandaloneClient(t)
	defer c.Close()

	if _, err := c.ListUsers(); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("ListUsers returned %v, want command.ErrDaemonNotConfigured", err)
	}
	if err := c.DisconnectUser("alice", ""); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("DisconnectUser returned %v, want command.ErrDaemonNotConfigured", err)
	}
	if err := c.Reboot(); !errors.Is(err, command.ErrDaemonNotConfigured) {
		t.Errorf("Reboot returned %v, want command.ErrDaemonNotConfigured", err)
	}
	if ch := c.FarewellChannel(); ch != nil {
		t.Errorf("FarewellChannel returned %v, want nil", ch)
	}
}

// TestStandaloneClientCloseStopsFurtherUse - This test verifies Close
// stops the underlying Store, so a call made after Close fails with
// ErrStoreClosed rather than hanging or silently succeeding against a
// store nothing is servicing any longer, the same contract store_test.go's
// TestStoreDoAfterCloseReturnsErrStoreClosed already establishes for
// Store.Do directly.
func TestStandaloneClientCloseStopsFurtherUse(t *testing.T) {
	c := newTestStandaloneClient(t)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := c.MutateProductState(func(ps any) (any, error) { return nil, nil })
	if err != ErrStoreClosed {
		t.Errorf("MutateProductState after Close = %v, want ErrStoreClosed", err)
	}
}

// TestStandaloneClientCloseIsSafeToCallRepeatedly - This test verifies
// Close does not panic, or otherwise misbehave, when called more than
// once, matching Store.Close's repeated close safety, which
// StandaloneClient's Close delegates to.
func TestStandaloneClientCloseIsSafeToCallRepeatedly(t *testing.T) {
	c := newTestStandaloneClient(t)

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestStandaloneClientConcurrentMutateProductStateNeverLosesAnUpdate -This
// test proves StandaloneClient carries Store's single writer safety all
// the way through a Client's public interface, the whole reason
// StandaloneClient wraps a Store rather than a bare value: many goroutines
// incrementing the same counter, entirely through MutateProductState, must
// never lose a single update.
func TestStandaloneClientConcurrentMutateProductStateNeverLosesAnUpdate(t *testing.T) {
	type counter struct{ N int }
	c := NewStandaloneClient(NewState(
		&counter{},
		&command.TreeStructure{ByName: map[string]*command.CommandLevel{}},
		auth.Users{},
		&command.RoleSet{ByName: map[string]*command.Role{}},
		&config.SystemConfig{},
	))
	defer c.Close()

	const goroutines = 100
	const incrementsEach = 25
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsEach; j++ {
				_, err := c.MutateProductState(func(ps any) (any, error) {
					ps.(*counter).N++
					return nil, nil
				})
				if err != nil {
					t.Errorf("MutateProductState: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got, err := c.MutateProductState(func(ps any) (any, error) {
		return ps.(*counter).N, nil
	})
	if err != nil {
		t.Fatalf("MutateProductState (final read): %v", err)
	}
	want := goroutines * incrementsEach
	if got.(int) != want {
		t.Errorf("final counter = %d, want %d", got, want)
	}
}
