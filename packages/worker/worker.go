// Package worker processes uploaded demos: it takes jobs off the queue in
// Postgres, turns each upload into the site's files, and publishes them.
//
// It only ever makes outbound connections - Postgres and the bucket - so the
// same code runs on the owner's PC (started at login) and as a cloud service,
// and any number of workers can share one queue: a job is claimed with
// SELECT ... FOR UPDATE SKIP LOCKED, so each goes to exactly one.
//
// A claim is a lease. The worker renews it every Heartbeat while the job
// runs, and a job whose lease has not been renewed for LeaseFor is taken to
// be abandoned - the machine slept, crashed or lost its connection - and is
// claimed again by whichever worker looks next. That is what lets a job
// survive the PC being switched off mid-parse without anyone noticing.
//
// docs/uploads.md has the whole flow; docs/demo-worker.md is how to run it
// and where its logs are.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// Heartbeat is how often a running worker reports in and renews the
	// lease on the job it holds.
	Heartbeat = 30 * time.Second
	// LeaseFor is how long a job's lease lasts unrenewed: four missed
	// heartbeats. The API calls a worker offline after the same time.
	LeaseFor = 2 * time.Minute
)

// Storage is the bucket, as much of it as a job needs. *store.Store.
type Storage interface {
	GetTo(ctx context.Context, key string, w io.Writer) (int64, error)
	Put(ctx context.Context, key string, body []byte, contentType string) error
	PutFile(ctx context.Context, key, path, contentType string) error
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
}

// ProcessFunc builds the site's files for one demo, into <out>/<name>/.
// Anything it prints goes to log, which becomes the job's log. Subprocess is
// the real one.
type ProcessFunc func(ctx context.Context, demo, out, name string, log io.Writer) error

// PublishFunc puts a built folder on the site: payloads to the bucket, the
// row marked ready, the index regenerated. catalog.Publisher.Put.
type PublishFunc func(ctx context.Context, dir string, log io.Writer) error

// Config is how a worker behaves. Name and Version identify it in the
// workers table.
type Config struct {
	Name        string        // this machine; workers.name and jobs.locked_by
	Version     string        // the build, so a log says which code ran
	Scratch     string        // where a job's files go while it runs
	Poll        time.Duration // how often an idle worker looks for work
	Timeout     time.Duration // the longest one demo may take to process
	MaxAttempts int           // tries before a job is failed for good
	Once        bool          // stop when the queue is empty, instead of waiting
}

// Worker is one machine's loop. Fill the fields, then Run.
type Worker struct {
	DB      *pgxpool.Pool
	Store   Storage
	Process ProcessFunc
	Publish PublishFunc
	Log     *slog.Logger
	Config

	started time.Time
	mu      sync.Mutex
	current *Job
	lost    context.CancelCauseFunc
}

// errLostLease cancels a job whose lease another worker has taken over: this
// one was stalled past LeaseFor, and the job is no longer its to finish.
var errLostLease = errors.New("the lease on this job was lost to another worker")

// Run processes jobs until ctx is cancelled (or, with Once, until there are
// none). A job running when ctx is cancelled is handed back to the queue
// without counting as an attempt.
//
// The database being unreachable is not fatal: a PC waking from sleep spends
// a few seconds without a network, and the loop simply tries again.
func (w *Worker) Run(ctx context.Context) error {
	if w.Poll <= 0 {
		w.Poll = 5 * time.Second
	}
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 3
	}
	w.started = time.Now()
	w.Log.Info("worker started", "name", w.Name, "version", w.Version)

	beat, stopBeat := context.WithCancel(ctx)
	defer stopBeat()
	go w.heartbeat(beat)

	for ctx.Err() == nil {
		job, err := claim(ctx, w.DB, w.Name)
		if err != nil {
			if ctx.Err() == nil {
				w.Log.Warn("could not reach the queue", "err", err)
				sleep(ctx, w.Poll)
			}
			continue
		}
		if job == nil {
			if w.Once {
				return nil
			}
			sleep(ctx, w.Poll)
			continue
		}
		w.handle(ctx, job)
	}
	w.Log.Info("worker stopped")
	return nil
}

// handle runs one claimed job to an outcome, and records it.
func (w *Worker) handle(ctx context.Context, j *Job) {
	log := w.Log.With("job", j.ID, "attempt", j.Attempt, "demo", j.Name)
	// The job's own writes must land even when ctx is what ended it.
	after := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	}

	if j.Attempt > w.MaxAttempts {
		// Claimed again after its lease lapsed every time: whatever this
		// demo does to a worker, it does it every time.
		c, cancel := after()
		defer cancel()
		err := Permanent("processing stopped partway %d times; this demo needs a look by hand", j.Attempt-1)
		if ferr := fail(c, w.DB, j, w.Name, nil, err, true); ferr != nil {
			log.Error("could not record the failure", "err", ferr)
		}
		log.Error("job failed", "err", err)
		return
	}

	jobCtx, cancelJob := context.WithCancelCause(ctx)
	defer cancelJob(nil)
	w.mu.Lock()
	w.current, w.lost = j, cancelJob
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.current, w.lost = nil, nil
		w.mu.Unlock()
	}()
	w.beat(ctx) // the site shows what is being worked on now, not in 30s

	log.Info("job claimed", "size", j.Size)
	jl := NewJobLog()
	fmt.Fprintf(jl, "job %d, attempt %d, demo %s (%s), on %s running %s\n",
		j.ID, j.Attempt, j.Name, j.DemoID, w.Name, w.Version)
	steps := newSteps(jl)
	err := w.run(jobCtx, j, jl, steps)

	c, cancel := after()
	defer cancel()
	switch {
	case err == nil:
		fmt.Fprintf(jl, "done\n")
		if err := finish(c, w.DB, j, w.Name, steps.ms); err != nil {
			log.Error("processed, but the job could not be marked done", "err", err)
		}
		log.Info("job done", "steps_ms", steps.ms)
	case errors.Is(context.Cause(jobCtx), errLostLease):
		fmt.Fprintf(jl, "stopped: %v\n", errLostLease)
		log.Warn("job abandoned", "err", errLostLease)
	case ctx.Err() != nil:
		fmt.Fprintf(jl, "stopped: the worker is shutting down; the job goes back to the queue\n")
		if err := release(c, w.DB, j, w.Name); err != nil {
			log.Error("could not hand the job back", "err", err)
		}
		log.Info("job handed back")
	default:
		final := IsPermanent(err) || j.Attempt >= w.MaxAttempts
		fmt.Fprintf(jl, "failed: %v\n", err)
		if ferr := fail(c, w.DB, j, w.Name, steps.ms, err, final); ferr != nil {
			log.Error("could not record the failure", "err", ferr)
		}
		if final {
			log.Error("job failed", "err", err, "steps_ms", steps.ms)
		} else {
			log.Warn("job failed, will retry", "err", err, "steps_ms", steps.ms)
		}
	}

	// The log is how a failure at 2am is understood the next morning. Best
	// effort: losing it must not lose the job's outcome, written above.
	key := jobLogKey(j)
	if err := w.Store.Put(c, key, jl.Bytes(), "text/plain; charset=utf-8"); err != nil {
		log.Warn("job log not stored", "err", err)
	} else {
		log.Info("job log stored", "key", key)
	}
}

// heartbeat reports in every Heartbeat until ctx ends.
func (w *Worker) heartbeat(ctx context.Context) {
	t := time.NewTicker(Heartbeat)
	defer t.Stop()
	for {
		w.beat(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// beat says this worker is alive and what it is doing, and renews the lease
// on its job - cancelling the job if the lease has gone to another worker.
func (w *Worker) beat(ctx context.Context) {
	w.mu.Lock()
	j, lost := w.current, w.lost
	w.mu.Unlock()
	var jobID *int64
	if j != nil {
		jobID = &j.ID
	}
	if err := reportIn(ctx, w.DB, w.Name, w.Version, w.started, jobID); err != nil && ctx.Err() == nil {
		w.Log.Warn("could not report in", "err", err)
	}
	if j == nil {
		return
	}
	held, err := renew(ctx, w.DB, j, w.Name)
	if err != nil {
		if ctx.Err() == nil {
			w.Log.Warn("could not renew the lease", "job", j.ID, "err", err)
		}
		return
	}
	if !held {
		lost(errLostLease)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
