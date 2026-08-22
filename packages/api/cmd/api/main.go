// Command api serves the demo viewer's backend: Steam login now, uploads and
// parsing later.
//
// One binary will eventually run both the HTTP API and the parse worker. They
// share the collector package and the database, and splitting them into two
// deployables would buy independent scaling that a two-person service does not
// need.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/api/internal/auth"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/api/internal/config"
)

const sessionTTL = 30 * 24 * time.Hour

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	// Ping at startup rather than on the first request: a bad connection string
	// should fail the deploy, not surface as a 500 to whoever arrives first.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	log.Info("database ready")

	srv := &server{cfg: cfg, pool: pool, log: log}
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "port", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	// Fly sends SIGTERM before replacing a machine; draining in-flight requests
	// keeps a deploy from cutting someone off mid-response.
	shutCtx, cancelShut := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShut()
	return httpSrv.Shutdown(shutCtx)
}

type server struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	log  *slog.Logger
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /auth/steam/login", s.steamLogin)
	mux.HandleFunc("GET /auth/steam/callback", s.steamCallback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /me", s.me)
	return s.withCORS(mux)
}

// withCORS allows exactly the configured frontend origin. Credentials are
// required because the session is a cookie, and a wildcard origin is not
// permitted alongside credentials - so this must be the specific origin.
func (s *server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == s.cfg.WebOrigin {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", s.cfg.WebOrigin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// health is what Fly polls. It checks the database too, so a machine that has
// lost its connection is taken out of rotation instead of accepting traffic it
// cannot serve.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		s.log.Error("health: database unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "degraded", "database": "unreachable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
}

func (s *server) steamLogin(w http.ResponseWriter, r *http.Request) {
	returnTo := s.cfg.PublicURL + "/auth/steam/callback"
	http.Redirect(w, r, auth.RedirectURL(s.cfg.PublicURL, returnTo), http.StatusFound)
}

func (s *server) steamCallback(w http.ResponseWriter, r *http.Request) {
	steamID, err := auth.Verify(r.Context(), auth.DefaultClient, r.URL.Query())
	if err != nil {
		s.log.Warn("steam login rejected", "err", err)
		http.Error(w, "steam login failed", http.StatusUnauthorized)
		return
	}

	// Upsert on every login so a name change is picked up without a separate
	// sync. The display name is a placeholder until the Steam Web API is wired
	// in - OpenID returns the id and nothing else.
	const q = `
		INSERT INTO users (steamid, display_name)
		VALUES ($1, $2)
		ON CONFLICT (steamid) DO UPDATE SET last_seen_at = now()`
	if _, err := s.pool.Exec(r.Context(), q, int64(steamID), fmt.Sprint(steamID)); err != nil {
		s.log.Error("upsert user", "err", err, "steamid", steamID)
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}

	auth.SetCookie(w, s.cfg.SessionKey, auth.Session{
		SteamID: steamID,
		Expires: time.Now().Add(sessionTTL),
	})
	s.log.Info("login", "steamid", steamID)
	http.Redirect(w, r, s.cfg.WebOrigin, http.StatusFound)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	sess, err := auth.FromRequest(s.cfg.SessionKey, r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		return
	}

	var name string
	var avatar *string
	const q = `SELECT display_name, avatar_url FROM users WHERE steamid = $1`
	if err := s.pool.QueryRow(r.Context(), q, int64(sess.SteamID)).Scan(&name, &avatar); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		// A steamid exceeds what JSON numbers hold safely, so it goes as a
		// string - the same reason Player.id is a string in the payload.
		"steamid": fmt.Sprint(sess.SteamID),
		"name":    name,
		"avatar":  avatar,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
