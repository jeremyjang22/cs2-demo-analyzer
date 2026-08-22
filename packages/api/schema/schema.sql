-- Schema for the hosted viewer: who signed in, which demos exist, and the
-- work queue that turns an upload into something the frontend can draw.
--
-- Read by BOTH Atlas (to generate migrations) and sqlc (to generate Go), so
-- it is plain SQL rather than Atlas HCL. One source of truth for both.
--
-- Deliberately absent: tick data. Ticks live in Parquet next to the demo, not
-- in Postgres. 100 demos is ~316M tick rows, and the access pattern is "give
-- me one whole round", which is a file read rather than a query. This database
-- exists to find things, not to store them.

-- Steam is the identity provider, so the steamid IS the user. It is also the
-- join key already present in every parsed CSV, which is why Steam login was
-- worth preferring over email: "my games" needs no extra mapping step.
CREATE TABLE users (
    steamid       bigint      PRIMARY KEY,
    display_name  text        NOT NULL,
    avatar_url    text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE demo_status AS ENUM ('uploading', 'queued', 'parsing', 'ready', 'failed');

-- A demo contains ten people's data, so who may see it is a real decision
-- rather than a default. 'private' is the uploader only; 'link' is anyone
-- holding the id; 'public' is listed.
CREATE TYPE demo_visibility AS ENUM ('private', 'link', 'public');

CREATE TABLE demos (
    id                uuid            PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Content address. The dedup key: a demo uploaded twice is parsed once
    -- ever, and the second uploader gets an instant result. Also what makes a
    -- re-parse idempotent, since filenames are not stable identifiers.
    sha256            char(64)        NOT NULL UNIQUE,

    uploaded_by       bigint          NOT NULL REFERENCES users (steamid),
    original_filename text            NOT NULL,
    size_bytes        bigint          NOT NULL,

    status            demo_status     NOT NULL DEFAULT 'uploading',
    visibility        demo_visibility NOT NULL DEFAULT 'private',

    -- Populated once parsing succeeds; null until then.
    map               text,
    tick_rate         real,
    rounds            integer,
    -- False when the recording cut off before the last round's official end.
    -- Routinely false on a normal demo - see round-collector-schema.md.
    complete          boolean,

    -- Which collector produced the output, so "reprocess everything older
    -- than X" is a query rather than a guess.
    schema_version    text,
    collector_version text,

    created_at        timestamptz     NOT NULL DEFAULT now(),
    parsed_at         timestamptz
);

CREATE INDEX demos_uploaded_by_idx ON demos (uploaded_by, created_at DESC);
CREATE INDEX demos_status_idx      ON demos (status);

-- One row per player per demo. Exists so "my games" is a single indexed
-- lookup on steamid instead of opening every demo's parsed output, and so the
-- demo list can show K/D/A without touching Parquet at all.
CREATE TABLE demo_players (
    demo_id   uuid   NOT NULL REFERENCES demos (id) ON DELETE CASCADE,
    steamid   bigint NOT NULL,
    name      text   NOT NULL,
    -- Roster index, stable across the halftime side swap - NOT the CS team
    -- number, which changes at half.
    team      smallint NOT NULL,
    kills     integer  NOT NULL DEFAULT 0,
    deaths    integer  NOT NULL DEFAULT 0,
    assists   integer  NOT NULL DEFAULT 0,

    PRIMARY KEY (demo_id, steamid)
);

CREATE INDEX demo_players_steamid_idx ON demo_players (steamid);

CREATE TYPE job_state AS ENUM ('queued', 'running', 'done', 'failed');

-- Parsing takes 12-35s, far past any HTTP timeout, so uploads enqueue work
-- rather than doing it inline. Postgres is the queue: SELECT ... FOR UPDATE
-- SKIP LOCKED is sufficient at this scale and avoids running a broker for one
-- queue. attempts + locked_at are what let a crashed worker's job be retried
-- instead of being lost.
CREATE TABLE jobs (
    id         bigserial   PRIMARY KEY,
    demo_id    uuid        NOT NULL REFERENCES demos (id) ON DELETE CASCADE,
    state      job_state   NOT NULL DEFAULT 'queued',
    attempts   integer     NOT NULL DEFAULT 0,
    last_error text,
    locked_at  timestamptz,
    locked_by  text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The claim query orders by this; partial index keeps it to live work only.
CREATE INDEX jobs_claimable_idx ON jobs (created_at) WHERE state = 'queued';
CREATE UNIQUE INDEX jobs_one_live_per_demo_idx ON jobs (demo_id)
    WHERE state IN ('queued', 'running');
