# Uploading demos through the site

Status: **phases A and C built** (2026-11-26); D and E to come. Decisions dated where made.

The goal: open the site on any machine, upload a `.dem`, and have it appear
as a replay a minute later - processed by a worker that runs on the owner's
PC today and on Northflank later, with no code change between the two.

## The flow

```
 any machine (a browser)                Cloudflare                     worker: owner's PC → Northflank
 ┌────────────────────────┐  1 ask    ┌──────────────┐               ┌──────────────────────────┐
 │ Your demos ▸ Upload    │─────────► │ Worker /api/*│──► Go API ─┐  │ demo-worker (Go)          │
 │ hash, then PUT the file│◄──────────│              │ presigned  │  │  claim job (SKIP LOCKED)  │
 │ straight to storage ───┼─┐ 2 link  └──────────────┘ PUT URL    │  │  fetch + verify the .dem  │
 │ row: queued → ready    │ │                                     ▼  │  payload + analytics      │
 └────────────────────────┘ │ 3 PUT  ┌───────────────┐  ┌──────────┐ │  store data/<name>/       │
                            └──────► │ R2 incoming/  │  │ Supabase │ │  keep raw/<sha>.dem.zst   │
                                     │ R2 raw/       │◄─│ demos    │◄┤  mark ready, publish index│
                                     │ R2 data/ (served)│ jobs    │ └──────────────────────────┘
                                     └───────────────┘  └──────────┘
```

1. **Ask.** The page hashes the file (SHA-256, in 8 MB slices so a 670 MB
   demo is never in memory at once) and asks the API for an upload.
   The API checks: signed in, on the uploader allowlist, under the size cap
   (1 GB), under a daily quota. A demo whose hash is already known returns the
   existing demo at once. Otherwise it inserts a `demos` row (`uploading`) and
   returns a presigned R2 PUT for `incoming/<id>`, valid for an hour and bound
   to the declared length - R2 itself refuses a body of any other size
   (checked 2026-11-26). Asking again for the same unfinished upload hands out
   a fresh URL, so an interrupted transfer can be started over.
2. **Upload.** The browser PUTs the file **directly to R2**, with progress.
   Not through the Worker - Cloudflare's free plan caps a request body at
   100 MB, and demos run to 670 MB - and not through Northflank, which would
   pay for the bandwidth twice.
3. **Queue.** The page reports completion; the API HEADs the object, checks the
   size, sets `queued` and inserts a `jobs` row.
4. **Process.** `demo-worker` claims the job with `SELECT … FOR UPDATE SKIP
   LOCKED`, so any number of workers can run against one queue and each job
   goes to exactly one. It downloads the upload, re-hashes it against the
   declared SHA-256, checks it is a CS2 demo (`PBDEMS2` magic, or zstd around
   one), and runs the pipeline `demo-export` runs (`packages/payload` +
   analytics, ~10 s a match). It writes `data/<name>/movement.json` and
   `analytics.json`, re-stores the original as `raw/<sha256>.dem.zst`, deletes
   `incoming/<id>`, fills the row's measured fields, marks it `ready`, and
   republishes the index.
5. **Show.** The row on Your demos polls its status and opens when ready.
   Uploads are private to the uploader by default.

The worker only ever makes outbound connections (Postgres, R2). Nothing has to
reach the PC: no open port, no tunnel. That is what makes "move it to the
cloud" a deployment change.

## The catalogue: the database is the list

Until now the list of demos was `index.json`, built from whatever folders sat
on the publishing machine's disk. Uploads make the database the single source
of truth instead - every demo, uploaded or hand-published, pro or sample, is a
`demos` row - and `index.json` becomes a **projection**: regenerated from the
database after every change and written to R2. The Worker keeps reading the
same file with the same access code (`worker/access.ts`); it does not learn
that a database exists. Go writes files; nothing calls back.

What a row holds, split the way `info.json` already splits it:

- **Declared** by a person: `visibility`, `owner`, `kind`, `event`,
  `played_on`, `title`, `sample`.
- **Measured** from the payload when it is built: map, rounds, players, length,
  score, every player's K/D/A/ADR, the roster. Stored as `summary jsonb`, the
  measured half of an index entry. Which pros a demo contains is NOT stored:
  it is matched against `pros.json` when the index is generated, so adding a
  pro to the catalogue lists their existing games without reprocessing.

Hand-published demos (`demo-export` from a machine that has the `.dem`) go
through the same table, so there is one path to the site, not two.

## Storage

| Prefix | What | Served |
|---|---|---|
| `incoming/<id>` | an upload in flight, as the browser sent it | never |
| `raw/<sha256>.dem.zst` | every original, zstd-compressed (~3-5× smaller) | never |
| `data/<name>/movement.json`, `analytics.json` | the replay and its analytics | via the Worker, per `index.json` |
| `data/index.json` | the catalogue projection | via the Worker, filtered per viewer |

2026-11-26: originals are **kept, compressed**, so any demo can be
re-processed after a payload change without asking anyone to upload it again.
R2's free tier is 10 GB; past that it is paid per GB, so this is the line to
revisit first if storage grows. An R2 lifecycle rule deletes `incoming/` after
a week, for uploads abandoned half way.

## Safety

An upload is a file from someone else, parsed by code on the owner's PC.

- **Allowlist first** (2026-11-26): only listed Steam accounts can upload.
- **Fail closed everywhere**: unknown size, hash mismatch, not a demo,
  over quota - rejected, with the reason on the row.
- **Bounded processing**: a time limit per job, the parser's panics recovered,
  three attempts, then `failed` with `last_error`.
- **Private by default**, owned by the uploader; nothing is listed until the
  uploader says otherwise.
- **Keys**: the API signs upload URLs with an R2 key scoped to this one bucket;
  the browser never holds a credential, only a URL good for one object for an
  hour.

## Configuration

| Variable | API | Worker | What |
|---|---|---|---|
| `DATABASE_URL` | ✓ | ✓ | Supabase session pooler |
| `R2_ACCOUNT_ID` | ✓ | ✓ | the S3 endpoint is `<id>.r2.cloudflarestorage.com` |
| `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | ✓ | ✓ | R2 API token, Object Read & Write, this bucket only |
| `R2_BUCKET` | ✓ | ✓ | `cs2-demo-payloads` |
| `UPLOAD_MAX_BYTES` | ✓ | | default 1 GB |

The worker reads the same names on the PC (from `packages/api/.env`) and on
Northflank (service environment). That symmetry is the whole migration.

## Phases

Each one ships on its own and leaves the site working.

| | Phase | Visible result |
|---|---|---|
| A | Catalogue in the database: schema, `summary`, existing demos registered; Go writes payloads and `index.json` to R2 from the database (`packages/catalog`, `store`, `demo-catalog`) - **done** | nothing - publishing goes through the database |
| C | Upload endpoints, allowlist, presigned PUTs, bucket CORS; Upload button and status rows (`api/internal/uploads`, `Uploads.tsx`) - **done** | rows reach "queued" |
| D | `demo-worker`, started at login on the owner's PC (2026-11-26) | an upload from another machine appears on the site |
| E | The same binary as a Northflank service, sized from its measured memory on the 670 MB reference demo | processing without the PC |

(B - giving Go R2 access - folded into A, which is the first thing to need it.)
