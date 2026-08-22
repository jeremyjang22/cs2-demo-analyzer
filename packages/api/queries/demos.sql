-- name: GetDemoBySHA :one
-- The dedup fast path. Called before handing out an upload URL: if the hash is
-- already known, the uploader skips the 300 MB transfer entirely.
SELECT * FROM demos WHERE sha256 = $1;

-- name: CreateDemo :one
INSERT INTO demos (sha256, uploaded_by, original_filename, size_bytes)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: MarkDemoParsed :exec
UPDATE demos
SET status = 'ready', map = $2, tick_rate = $3, rounds = $4, complete = $5,
    schema_version = $6, collector_version = $7, parsed_at = now()
WHERE id = $1;

-- name: ListMyDemos :many
-- Every demo the caller appears in, whether or not they uploaded it - which is
-- the point of demo_players. Visibility still applies: a demo someone else
-- uploaded is only listed if they shared it.
SELECT d.*, dp.kills, dp.deaths, dp.assists
FROM demos d
JOIN demo_players dp ON dp.demo_id = d.id
WHERE dp.steamid = $1
  AND d.status = 'ready'
  AND (d.uploaded_by = $1 OR d.visibility <> 'private')
ORDER BY d.created_at DESC
LIMIT $2 OFFSET $3;
