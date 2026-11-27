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

-- What a person says the game was. Undeclared (null) is listed as "unknown".
CREATE TYPE demo_kind AS ENUM ('pug', 'match');

-- The catalogue: every demo the site can show is a row here, uploaded through
-- the site or published by hand with demo-export. data/index.json, which the
-- Worker reads to decide who sees what, is generated from this table and is
-- never edited on its own (packages/catalog). See docs/uploads.md.
CREATE TABLE demos (
    id                uuid            PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The demo's name on the site: data/<name>/ in storage, ?demo=<name> in
    -- the address. Restricted to what is safe in both without escaping.
    name              text            NOT NULL UNIQUE
                      CHECK (name ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'),

    -- Content address of the original .dem. The dedup key: a demo uploaded
    -- twice is parsed once ever, and the second uploader gets an instant
    -- result. Null for a demo published by hand from files without it.
    sha256            char(64)        UNIQUE,

    -- Set for uploads through the site; null for a demo published by hand.
    uploaded_by       bigint          REFERENCES users (steamid),
    original_filename text,
    size_bytes        bigint,

    status            demo_status     NOT NULL DEFAULT 'uploading',

    -- DECLARED by a person, never guessed: who may see it and what it was.
    -- This is info.json's half of an index entry (scripts/demo-info.mjs).
    visibility        demo_visibility NOT NULL DEFAULT 'private',
    -- Whose demo it is, a 64-bit steamid. Not a reference to users: a demo
    -- can belong to someone who has never signed in.
    owner             bigint,
    kind              demo_kind,
    event             text,
    played_on         date,
    title             text,
    -- What a signed-out visitor is shown. The Worker would refuse a private
    -- sample to exactly the people it is for, so the database refuses it.
    sample            boolean         NOT NULL DEFAULT false,
    CONSTRAINT demos_sample_is_public CHECK (NOT sample OR visibility = 'public'),

    -- MEASURED from movement.json when it is built: the measured half of an
    -- index entry (games, map, rounds, players, kb, seconds, score, lines),
    -- exactly as the index carries it. Which pros are in the demo is NOT
    -- stored: it is matched against pros.json when the index is generated, so
    -- adding a pro lists their existing games without reprocessing.
    summary           jsonb,

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
    -- The worker running it: workers.name.
    locked_by  text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- The latest attempt: when it started and ended, and how long each step
    -- took in milliseconds ({"download": 8123, "process": 10950, ...}). So
    -- "why was that one slow" is a query, wherever the worker ran. The
    -- attempt's full log is in the bucket at logs/jobs/<id>-<attempt>.txt.
    started_at  timestamptz,
    finished_at timestamptz,
    steps       jsonb
);

-- The claim query orders by this; partial index keeps it to live work only.
CREATE INDEX jobs_claimable_idx ON jobs (created_at) WHERE state = 'queued';
CREATE UNIQUE INDEX jobs_one_live_per_demo_idx ON jobs (demo_id)
    WHERE state IN ('queued', 'running');

-- Every demo-worker that has run, and when it was last alive. A worker
-- reports in every 30 seconds while it runs, so "nobody has reported in for
-- two minutes" is how the site knows an upload is waiting on a machine that
-- is off (the owner's PC asleep) rather than on a slow parse.
CREATE TABLE workers (
    -- The machine's host name: one row per machine, however often it restarts.
    name       text        PRIMARY KEY,
    version    text        NOT NULL,
    started_at timestamptz NOT NULL,
    seen_at    timestamptz NOT NULL,
    -- What it is processing now; null when idle.
    job_id     bigint
);
