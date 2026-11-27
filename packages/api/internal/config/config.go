// Package config loads settings from the environment.
//
// Everything is read once at startup and validated there, so a missing value
// fails the process immediately rather than the first time a request needs it.
// On Fly that means a bad deploy stops at boot and the previous release keeps
// serving, instead of returning 500s.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/api/internal/uploads"
)

type Config struct {
	Port string

	// DatabaseURL is Supabase's session pooler (port 5432). Not the
	// transaction pooler (6543): pgx caches prepared statements on each
	// connection, and a transaction pooler hands that connection to someone
	// else between transactions - "prepared statement already exists".
	DatabaseURL string

	// SessionKey signs the login cookie. Rotating it logs everyone out, which
	// is the intended escape hatch if it ever leaks.
	SessionKey []byte

	// PublicURL is this API's own origin, used to build the Steam OpenID
	// return_to. Steam checks it against the realm, so a mismatch fails login
	// with no useful error.
	PublicURL string

	// WebOrigin is where the frontend is served from. Needed for CORS, and to
	// send the user somewhere after login.
	WebOrigin string

	// SteamAPIKey reads each player's name and avatar at sign-in
	// (steamcommunity.com/dev/apikey). Optional: without it sign-in still
	// works and a player is shown by steamid, because OpenID returns the id
	// and nothing else.
	SteamAPIKey string

	// Uploads: who may upload (UPLOADERS, steamids), how big a file
	// (UPLOAD_MAX_BYTES, default 1 GB) and how many a day each
	// (UPLOAD_DAILY_LIMIT, default 20). The R2 keys are read by
	// store.ConfigFromEnv; without them the API runs and uploads are off.
	Uploads uploads.Config
}

func Load() (*Config, error) {
	c := &Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SessionKey:  []byte(os.Getenv("SESSION_KEY")),
		PublicURL:   strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/"),
		WebOrigin:   strings.TrimSuffix(os.Getenv("WEB_ORIGIN"), "/"),
		SteamAPIKey: strings.TrimSpace(os.Getenv("STEAM_API_KEY")),
	}

	var err error
	if c.Uploads.Uploaders, err = uploads.ParseUploaders(os.Getenv("UPLOADERS")); err != nil {
		return nil, err
	}
	if c.Uploads.MaxBytes, err = intEnv("UPLOAD_MAX_BYTES", 1<<30); err != nil {
		return nil, err
	}
	limit, err := intEnv("UPLOAD_DAILY_LIMIT", 20)
	if err != nil {
		return nil, err
	}
	c.Uploads.DailyLimit = int(limit)
	c.Uploads.URLTTL = time.Hour

	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(c.SessionKey) < 32 {
		missing = append(missing, "SESSION_KEY (needs >= 32 bytes)")
	}
	if c.PublicURL == "" {
		missing = append(missing, "PUBLIC_URL")
	}
	if c.WebOrigin == "" {
		missing = append(missing, "WEB_ORIGIN")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing config: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

func intEnv(key string, fallback int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive whole number, not %q", key, v)
	}
	return n, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
