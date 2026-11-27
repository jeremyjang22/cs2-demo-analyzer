package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/zstd"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/store"
)

// bucket is the bucket in a map.
type bucket struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (b *bucket) GetTo(_ context.Context, key string, w io.Writer) (int64, error) {
	b.mu.Lock()
	body, ok := b.objects[key]
	b.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("get %s: %w", key, store.ErrNotFound)
	}
	n, err := w.Write(body)
	return int64(n), err
}

func (b *bucket) Put(_ context.Context, key string, body []byte, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = bytes.Clone(body)
	return nil
}

func (b *bucket) PutFile(ctx context.Context, key, path, ct string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return b.Put(ctx, key, body, ct)
}

func (b *bucket) Exists(_ context.Context, key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.objects[key]
	return ok, nil
}

func (b *bucket) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	return nil
}

func (b *bucket) has(key string) bool { ok, _ := b.Exists(context.Background(), key); return ok }

func (b *bucket) get(key string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.objects[key]
}

const uploader = 76561198230376396

// Against a migrated THROWAWAY database (TEST_DATABASE_URL): it deletes
// every demo and every worker.
func TestQueue(t *testing.T) {
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
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM demos`)
	exec(`DELETE FROM workers`)
	exec(`INSERT INTO users (steamid, display_name) VALUES ($1, 'me') ON CONFLICT DO NOTHING`, uploader)

	b := &bucket{objects: map[string][]byte{}}
	// upload queues a file as the API would leave it after /complete.
	upload := func(name string, body []byte) (demoID string, jobID int64) {
		t.Helper()
		sum := sha256.Sum256(body)
		if err := db.QueryRow(ctx, `
INSERT INTO demos (name, sha256, uploaded_by, owner, original_filename, size_bytes, status)
VALUES ($1, $2, $3, $3, $1 || '.dem', $4, 'queued') RETURNING id::text`,
			name, hex.EncodeToString(sum[:]), uploader, len(body)).Scan(&demoID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `INSERT INTO jobs (demo_id) VALUES ($1) RETURNING id`, demoID).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		b.Put(ctx, store.IncomingKey(demoID), body, "")
		return demoID, jobID
	}
	type outcome struct {
		job, demo string
		attempts  int
		err       string
		steps     map[string]int64
	}
	outcomeOf := func(jobID int64) outcome {
		t.Helper()
		var o outcome
		var lastErr *string
		if err := db.QueryRow(ctx, `
SELECT j.state::text, d.status::text, j.attempts, j.last_error, coalesce(j.steps, '{}')
FROM jobs j JOIN demos d ON d.id = j.demo_id WHERE j.id = $1`, jobID).
			Scan(&o.job, &o.demo, &o.attempts, &lastErr, &o.steps); err != nil {
			t.Fatal(err)
		}
		if lastErr != nil {
			o.err = *lastErr
		}
		return o
	}

	var processed, published []string
	processFails := map[string]error{}
	w := &Worker{
		DB:    db,
		Store: b,
		Process: func(ctx context.Context, demo, out, name string, log io.Writer) error {
			processed = append(processed, filepath.Base(demo))
			if err := processFails[name]; err != nil {
				return err
			}
			fmt.Fprintln(log, "  round  1  100 ticks")
			if err := os.MkdirAll(filepath.Join(out, name), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(out, name, "movement.json"), []byte(`{}`), 0o644)
		},
		Publish: func(ctx context.Context, dir string, log io.Writer) error {
			if _, err := os.Stat(filepath.Join(dir, "movement.json")); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("published %s without a payload", dir)
			}
			published = append(published, filepath.Base(dir))
			_, err := db.Exec(ctx, `UPDATE demos SET status = 'ready' WHERE name = $1`, filepath.Base(dir))
			return err
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: Config{
			Name: "test-pc", Version: "test", Scratch: t.TempDir(),
			Timeout: time.Minute, MaxAttempts: 2, Once: true,
		},
	}

	demo := append([]byte("PBDEMS2\x00"), bytes.Repeat([]byte("entity update "), 5000)...)
	enc, _ := zstd.NewWriter(nil)
	good, goodJob := upload("good", demo)
	packed, packedJob := upload("packed", enc.EncodeAll(append(bytes.Clone(demo), 1), nil))
	_, notDemoJob := upload("notademo", []byte("PK\x03\x04 a zip file"))
	_, flakyJob := upload("flaky", append(bytes.Clone(demo), 2))
	processFails["flaky"] = errors.New("out of memory")
	_, badJob := upload("bad", append(bytes.Clone(demo), 3))
	processFails["bad"] = Permanent("the demo could not be read: bad packet")
	tampered, tamperedJob := upload("tampered", append(bytes.Clone(demo), 4))
	b.objects[store.IncomingKey(tampered)][100] ^= 1 // same length, different bytes

	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// A plain .dem: processed as one, kept compressed, published, tidied.
	o := outcomeOf(goodJob)
	if o.job != "done" || o.demo != "ready" || o.attempts != 1 || o.err != "" {
		t.Errorf("good: %+v", o)
	}
	for _, step := range []string{"download", "check", "process", "keep", "publish", "tidy"} {
		if _, ok := o.steps[step]; !ok {
			t.Errorf("good: no timing for %s in %v", step, o.steps)
		}
	}
	sum := sha256.Sum256(demo)
	raw := b.get(store.RawKey(hex.EncodeToString(sum[:])))
	dec, _ := zstd.NewReader(nil)
	if back, err := dec.DecodeAll(raw, nil); err != nil || !bytes.Equal(back, demo) {
		t.Errorf("good: the kept original does not decompress to the upload: %v", err)
	}
	if b.has(store.IncomingKey(good)) {
		t.Error("good: the upload was left in incoming/")
	}
	log := string(b.get(store.JobLogKey(goodJob, 1)))
	if !strings.Contains(log, "round  1") || !strings.HasSuffix(log, "done\n") {
		t.Errorf("good: job log:\n%s", log)
	}

	// A .dem.zst is handed to the parser as one, and kept as it came.
	if o := outcomeOf(packedJob); o.job != "done" {
		t.Errorf("packed: %+v", o)
	}
	if !strings.Contains(strings.Join(processed, " "), "packed.dem.zst") {
		t.Errorf("processed %v: packed should be a .dem.zst", processed)
	}
	if b.has(store.IncomingKey(packed)) {
		t.Error("packed: left in incoming/")
	}

	// Wrong files fail at once, with a reason worded for the uploader, and
	// never reach the parser.
	if o := outcomeOf(notDemoJob); o.job != "failed" || o.demo != "failed" || o.attempts != 1 || o.err != "the file is not a CS2 demo" {
		t.Errorf("not a demo: %+v", o)
	}
	if o := outcomeOf(tamperedJob); o.job != "failed" || !strings.Contains(o.err, "SHA-256") {
		t.Errorf("tampered: %+v", o)
	}
	if o := outcomeOf(badJob); o.job != "failed" || o.attempts != 1 || o.err != "the demo could not be read: bad packet" {
		t.Errorf("bad: %+v", o)
	}
	for _, p := range processed {
		if strings.HasPrefix(p, "notademo") || strings.HasPrefix(p, "tampered") {
			t.Errorf("%s reached the parser", p)
		}
	}

	// Anything else is retried, then failed with the attempts used up.
	if o := outcomeOf(flakyJob); o.job != "failed" || o.attempts != 2 || o.err != "processing failed: out of memory" {
		t.Errorf("flaky: %+v", o)
	}
	if !b.has(store.JobLogKey(flakyJob, 1)) || !b.has(store.JobLogKey(flakyJob, 2)) {
		t.Error("flaky: each attempt keeps its own log")
	}
	if fmt.Sprint(published) != "[good packed]" {
		t.Errorf("published %v", published)
	}

	// A job whose worker went quiet is claimed again once its lease lapses,
	// and not before.
	_, staleJob := upload("stale", append(bytes.Clone(demo), 5))
	exec(`UPDATE jobs SET state = 'running', attempts = 1, locked_by = 'asleep', locked_at = now() WHERE id = $1`, staleJob)
	w.Run(ctx)
	if o := outcomeOf(staleJob); o.job != "running" {
		t.Errorf("a live lease was taken: %+v", o)
	}
	exec(`UPDATE jobs SET locked_at = now() - interval '3 minutes' WHERE id = $1`, staleJob)
	w.Run(ctx)
	if o := outcomeOf(staleJob); o.job != "done" || o.attempts != 2 {
		t.Errorf("lapsed lease: %+v", o)
	}

	// ...but one that has taken a worker down every time is given up on.
	_, crashJob := upload("crasher", append(bytes.Clone(demo), 6))
	exec(`UPDATE jobs SET state = 'running', attempts = 2, locked_by = 'gone', locked_at = now() - interval '1 hour' WHERE id = $1`, crashJob)
	w.Run(ctx)
	if o := outcomeOf(crashJob); o.job != "failed" || o.attempts != 3 || !strings.Contains(o.err, "stopped partway 2 times") {
		t.Errorf("crasher: %+v", o)
	}

	// The worker reported in.
	var seen time.Time
	var version string
	if err := db.QueryRow(ctx, `SELECT seen_at, version FROM workers WHERE name = 'test-pc'`).Scan(&seen, &version); err != nil {
		t.Fatal(err)
	}
	if time.Since(seen) > time.Minute || version != "test" {
		t.Errorf("workers row: seen %v ago, version %s", time.Since(seen), version)
	}
}

// A worker stopped mid-job hands the job back without spending an attempt.
func TestShutdownHandsBack(t *testing.T) {
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
	db.Exec(ctx, `DELETE FROM demos`)
	db.Exec(ctx, `INSERT INTO users (steamid, display_name) VALUES ($1, 'me') ON CONFLICT DO NOTHING`, uploader)
	body := append([]byte("PBDEMS2\x00"), 1, 2, 3)
	sum := sha256.Sum256(body)
	var demoID string
	var jobID int64
	db.QueryRow(ctx, `INSERT INTO demos (name, sha256, uploaded_by, size_bytes, status) VALUES ('slow', $1, $2, $3, 'queued') RETURNING id::text`,
		hex.EncodeToString(sum[:]), uploader, len(body)).Scan(&demoID)
	db.QueryRow(ctx, `INSERT INTO jobs (demo_id) VALUES ($1) RETURNING id`, demoID).Scan(&jobID)
	b := &bucket{objects: map[string][]byte{store.IncomingKey(demoID): body}}

	runCtx, stop := context.WithCancel(ctx)
	started := make(chan struct{})
	w := &Worker{
		DB: db, Store: b,
		Process: func(ctx context.Context, _, _, _ string, _ io.Writer) error {
			close(started)
			<-ctx.Done() // a parse in progress, interrupted
			return ctx.Err()
		},
		Publish: func(context.Context, string, io.Writer) error { return errors.New("not reached") },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:  Config{Name: "test-pc", Version: "test", Scratch: t.TempDir(), Timeout: time.Minute},
	}
	done := make(chan error)
	go func() { done <- w.Run(runCtx) }()
	<-started
	var status string
	db.QueryRow(ctx, `SELECT status::text FROM demos WHERE id = $1`, demoID).Scan(&status)
	if status != "parsing" {
		t.Errorf("while processing, the demo is %s", status)
	}
	stop()
	<-done

	var state string
	var attempts int
	db.QueryRow(ctx, `SELECT j.state::text, j.attempts, d.status::text FROM jobs j JOIN demos d ON d.id = j.demo_id WHERE j.id = $1`, jobID).
		Scan(&state, &attempts, &status)
	if state != "queued" || attempts != 0 || status != "queued" {
		t.Errorf("after shutdown: job %s, %d attempts, demo %s", state, attempts, status)
	}
}
