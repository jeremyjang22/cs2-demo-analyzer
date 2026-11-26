package auth

import (
	"strings"
	"testing"
	"time"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func TestRoundTrip(t *testing.T) {
	want := Session{SteamID: 76561198230376396, Expires: time.Now().Add(time.Hour).Truncate(time.Second)}
	got, err := Decode(key, Encode(key, want))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.SteamID != want.SteamID || !got.Expires.Equal(want.Expires) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// The whole point of signing. Without this check anyone could log in as anyone
// by editing the steamid in their own cookie.
func TestTamperedSteamIDRejected(t *testing.T) {
	raw := Encode(key, Session{SteamID: 1, Expires: time.Now().Add(time.Hour)})
	parts := strings.Split(raw, ".")
	forged := "76561198230376396." + parts[1] + "." + parts[2]

	if _, err := Decode(key, forged); err == nil {
		t.Fatal("a forged steamid was accepted")
	}
}

func TestWrongKeyRejected(t *testing.T) {
	raw := Encode(key, Session{SteamID: 42, Expires: time.Now().Add(time.Hour)})
	if _, err := Decode([]byte("ffffffffffffffffffffffffffffffff"), raw); err == nil {
		t.Fatal("a session signed with another key was accepted")
	}
}

func TestExpiredRejected(t *testing.T) {
	raw := Encode(key, Session{SteamID: 42, Expires: time.Now().Add(-time.Second)})
	if _, err := Decode(key, raw); err == nil {
		t.Fatal("an expired session was accepted")
	}
}

func TestMalformedRejected(t *testing.T) {
	for _, s := range []string{"", "a", "a.b", "a.b.c.d", "notanumber.123.sig"} {
		if _, err := Decode(key, s); err == nil {
			t.Errorf("Decode(%q) accepted a malformed session", s)
		}
	}
}

// The Cloudflare Worker verifies this cookie too, without calling the API, to
// decide who may read a private demo (packages/web/worker/session.ts). The two
// implementations share no code, so they share this vector instead: the same
// string is asserted in worker/session.test.ts. Change the format here and
// that test is the one that tells you the Worker now locks everyone out.
func TestEncodeMatchesWorkerVector(t *testing.T) {
	const want = "76561198230376396.4102444800.ZmtrVcXaMOlnOiKp3AXxjOyXOc5dYnTacJd9GogvYRI"
	got := Encode(key, Session{SteamID: 76561198230376396, Expires: time.Unix(4102444800, 0)})
	if got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
}
