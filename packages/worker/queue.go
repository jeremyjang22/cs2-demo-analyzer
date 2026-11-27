package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// claim takes the oldest job that is waiting, or whose lease has lapsed, and
// makes it this worker's. nil when there is none.
//
// Every claim counts as an attempt, a lapsed one included: a demo that
// takes the machine down with it every time must not be retried forever.
func claim(ctx context.Context, db *pgxpool.Pool, worker string) (*Job, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	j := &Job{}
	err = tx.QueryRow(ctx, `
WITH next AS (
    SELECT id FROM jobs
    WHERE state = 'queued'
       OR (state = 'running' AND locked_at < now() - make_interval(secs => $2))
    ORDER BY created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs j SET state = 'running', attempts = j.attempts + 1, locked_at = now(), locked_by = $1,
                  started_at = now(), finished_at = NULL, steps = NULL, updated_at = now()
FROM next WHERE j.id = next.id
RETURNING j.id, j.attempts, j.demo_id::text`, worker, LeaseFor.Seconds()).Scan(&j.ID, &j.Attempt, &j.DemoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var sha *string
	var size *int64
	if err := tx.QueryRow(ctx, `SELECT name, sha256, size_bytes FROM demos WHERE id = $1`, j.DemoID).
		Scan(&j.Name, &sha, &size); err != nil {
		return nil, fmt.Errorf("job %d: its demo: %w", j.ID, err)
	}
	if sha == nil || size == nil {
		// Only an upload has a job, and every upload has both. A row that
		// does not is not something to guess at.
		return nil, fmt.Errorf("job %d: demo %s has no declared hash or size", j.ID, j.Name)
	}
	j.SHA256, j.Size = *sha, *size
	return j, tx.Commit(ctx)
}

// markProcessing is what the uploader's row shows while the job runs.
func markProcessing(ctx context.Context, db *pgxpool.Pool, j *Job) error {
	_, err := db.Exec(ctx, `UPDATE demos SET status = 'parsing' WHERE id = $1 AND status <> 'ready'`, j.DemoID)
	return err
}

// The writes below all name the worker and the attempt, so a worker that
// lost its lease cannot record an outcome over the one that took it over.

// finish records a job done. Publishing has already marked the demo ready.
func finish(ctx context.Context, db *pgxpool.Pool, j *Job, worker string, steps map[string]int64) error {
	raw, _ := json.Marshal(steps)
	_, err := db.Exec(ctx, `
UPDATE jobs SET state = 'done', finished_at = now(), steps = $4::jsonb, last_error = NULL,
                locked_at = NULL, updated_at = now()
WHERE id = $1 AND locked_by = $2 AND attempts = $3 AND state = 'running'`, j.ID, worker, j.Attempt, string(raw))
	return err
}

// fail records a failed attempt: back to the queue, or - final - failed for
// good, with the reason on the demo's row for its uploader to read.
func fail(ctx context.Context, db *pgxpool.Pool, j *Job, worker string, steps map[string]int64, cause error, final bool) error {
	raw, _ := json.Marshal(steps)
	state, demo := "queued", "queued"
	if final {
		state, demo = "failed", "failed"
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE jobs SET state = $4::job_state, last_error = $5, finished_at = now(), steps = $6::jsonb,
                locked_at = NULL, locked_by = CASE WHEN $4 = 'queued' THEN NULL ELSE locked_by END,
                updated_at = now()
WHERE id = $1 AND locked_by = $2 AND attempts = $3 AND state = 'running'`,
		j.ID, worker, j.Attempt, state, Reason(cause), string(raw))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // no longer ours
	}
	if _, err := tx.Exec(ctx, `UPDATE demos SET status = $2::demo_status WHERE id = $1 AND status <> 'ready'`, j.DemoID, demo); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// release hands a job back untried: the worker is stopping, and the attempt
// it was making is not the demo's fault.
func release(ctx context.Context, db *pgxpool.Pool, j *Job, worker string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE jobs SET state = 'queued', attempts = attempts - 1, locked_at = NULL, locked_by = NULL,
                started_at = NULL, updated_at = now()
WHERE id = $1 AND locked_by = $2 AND attempts = $3 AND state = 'running'`, j.ID, worker, j.Attempt)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE demos SET status = 'queued' WHERE id = $1 AND status = 'parsing'`, j.DemoID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// renew extends the lease on a running job, reporting false when the job is
// no longer this worker's.
func renew(ctx context.Context, db *pgxpool.Pool, j *Job, worker string) (bool, error) {
	tag, err := db.Exec(ctx, `
UPDATE jobs SET locked_at = now(), updated_at = now()
WHERE id = $1 AND locked_by = $2 AND attempts = $3 AND state = 'running'`, j.ID, worker, j.Attempt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// reportIn is the heartbeat row: alive now, and on which job.
func reportIn(ctx context.Context, db *pgxpool.Pool, worker, version string, started time.Time, job *int64) error {
	_, err := db.Exec(ctx, `
INSERT INTO workers (name, version, started_at, seen_at, job_id) VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (name) DO UPDATE SET version = excluded.version, started_at = excluded.started_at,
                                 seen_at = now(), job_id = excluded.job_id`, worker, version, started, job)
	return err
}

// Reason is a failure as the uploader reads it on their row. A permanent one
// was worded for them; anything else is our problem, said briefly, with the
// details left in the job's log.
func Reason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if !IsPermanent(err) {
		msg = "processing failed: " + msg
	}
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:297]) + "..."
	}
	return msg
}
