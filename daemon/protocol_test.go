// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package daemon

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// unmarshalPayload is a small test helper decoding body into T, letting
// each round trip subtest above stay one line instead of repeating `var p
// T; err := json.Unmarshal(body, &p)` at every call site.
func unmarshalPayload[T any](body []byte) (T, error) {
	var v T
	err := json.Unmarshal(body, &v)
	return v, err
}

// mustEncode is a small test helper building one of this fuzz test's seed
// corpus entries, failing the test outright if EncodeMessage itself cannot
// encode a payload this file already defines, which would mean this test's
// seed corpus is broken, not the code under test.
func mustEncode(tb testing.TB, kind MessageKind, payload any) []byte {
	tb.Helper()
	raw, err := EncodeMessage(kind, payload)
	if err != nil {
		tb.Fatalf("mustEncode(%s): %v", kind, err)
	}
	return raw
}

// TestEncodeDecodeMessageRoundTrips - This test verifies that every
// payload type this file defines, encoded through EncodeMessage and
// decoded back through DecodeMessage plus the caller's json.Unmarshal,
// produces the exact same MessageKind and an equivalent payload, one
// subtest per kind in the catalog.
func TestEncodeDecodeMessageRoundTrips(t *testing.T) {
	t.Run("Hello", func(t *testing.T) {
		want := HelloPayload{Username: "alice", PID: 4242, Terminal: "pts/3"}
		raw, err := EncodeMessage(KindHello, want)
		if err != nil {
			t.Fatalf("EncodeMessage: %v", err)
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			t.Fatalf("DecodeMessage: %v", err)
		}
		if kind != KindHello {
			t.Fatalf("kind = %s, want %s", kind, KindHello)
		}
		got, err := unmarshalPayload[HelloPayload](body)
		if err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if got != want {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
	})

	t.Run("AuditEvent", func(t *testing.T) {
		when := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
		want := AuditEventPayload{Username: "bob", Command: "show version", Level: "exec", Time: when, Success: true}
		raw, err := EncodeMessage(KindAuditEvent, want)
		if err != nil {
			t.Fatalf("EncodeMessage: %v", err)
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			t.Fatalf("DecodeMessage: %v", err)
		}
		if kind != KindAuditEvent {
			t.Fatalf("kind = %s, want %s", kind, KindAuditEvent)
		}
		got, err := unmarshalPayload[AuditEventPayload](body)
		if err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if !got.Time.Equal(want.Time) || got.Username != want.Username || got.Command != want.Command || got.Level != want.Level || got.Success != want.Success {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
	})

	t.Run("ListUsersResponse", func(t *testing.T) {
		want := ListUsersResponsePayload{Sessions: []SessionInfo{
			{ID: "abc123", Username: "alice", CommandLevel: "exec", ConnectedAt: time.Now().UTC(), LastActivity: time.Now().UTC()},
		}}
		raw, err := EncodeMessage(KindListUsersResponse, want)
		if err != nil {
			t.Fatalf("EncodeMessage: %v", err)
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			t.Fatalf("DecodeMessage: %v", err)
		}
		if kind != KindListUsersResponse {
			t.Fatalf("kind = %s, want %s", kind, KindListUsersResponse)
		}
		got, err := unmarshalPayload[ListUsersResponsePayload](body)
		if err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(got.Sessions) != 1 || got.Sessions[0].ID != "abc123" {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
	})

	t.Run("DisconnectUserRequest", func(t *testing.T) {
		want := DisconnectUserRequestPayload{Username: "alice", SessionID: "abc123"}
		raw, err := EncodeMessage(KindDisconnectUserRequest, want)
		if err != nil {
			t.Fatalf("EncodeMessage: %v", err)
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			t.Fatalf("DecodeMessage: %v", err)
		}
		if kind != KindDisconnectUserRequest {
			t.Fatalf("kind = %s, want %s", kind, KindDisconnectUserRequest)
		}
		got, err := unmarshalPayload[DisconnectUserRequestPayload](body)
		if err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if got != want {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
	})

	t.Run("Farewell", func(t *testing.T) {
		want := FarewellPayload{Text: FarewellRebooting}
		raw, err := EncodeMessage(KindFarewell, want)
		if err != nil {
			t.Fatalf("EncodeMessage: %v", err)
		}
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			t.Fatalf("DecodeMessage: %v", err)
		}
		if kind != KindFarewell {
			t.Fatalf("kind = %s, want %s", kind, KindFarewell)
		}
		got, err := unmarshalPayload[FarewellPayload](body)
		if err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if got != want {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
	})

	t.Run("no-payload kinds encode an empty JSON object", func(t *testing.T) {
		for _, kind := range []MessageKind{KindGoodbye, KindListUsersRequest, KindRebootRequest} {
			raw, err := EncodeMessage(kind, struct{}{})
			if err != nil {
				t.Fatalf("EncodeMessage(%s): %v", kind, err)
			}
			gotKind, body, err := DecodeMessage(raw)
			if err != nil {
				t.Fatalf("DecodeMessage(%s): %v", kind, err)
			}
			if gotKind != kind {
				t.Errorf("kind = %s, want %s", gotKind, kind)
			}
			if string(body) != "{}" {
				t.Errorf("body = %q, want {}", body)
			}
		}
	})
}

// TestDecodeMessageEmptyFrameReturnsErrMalformedMessage - This test
// verifies that DecodeMessage against a zero length frame, too short to
// carry even a one byte type tag, reports ErrMalformedMessage rather than
// panicking or returning a zero MessageKind silently.
func TestDecodeMessageEmptyFrameReturnsErrMalformedMessage(t *testing.T) {
	_, _, err := DecodeMessage(nil)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("DecodeMessage(nil) returned %v, want ErrMalformedMessage", err)
	}

	_, _, err = DecodeMessage([]byte{})
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("DecodeMessage([]byte{}) returned %v, want ErrMalformedMessage", err)
	}
}

// TestDecodeMessageUnknownKindStillReturnsTheRawTagByte - This test
// verifies that DecodeMessage itself never rejects a tag byte it does not
// recognize; validating the kind is a caller's job, see DecodeMessage's
// doc comment, and this confirms an unrecognized tag comes back as an
// ordinary MessageKind value rather than an error, so a caller's switch
// default branch is what handles it.
func TestDecodeMessageUnknownKindStillReturnsTheRawTagByte(t *testing.T) {
	kind, body, err := DecodeMessage([]byte{0xff, '{', '}'})
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if kind != MessageKind(0xff) {
		t.Errorf("kind = %v, want 0xff", kind)
	}
	if string(body) != "{}" {
		t.Errorf("body = %q, want {}", body)
	}
}

// TestMessageKindStringNamesEveryDefinedKind - This test verifies that
// every MessageKind constant this file defines has its own name in String,
// rather than falling through to the numeric default, so a log line or a
// test failure message naming one is always readable.
func TestMessageKindStringNamesEveryDefinedKind(t *testing.T) {
	kinds := []MessageKind{
		KindHello, KindHelloResponse, KindGoodbye, KindAuditEvent,
		KindListUsersRequest, KindListUsersResponse,
		KindDisconnectUserRequest, KindDisconnectUserResponse,
		KindRebootRequest, KindRebootResponse, KindFarewell,
	}
	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		name := k.String()
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			t.Errorf("MessageKind(%d).String() = %q, want a readable name", byte(k), name)
		}
		if seen[name] {
			t.Errorf("MessageKind(%d).String() = %q, collides with another kind's name", byte(k), name)
		}
		seen[name] = true
	}

	if got := MessageKind(0xff).String(); got != "MessageKind(255)" {
		t.Errorf("an unrecognized MessageKind's String() = %q, want MessageKind(255)", got)
	}
}

// FuzzDecodeMessageThenUnmarshalPayloads - This fuzz test feeds arbitrary
// byte slices to DecodeMessage and then, for whichever kind came back,
// attempts to json.Unmarshal the body into every payload type this file
// defines, confirming the testing requirement directly: "The wire
// protocol's decoder gets a Go native fuzz test... targeting truncated,
// oversized, and structurally malformed messages, confirming none of them
// panic the daemon." A malformed or truncated body is expected to produce
// an ordinary error from json.Unmarshal, never a panic; this test's only
// assertion is that nothing panics, run with `go test
// -fuzz=FuzzDecodeMessageThenUnmarshalPayloads`.
func FuzzDecodeMessageThenUnmarshalPayloads(f *testing.F) {
	seed := [][]byte{
		nil,
		{},
		{byte(KindHello)},
		{byte(KindHello), '{', '}'},
		mustEncode(f, KindHello, HelloPayload{Username: "alice", PID: 1, Terminal: "pts/0"}),
		mustEncode(f, KindAuditEvent, AuditEventPayload{Username: "bob", Command: "show version", Level: "exec", Time: time.Now(), Success: true}),
		mustEncode(f, KindListUsersResponse, ListUsersResponsePayload{}),
		mustEncode(f, KindDisconnectUserRequest, DisconnectUserRequestPayload{Username: "alice"}),
		mustEncode(f, KindFarewell, FarewellPayload{Text: FarewellRebooting}),
		{0xff, 0xff, 0xff, 0xff},
	}
	for _, s := range seed {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		kind, body, err := DecodeMessage(raw)
		if err != nil {
			return
		}
		// Attempting every payload type against whatever body came back,
		// regardless of kind, casts a wider net than "only try the one
		// payload type this kind is supposed to carry": a fuzzer mutating
		// both the tag byte and the body together can and will produce a
		// kind that does not match its own body's shape, and that mismatch
		// must still never panic.
		_, _ = unmarshalPayload[HelloPayload](body)
		_, _ = unmarshalPayload[HelloResponsePayload](body)
		_, _ = unmarshalPayload[AuditEventPayload](body)
		_, _ = unmarshalPayload[ListUsersResponsePayload](body)
		_, _ = unmarshalPayload[DisconnectUserRequestPayload](body)
		_, _ = unmarshalPayload[DisconnectUserResponsePayload](body)
		_, _ = unmarshalPayload[RebootResponsePayload](body)
		_, _ = unmarshalPayload[FarewellPayload](body)
		_ = kind.String()
	})
}
