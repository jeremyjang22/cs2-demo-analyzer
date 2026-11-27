package uploads

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/store"
)

func TestNameFor(t *testing.T) {
	for in, want := range map[string]string{
		"nukepug.dem": "nukepug",
		"1-604132bd-e063-4e28-acfa-c2b4dc1d670c-1-1.dem.zst": "1-604132bd-e063-4e28-acfa-c2b4dc1d670c-1-1",
		`C:\demos\Mirage Final (1).DEM`:                      "Mirage-Final-1",
		"../../etc/passwd.dem":                               "passwd",
		"---.dem":                                            "demo",
		"заставка.dem":                                       "demo",
		"notes.txt":                                          "",
		"x.dem.gz":                                           "",
	} {
		if got := NameFor(in); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NameFor(strings.Repeat("a", 300) + ".dem"); len(got) != 100 {
		t.Errorf("long name kept %d characters", len(got))
	}
}

func TestCheck(t *testing.T) {
	c := Config{MaxBytes: 1 << 30}
	sha := strings.Repeat("ab", 32)
	if err := c.Check(Request{"a.dem", 100, sha}); err != nil {
		t.Errorf("good request: %v", err)
	}
	for name, r := range map[string]Request{
		"not a demo": {"a.zip", 100, sha},
		"empty":      {"a.dem", 0, sha},
		"too big":    {"a.dem", 2 << 30, sha},
		"bad hash":   {"a.dem", 100, strings.ToUpper(sha)},
	} {
		if c.Check(r) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	var e *Error
	if err := c.Check(Request{"a.dem", 2 << 30, sha}); !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("too big: %v", err)
	}
}

func TestParseUploaders(t *testing.T) {
	got, err := ParseUploaders(" 76561198230376396, 76561198074762801\n")
	if err != nil || len(got) != 2 || !got[76561198230376396] {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := ParseUploaders("76561198230376396,7656119"); err == nil {
		t.Error("a short id should be refused, not skipped")
	}
	if got, err := ParseUploaders(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %v, %v", got, err)
	}
}

// fakeStorage is a bucket in a map: PresignPut "uploads" nothing, and the
// test puts objects in directly, as a browser's PUT would.
type fakeStorage struct{ objects map[string]int64 }

func (f *fakeStorage) PresignPut(_ context.Context, key string, size int64, _ time.Duration) (string, error) {
	return "https://r2.example/" + key, nil
}

func (f *fakeStorage) Size(_ context.Context, key string) (int64, error) {
	if n, ok := f.objects[key]; ok {
		return n, nil
	}
	return 0, store.ErrNotFound
}

const (
	me    = uint64(76561198230376396)
	other = uint64(76561198000000001)
)

// Against a migrated THROWAWAY database (TEST_DATABASE_URL): it deletes
// every demo.
func TestUploadFlow(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated throwaway database")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`DELETE FROM demos`,
		`INSERT INTO users (steamid, display_name) VALUES (76561198230376396, 'me'), (76561198000000001, 'other') ON CONFLICT DO NOTHING`,
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	bucket := &fakeStorage{objects: map[string]int64{}}
	s := &Service{DB: db, Store: bucket, Cfg: Config{
		Uploaders: map[uint64]bool{me: true, other: true}, MaxBytes: 1 << 30, DailyLimit: 3, URLTTL: time.Hour,
	}}
	status := func(err error) int {
		var e *Error
		if errors.As(err, &e) {
			return e.Status
		}
		return 0
	}
	sha := func(c string) string { return strings.Repeat(c, 64) }

	// Not on the list.
	if _, err := s.Start(ctx, 76561198999999999, Request{"x.dem", 10, sha("0")}); status(err) != http.StatusForbidden {
		t.Errorf("stranger: %v", err)
	}

	tk, err := s.Start(ctx, me, Request{"nukepug.dem", 1000, sha("a")})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Duplicate || tk.URL != "https://r2.example/incoming/"+tk.Upload.ID || tk.Upload.Name != "nukepug" || tk.Upload.Status != "uploading" {
		t.Fatalf("ticket = %+v", tk)
	}

	// Asking again for the same unfinished upload resumes it.
	again, err := s.Start(ctx, me, Request{"nukepug.dem", 1000, sha("a")})
	if err != nil || again.Upload.ID != tk.Upload.ID || again.URL == "" {
		t.Errorf("resume: %+v, %v", again, err)
	}

	// Complete before the file arrives, then with the wrong size, then right.
	if _, err := s.Complete(ctx, me, tk.Upload.ID); status(err) != http.StatusConflict {
		t.Errorf("not arrived: %v", err)
	}
	bucket.objects[store.IncomingKey(tk.Upload.ID)] = 999
	if _, err := s.Complete(ctx, me, tk.Upload.ID); status(err) != http.StatusConflict {
		t.Errorf("short: %v", err)
	}
	if _, err := s.Complete(ctx, other, tk.Upload.ID); status(err) != http.StatusNotFound {
		t.Errorf("someone else's upload: %v", err)
	}
	bucket.objects[store.IncomingKey(tk.Upload.ID)] = 1000
	u, err := s.Complete(ctx, me, tk.Upload.ID)
	if err != nil || u.Status != "queued" {
		t.Fatalf("complete: %+v, %v", u, err)
	}
	if u, err := s.Complete(ctx, me, tk.Upload.ID); err != nil || u.Status != "queued" {
		t.Errorf("second complete: %+v, %v", u, err)
	}
	var jobs int
	db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE demo_id = $1::uuid AND state = 'queued'`, tk.Upload.ID).Scan(&jobs)
	if jobs != 1 {
		t.Errorf("%d queued jobs, want 1", jobs)
	}
	var owner int64
	var visibility string
	db.QueryRow(ctx, `SELECT owner, visibility::text FROM demos WHERE id = $1::uuid`, tk.Upload.ID).Scan(&owner, &visibility)
	if uint64(owner) != me || visibility != "private" {
		t.Errorf("owner %d, visibility %s: an upload is private to its uploader", owner, visibility)
	}

	// The same file from anyone is the same demo.
	dup, err := s.Start(ctx, other, Request{"renamed.dem", 1000, sha("a")})
	if err != nil || !dup.Duplicate || dup.URL != "" || dup.Upload.Name != "nukepug" {
		t.Errorf("duplicate: %+v, %v", dup, err)
	}

	// A taken name gets the next one.
	tk2, err := s.Start(ctx, me, Request{"nukepug.dem", 50, sha("b")})
	if err != nil || tk2.Upload.Name != "nukepug-2" {
		t.Errorf("second nukepug: %+v, %v", tk2, err)
	}

	// Three in a day is the limit here.
	if _, err := s.Start(ctx, me, Request{"third.dem", 50, sha("c")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, me, Request{"fourth.dem", 50, sha("d")}); status(err) != http.StatusTooManyRequests {
		t.Errorf("over quota: %v", err)
	}

	list, err := s.List(ctx, me)
	if err != nil || len(list) != 3 || list[0].Name != "third" {
		t.Errorf("list: %+v, %v", list, err)
	}

	// Processing is online while a worker has reported in lately.
	db.Exec(ctx, `DELETE FROM workers`)
	if on, err := s.Online(ctx); err != nil || on {
		t.Errorf("no workers: online=%v, %v", on, err)
	}
	db.Exec(ctx, `INSERT INTO workers (name, version, started_at, seen_at) VALUES ('pc', 'v', now(), now() - interval '10 minutes')`)
	if on, _ := s.Online(ctx); on {
		t.Error("a worker silent for ten minutes counts as online")
	}
	db.Exec(ctx, `UPDATE workers SET seen_at = now() - interval '20 seconds'`)
	if on, _ := s.Online(ctx); !on {
		t.Error("a worker seen 20s ago counts as offline")
	}

	// No bucket configured: nobody uploads, and it says why.
	off := &Service{DB: db, Cfg: s.Cfg}
	if off.Allowed(me) {
		t.Error("allowed without storage")
	}
	if _, err := off.Start(ctx, me, Request{"x.dem", 1, sha("e")}); status(err) != http.StatusServiceUnavailable {
		t.Errorf("unconfigured: %v", err)
	}
}
