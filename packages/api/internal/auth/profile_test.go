package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const me = uint64(76561198230376396)

func TestParseSummariesPicksTheAccount(t *testing.T) {
	body := `{"response":{"players":[
		{"steamid":"76561198000000000","personaname":"someone else"},
		{"steamid":"76561198230376396","personaname":"  solitude ",
		 "avatarmedium":"https://avatars.steamstatic.com/abc_medium.jpg"}]}}`
	p, err := parseSummaries([]byte(body), me)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "solitude" || p.Avatar != "https://avatars.steamstatic.com/abc_medium.jpg" {
		t.Errorf("got %+v", p)
	}
}

func TestParseSummariesRefusesWhatItCannotUse(t *testing.T) {
	for name, body := range map[string]string{
		"not listed": `{"response":{"players":[]}}`,
		"no name":    `{"response":{"players":[{"steamid":"76561198230376396","personaname":"  "}]}}`,
		"not json":   `<html>`,
	} {
		if _, err := parseSummaries([]byte(body), me); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseSummariesDropsANonHTTPSAvatar(t *testing.T) {
	body := `{"response":{"players":[{"steamid":"76561198230376396","personaname":"x",
		"avatarmedium":"javascript:alert(1)"}]}}`
	p, err := parseSummaries([]byte(body), me)
	if err != nil || p.Avatar != "" {
		t.Errorf("got %+v, %v; want the avatar dropped", p, err)
	}
}

// The key rides in the query string, and an error carrying the URL would put
// it in the logs. Whatever fails, the message must not contain it.
func TestFetchProfileNeverPutsTheKeyInAnError(t *testing.T) {
	const key = "SECRETKEY0123456789"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	// A refused key: an HTTP status.
	client := &http.Client{Transport: redirectTo(srv.URL)}
	if _, err := FetchProfile(context.Background(), client, key, me); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("403: err = %v", err)
	}
	// A transport failure: the error Go builds quotes the URL.
	srv.Close()
	if _, err := FetchProfile(context.Background(), client, key, me); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("connection refused: err = %v", err)
	}
}

// redirectTo sends every request to the test server, keeping the query.
type redirectTo string

func (base redirectTo) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	target, _ := http.NewRequestWithContext(r.Context(), r.Method, string(base)+"/?"+u.RawQuery, nil)
	return http.DefaultTransport.RoundTrip(target)
}
