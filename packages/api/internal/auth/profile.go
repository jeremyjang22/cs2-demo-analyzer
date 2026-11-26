package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Profile is the public part of a Steam account the site shows: the name and
// avatar a player chose. OpenID only proves which account signed in, so these
// come from a second call, to the Steam Web API.
type Profile struct {
	Name   string
	Avatar string // https URL, 64x64; "" when Steam gave none
}

// summariesURL is the Steam Web API endpoint for public profile data.
const summariesURL = "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/"

// maxName bounds what is stored. Steam caps a persona name at 32 characters;
// this is generous headroom, not a rule about names.
const maxName = 64

// FetchProfile asks Steam for one account's name and avatar.
//
// The key travels in the query string - that is the API's design - and Go's
// HTTP errors quote the full URL, so a failure logged as-is would write the
// key into the service's logs. Every error from here is rebuilt without it.
func FetchProfile(ctx context.Context, client *http.Client, apiKey string, steamID uint64) (Profile, error) {
	q := url.Values{"key": {apiKey}, "steamids": {strconv.FormatUint(steamID, 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, summariesURL+"?"+q.Encode(), nil)
	if err != nil {
		return Profile{}, errors.New("steam profile: could not build the request")
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the cause, without the URL and its key
		}
		return Profile{}, fmt.Errorf("steam profile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 403 is the usual one: a wrong or revoked key.
		return Profile{}, fmt.Errorf("steam profile: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return Profile{}, fmt.Errorf("steam profile: read: %w", err)
	}
	return parseSummaries(body, steamID)
}

// parseSummaries picks one account out of a GetPlayerSummaries response.
// Pure, so the shape of Steam's answer is what the tests pin.
func parseSummaries(body []byte, steamID uint64) (Profile, error) {
	var r struct {
		Response struct {
			Players []struct {
				SteamID      string `json:"steamid"`
				PersonaName  string `json:"personaname"`
				AvatarMedium string `json:"avatarmedium"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Profile{}, fmt.Errorf("steam profile: decode: %w", err)
	}
	want := strconv.FormatUint(steamID, 10)
	for _, p := range r.Response.Players {
		if p.SteamID != want {
			continue
		}
		name := strings.TrimSpace(p.PersonaName)
		if name == "" {
			return Profile{}, errors.New("steam profile: empty name")
		}
		if r := []rune(name); len(r) > maxName {
			name = string(r[:maxName])
		}
		prof := Profile{Name: name}
		// Only an https URL is kept: it is rendered straight into an <img>,
		// and anything else from a third-party response is not worth trusting.
		if strings.HasPrefix(p.AvatarMedium, "https://") {
			prof.Avatar = p.AvatarMedium
		}
		return prof, nil
	}
	// Steam answers an unknown or private-beyond-reach id with an empty list.
	return Profile{}, errors.New("steam profile: account not in the response")
}
