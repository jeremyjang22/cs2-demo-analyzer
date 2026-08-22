-- name: ClaimJob :one
-- SKIP LOCKED is what lets several workers poll the same table without
-- blocking each other or handing the same demo to two of them. Jobs whose
-- worker died mid-parse are reclaimed once their lock goes stale, so a crash
-- costs one retry rather than a lost upload.
UPDATE jobs SET
    state = 'running',
    attempts = attempts + 1,
    locked_at = now(),
    locked_by = $1,
    updated_at = now()
WHERE id = (
    SELECT id FROM jobs
    WHERE (state = 'queued')
       OR (state = 'running' AND locked_at < now() - $2::interval)
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: EnqueueJob :one
INSERT INTO jobs (demo_id) VALUES ($1) RETURNING *;

-- name: FinishJob :exec
UPDATE jobs SET state = 'done', locked_by = NULL, updated_at = now() WHERE id = $1;

-- name: FailJob :exec
-- Left in 'queued' while attempts remain so the poll picks it up again;
-- 'failed' is terminal.
UPDATE jobs SET
    state = CASE WHEN attempts >= $2 THEN 'failed'::job_state ELSE 'queued'::job_state END,
    last_error = $3,
    locked_by = NULL,
    locked_at = NULL,
    updated_at = now()
WHERE id = $1;
