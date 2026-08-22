package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const CookieName = "sid"

var ErrBadSession = errors.New("invalid or expired session")

// Session is a signed cookie rather than a database row.
//
// At this scale a server-side session table buys nothing: there is one API
// process, sessions carry only a steamid, and "log everyone out" is achieved by
// rotating SESSION_KEY. The tradeoff is that individual sessions cannot be
// revoked before they expire, which is acceptable for a viewer and would not be
// for anything that can spend money.
type Session struct {
	SteamID uint64
	Expires time.Time
}

// Encode returns "steamid.expiry.signature", all base64url.
func Encode(key []byte, s Session) string {
	payload := fmt.Sprintf("%d.%d", s.SteamID, s.Expires.Unix())
	return payload + "." + sign(key, payload)
}

func Decode(key []byte, raw string) (Session, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Session{}, ErrBadSession
	}
	payload := parts[0] + "." + parts[1]

	// Constant-time compare: a timing-variable check here leaks the signature
	// one byte at a time to anyone willing to measure.
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(sign(key, payload))) != 1 {
		return Session{}, ErrBadSession
	}

	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return Session{}, ErrBadSession
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Session{}, ErrBadSession
	}
	if time.Now().After(time.Unix(exp, 0)) {
		return Session{}, ErrBadSession
	}
	return Session{SteamID: id, Expires: time.Unix(exp, 0)}, nil
}

func sign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SetCookie writes the session. Secure and SameSite=Lax because the API and the
// frontend are on different origins: Lax still allows the top-level redirect
// back from Steam to carry the cookie, which Strict would block.
func SetCookie(w http.ResponseWriter, key []byte, s Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    Encode(key, s),
		Path:     "/",
		Expires:  s.Expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

// FromRequest reads and validates the session on an incoming request.
func FromRequest(key []byte, r *http.Request) (Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, ErrBadSession
	}
	return Decode(key, c.Value)
}
