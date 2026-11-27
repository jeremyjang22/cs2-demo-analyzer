// Package uploads takes a demo from a browser into the processing queue.
//
// The browser never sends the file through us. It asks for an upload (Start),
// gets a presigned URL good for one object of exactly the declared length for
// an hour, PUTs the file straight to R2, and says it is done (Complete), at
// which point the demo is queued for demo-worker. Cloudflare's free plan caps
// a request body at 100 MB and demos run to 670 MB, and relaying them through
// Northflank would pay for the bandwidth twice. docs/uploads.md has the flow.
//
// Everything here fails closed: not on the allowlist, too big, over the day's
// quota, a hash that is not a hash - refused, with a status a person can read.
package uploads

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/catalog"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/store"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/worker"
)

// Storage is what uploads needs of the bucket: *store.Store, or a fake in
// tests.
type Storage interface {
	PresignPut(ctx context.Context, key string, size int64, expires time.Duration) (string, error)
	Size(ctx context.Context, key string) (int64, error)
}

// Config is the rules. Who may upload at all is not here but in the
// uploaders table (catalog.IsUploader), so it changes without a restart.
type Config struct {
	MaxBytes int64
	// DailyLimit is uploads per uploader per rolling 24 hours.
	DailyLimit int
	// URLTTL is how long an upload URL stays good.
	URLTTL time.Duration
}

// Service is the upload endpoints' logic, without HTTP.
type Service struct {
	DB    *pgxpool.Pool
	Store Storage // nil when R2 is not configured: nobody can upload
	Cfg   Config
}

// Error is a refusal with the HTTP status it should be answered with.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func refuse(status int, format string, args ...any) error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// Request is what the browser declares before sending anything.
type Request struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// Upload is one of a person's uploads, as their status rows show it.
type Upload struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // uploading, queued, parsing, ready, failed
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Ticket is the answer to Start: where to PUT the file, or - for a demo
// already known by its hash - the demo itself and no URL.
type Ticket struct {
	Upload    Upload     `json:"upload"`
	URL       string     `json:"url,omitempty"`
	Expires   *time.Time `json:"expires,omitempty"`
	Duplicate bool       `json:"duplicate"`
}

// Allowed reports whether steamid may upload at all, now: storage is
// configured and they are on the allowlist. An error is a refusal too.
func (s *Service) Allowed(ctx context.Context, steamid uint64) (bool, error) {
	if s.Store == nil {
		return false, nil
	}
	return catalog.IsUploader(ctx, s.DB, steamid)
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Check is what is wrong with a request before anything is looked up.
func (c Config) Check(r Request) error {
	if NameFor(r.Filename) == "" {
		return refuse(http.StatusBadRequest, "%q is not a demo: upload a .dem or .dem.zst file", r.Filename)
	}
	if r.Size <= 0 {
		return refuse(http.StatusBadRequest, "the file is empty")
	}
	if r.Size > c.MaxBytes {
		return refuse(http.StatusRequestEntityTooLarge, "the file is %d MB; the limit is %d MB", r.Size>>20, c.MaxBytes>>20)
	}
	if !sha256Hex.MatchString(r.SHA256) {
		return refuse(http.StatusBadRequest, "sha256 must be 64 lowercase hex digits")
	}
	return nil
}

// NameFor is the name a demo uploaded as filename is published under, before
// making it unique: the file name without .dem or .dem.zst, with anything a
// URL or storage key would need escaped turned into '-'. "" for a file that
// is not a demo.
func NameFor(filename string) string {
	base := filename
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".dem.zst"):
		base = base[:len(base)-len(".dem.zst")]
	case strings.HasSuffix(lower, ".dem"):
		base = base[:len(base)-len(".dem")]
	default:
		return ""
	}
	var b strings.Builder
	dash := false
	for _, r := range base {
		ok := r < 128 && (r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		if !ok || r == '-' {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = true
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	name := strings.TrimLeft(b.String(), "._-")
	if len(name) > 100 {
		name = name[:100]
	}
	name = strings.TrimRight(name, "._-")
	if name == "" {
		return "demo"
	}
	return name
}

// candidate is the i-th name tried for base: base, base-2, base-3...
func candidate(base string, i int) string {
	if i == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, i)
}

const selectUpload = `
SELECT d.id::text, d.name, d.status::text, coalesce(d.original_filename, ''), coalesce(d.size_bytes, 0),
       coalesce(j.last_error, ''), d.created_at
FROM demos d
LEFT JOIN LATERAL (SELECT last_error FROM jobs WHERE demo_id = d.id ORDER BY id DESC LIMIT 1) j ON true`

func scanUpload(row pgx.Row) (Upload, error) {
	var u Upload
	err := row.Scan(&u.ID, &u.Name, &u.Status, &u.Filename, &u.Size, &u.Error, &u.CreatedAt)
	return u, err
}

// Start checks a request and hands out somewhere to put the file.
func (s *Service) Start(ctx context.Context, steamid uint64, r Request) (Ticket, error) {
	ok, err := s.Allowed(ctx, steamid)
	if err != nil {
		return Ticket{}, err
	}
	if !ok {
		if s.Store == nil {
			return Ticket{}, refuse(http.StatusServiceUnavailable, "uploads are not configured on this server")
		}
		return Ticket{}, refuse(http.StatusForbidden, "this account cannot upload demos yet")
	}
	if err := s.Cfg.Check(r); err != nil {
		return Ticket{}, err
	}

	// Known by its hash: parsed once ever. The same uploader's own
	// unfinished upload is handed a fresh URL instead, so an interrupted
	// transfer can be started again.
	known, by, err := s.bySHA(ctx, r.SHA256)
	if err != nil {
		return Ticket{}, err
	}
	if known != nil {
		if known.Status == "uploading" && by == steamid {
			return s.ticket(ctx, *known, r.Size)
		}
		return Ticket{Upload: *known, Duplicate: true}, nil
	}

	var recent int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM demos WHERE uploaded_by = $1 AND created_at > now() - interval '24 hours'`,
		int64(steamid)).Scan(&recent); err != nil {
		return Ticket{}, err
	}
	if recent >= s.Cfg.DailyLimit {
		return Ticket{}, refuse(http.StatusTooManyRequests, "%d uploads in the last day is the limit; try again tomorrow", s.Cfg.DailyLimit)
	}

	base := NameFor(r.Filename)
	for i := 1; i <= 100; i++ {
		name := candidate(base, i)
		if catalog.CheckName(name) != nil {
			break
		}
		// Private and owned by the uploader until they say otherwise. ON
		// CONFLICT covers both the name and the hash: a taken name tries
		// the next, and a hash that arrived in the meantime is a duplicate.
		u, err := scanUpload(s.DB.QueryRow(ctx, `
WITH d AS (
    INSERT INTO demos (name, sha256, uploaded_by, owner, original_filename, size_bytes)
    VALUES ($1, $2, $3, $3, $4, $5)
    ON CONFLICT DO NOTHING
    RETURNING *
)
SELECT d.id::text, d.name, d.status::text, d.original_filename, d.size_bytes, '', d.created_at FROM d`,
			name, r.SHA256, int64(steamid), r.Filename, r.Size))
		if errors.Is(err, pgx.ErrNoRows) {
			if known, _, err := s.bySHA(ctx, r.SHA256); err != nil {
				return Ticket{}, err
			} else if known != nil {
				return Ticket{Upload: *known, Duplicate: true}, nil
			}
			continue
		}
		if err != nil {
			return Ticket{}, err
		}
		return s.ticket(ctx, u, r.Size)
	}
	return Ticket{}, refuse(http.StatusConflict, "no free name for %q; rename the file", r.Filename)
}

func (s *Service) bySHA(ctx context.Context, sha string) (*Upload, uint64, error) {
	var by *int64
	row := s.DB.QueryRow(ctx, `SELECT uploaded_by FROM demos WHERE sha256 = $1`, sha)
	if err := row.Scan(&by); errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	}
	u, err := scanUpload(s.DB.QueryRow(ctx, selectUpload+` WHERE d.sha256 = $1`, sha))
	if err != nil {
		return nil, 0, err
	}
	var uploader uint64
	if by != nil {
		uploader = uint64(*by)
	}
	return &u, uploader, nil
}

func (s *Service) ticket(ctx context.Context, u Upload, size int64) (Ticket, error) {
	url, err := s.Store.PresignPut(ctx, store.IncomingKey(u.ID), size, s.Cfg.URLTTL)
	if err != nil {
		return Ticket{}, err
	}
	expires := time.Now().Add(s.Cfg.URLTTL).UTC().Truncate(time.Second)
	return Ticket{Upload: u, URL: url, Expires: &expires}, nil
}

// Complete is the browser saying the PUT finished. The file is checked to be
// there at the declared size, and the demo is queued. Saying so twice is
// harmless.
func (s *Service) Complete(ctx context.Context, steamid uint64, id string) (Upload, error) {
	if s.Store == nil {
		return Upload{}, refuse(http.StatusServiceUnavailable, "uploads are not configured on this server")
	}
	u, err := scanUpload(s.DB.QueryRow(ctx, selectUpload+` WHERE d.id::text = $1 AND d.uploaded_by = $2`, id, int64(steamid)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, refuse(http.StatusNotFound, "no such upload")
	}
	if err != nil {
		return Upload{}, err
	}
	if u.Status != "uploading" {
		return u, nil
	}

	size, err := s.Store.Size(ctx, store.IncomingKey(u.ID))
	if errors.Is(err, store.ErrNotFound) {
		return Upload{}, refuse(http.StatusConflict, "the file has not arrived; upload it again")
	}
	if err != nil {
		return Upload{}, err
	}
	if size != u.Size {
		return Upload{}, refuse(http.StatusConflict, "%d bytes arrived of the %d declared; upload it again", size, u.Size)
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Upload{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE demos SET status = 'queued' WHERE id::text = $1 AND status = 'uploading'`, u.ID); err != nil {
		return Upload{}, err
	}
	// One live job per demo, by a partial unique index; a second Complete
	// racing this one inserts nothing.
	if _, err := tx.Exec(ctx, `INSERT INTO jobs (demo_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return Upload{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Upload{}, err
	}
	u.Status = "queued"
	return u, nil
}

// List is a person's most recent uploads, newest first.
func (s *Service) List(ctx context.Context, steamid uint64) ([]Upload, error) {
	rows, err := s.DB.Query(ctx, selectUpload+` WHERE d.uploaded_by = $1 ORDER BY d.created_at DESC LIMIT 20`, int64(steamid))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Online reports whether any demo-worker has reported in within a lease's
// length - whether a queued upload will start shortly, or is waiting for a
// machine that is off. The site says which, so a demo stuck at "queued"
// overnight is explained rather than mysterious.
func (s *Service) Online(ctx context.Context) (bool, error) {
	var online bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workers WHERE seen_at > now() - make_interval(secs => $1))`,
		worker.LeaseFor.Seconds()).Scan(&online)
	return online, err
}
