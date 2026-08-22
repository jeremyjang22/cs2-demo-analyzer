// Package auth implements Steam OpenID 2.0 login and a signed session cookie.
//
// Steam is the identity provider because the steamid it returns is already the
// join key in every parsed demo - players.csv, kills.csv, round_players.csv are
// all keyed on it. Email or Discord login would identify the person but leave
// "which player in this demo are you?" as a separate problem to solve.
//
// Steam speaks OpenID 2.0, which is old and deprecated generally, but it is
// what Steam offers and it is simple enough to implement against directly:
// redirect, then ask Steam to confirm the assertion it just sent back.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	steamLogin = "https://steamcommunity.com/openid/login"
	steamNS    = "http://specs.openid.net/auth/2.0"
)

// claimedID looks like https://steamcommunity.com/openid/id/76561198000000000
var claimedID = regexp.MustCompile(`^https?://steamcommunity\.com/openid/id/(\d{17})$`)

var ErrNotAuthenticated = errors.New("steam did not confirm the assertion")

// RedirectURL is where to send the browser to start login. realm is this
// service's own origin; returnTo must sit underneath it or Steam rejects it.
func RedirectURL(realm, returnTo string) string {
	q := url.Values{
		"openid.ns":         {steamNS},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {steamNS + "/identifier_select"},
		"openid.claimed_id": {steamNS + "/identifier_select"},
	}
	return steamLogin + "?" + q.Encode()
}

// Verify checks the callback parameters with Steam and returns the steamid.
//
// The signature cannot be validated locally, so the only safe check is to hand
// the whole assertion back to Steam with mode=check_authentication and let it
// confirm. Trusting the query string without this step would let anyone log in
// as anyone by editing a URL.
func Verify(ctx context.Context, client *http.Client, params url.Values) (uint64, error) {
	if params.Get("openid.mode") == "" {
		return 0, errors.New("not an openid callback")
	}

	check := url.Values{}
	for k, v := range params {
		check[k] = v
	}
	check.Set("openid.mode", "check_authentication")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, steamLogin,
		strings.NewReader(check.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("contact steam: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return 0, err
	}
	// Steam answers with a tiny key:value body; the only line that matters is
	// is_valid, and anything other than an explicit true is a rejection.
	if !strings.Contains(string(body), "is_valid:true") {
		return 0, ErrNotAuthenticated
	}

	m := claimedID.FindStringSubmatch(params.Get("openid.claimed_id"))
	if m == nil {
		return 0, fmt.Errorf("unexpected claimed_id %q", params.Get("openid.claimed_id"))
	}
	id, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// DefaultClient has a timeout because a hung request to Steam should fail the
// login rather than tie up a handler indefinitely.
var DefaultClient = &http.Client{Timeout: 10 * time.Second}
