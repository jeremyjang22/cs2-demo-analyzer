package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/store"
)

// Job is a claimed job and the upload it is for.
type Job struct {
	ID      int64
	Attempt int // 1 on the first try
	DemoID  string
	Name    string // the demo's name on the site, decided at upload
	SHA256  string // as the browser hashed it
	Size    int64  // as the browser declared it, and R2 enforced
}

func jobLogKey(j *Job) string { return store.JobLogKey(j.ID, j.Attempt) }

// run is the job itself. Each step is timed into steps, so the jobs row
// says where the time went.
//
// The order matters. The original is kept before anything is published, so
// a demo on the site always has its original to be rebuilt from; the upload
// is deleted only once everything else has worked, so a retry has it to
// start from again.
func (w *Worker) run(ctx context.Context, j *Job, log io.Writer, steps *steps) error {
	dir := filepath.Join(w.Scratch, fmt.Sprintf("job-%d", j.ID))
	if err := os.RemoveAll(dir); err != nil { // a crashed attempt's leftovers
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	if err := markProcessing(ctx, w.DB, j); err != nil {
		return err
	}

	upload := filepath.Join(dir, "upload")
	if err := steps.run("download", func() error { return w.download(ctx, j, upload) }); err != nil {
		return err
	}

	var demo string
	var zst bool
	if err := steps.run("check", func() error {
		var err error
		if zst, err = SniffFile(upload); err != nil {
			return err
		}
		// The parser picks decompression by the file's name.
		demo = filepath.Join(dir, j.Name+".dem")
		if zst {
			demo += ".zst"
		}
		return os.Rename(upload, demo)
	}); err != nil {
		return err
	}

	out := filepath.Join(dir, "out")
	if err := steps.run("process", func() error {
		pctx, cancel := context.WithTimeout(ctx, w.Timeout)
		defer cancel()
		err := w.Process(pctx, demo, out, j.Name, log)
		if err != nil && errors.Is(pctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			// Retrying would take as long again: a demo this slow needs a
			// look, not three more tries.
			return Permanent("processing took longer than %v", w.Timeout)
		}
		return err
	}); err != nil {
		return err
	}

	if err := steps.run("keep", func() error { return w.keep(ctx, j, demo, zst, dir, log) }); err != nil {
		return err
	}
	if err := steps.run("publish", func() error { return w.Publish(ctx, filepath.Join(out, j.Name), log) }); err != nil {
		return err
	}
	return steps.run("tidy", func() error { return w.Store.Delete(ctx, store.IncomingKey(j.DemoID)) })
}

// download fetches the upload to path, hashing it on the way, and checks it
// is the file the browser declared: same length, same SHA-256. The hash is
// what dedupes uploads and names the kept original, so a file that changed
// in transit must not get this far.
func (w *Worker) download(ctx context.Context, j *Job, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := w.Store.GetTo(ctx, store.IncomingKey(j.DemoID), io.MultiWriter(f, h))
	if errors.Is(err, store.ErrNotFound) {
		// Most likely the week-old rule: storage clears incoming/ after seven
		// days, and the upload waited longer than that.
		return Permanent("the uploaded file is no longer in storage; upload it again")
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if n != j.Size {
		return Permanent("the stored file is %d bytes, not the %d declared; upload it again", n, j.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != j.SHA256 {
		return Permanent("the stored file is not the one that was uploaded (its SHA-256 differs); upload it again")
	}
	return nil
}

// keep stores the original as raw/<sha256>.dem.zst, compressing it first
// unless it arrived compressed. Content-addressed: one that is already
// there is the same bytes, and is left alone.
func (w *Worker) keep(ctx context.Context, j *Job, demo string, zst bool, dir string, log io.Writer) error {
	key := store.RawKey(j.SHA256)
	if ok, err := w.Store.Exists(ctx, key); err != nil {
		return err
	} else if ok {
		fmt.Fprintf(log, "  %s already kept\n", key)
		return nil
	}
	path := demo
	if !zst {
		path = filepath.Join(dir, "original.dem.zst")
		start := time.Now()
		size, err := compress(demo, path)
		if err != nil {
			return fmt.Errorf("compress the original: %w", err)
		}
		fmt.Fprintf(log, "  compressed %d MB to %d MB in %.1fs\n", j.Size>>20, size>>20, time.Since(start).Seconds())
	}
	return w.Store.PutFile(ctx, key, path, "application/zstd")
}

// compress writes src to dst as zstd and returns dst's size.
func compress(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	enc, err := zstd.NewWriter(out)
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(enc, in); err != nil {
		enc.Close()
		return 0, err
	}
	if err := enc.Close(); err != nil {
		return 0, err
	}
	st, err := out.Stat()
	if err != nil {
		return 0, err
	}
	return st.Size(), out.Close()
}

// steps times a job's steps, by name, in milliseconds.
type steps struct {
	ms  map[string]int64
	log io.Writer
}

func newSteps(log io.Writer) *steps { return &steps{ms: map[string]int64{}, log: log} }

func (s *steps) run(name string, fn func() error) error {
	fmt.Fprintf(s.log, "%s\n", name)
	start := time.Now()
	err := fn()
	took := time.Since(start)
	s.ms[name] = took.Milliseconds()
	if err == nil {
		fmt.Fprintf(s.log, "  %s took %.1fs\n", name, took.Seconds())
	}
	return err
}

// permanent is a failure that trying again cannot fix - the file itself is
// wrong. Its message is what the uploader is shown.
type permanent struct{ msg string }

func (p *permanent) Error() string { return p.msg }

// Permanent is a failure not worth retrying, worded for the uploader.
func Permanent(format string, args ...any) error {
	return &permanent{fmt.Sprintf(format, args...)}
}

// IsPermanent reports whether err should fail the job at once.
func IsPermanent(err error) bool {
	var p *permanent
	return errors.As(err, &p)
}
