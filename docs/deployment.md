# Deployment

Repo to production: GitHub Actions builds and gates, Supabase holds the database,
Northflank runs the API container, Cloudflare Workers serves everything the
browser touches.

```
                 push to main
                      │
        ┌─────────────┴─────────────┐
        ▼                           ▼
   deploy-api.yml              deploy-web.yml
        │                           │
   go test                     npm test
        │                           │
   build image                 npm run build  ──► dist/
        │                           │
   push ghcr.io                wrangler deploy
        │                           │
   atlas migrate ──► Supabase       ▼
        │                     Cloudflare Worker
   northflank deploy                │
        │                    ┌──────┼──────┐
        ▼                    ▼      ▼      ▼
   Northflank service     /api/*  /data/*  /*
        ▲                    │      │      │
        └────────────────────┘   R2 bucket  static assets
```

**One origin.** The browser only ever talks to the Worker. `/api/*` is proxied
to Northflank, `/data/*` comes out of R2, everything else is the SPA. That is
not cosmetic — see [Why the proxy](#why-the-proxy).

## What ships, and what does not

| | Shipped by CI | Where it comes from |
|---|---|---|
| API container | yes | `packages/Dockerfile`, built from `packages/` |
| Database schema | yes | `atlas migrate apply` before the rollout |
| SPA bundle | yes | `npm run build` → `dist/` → Workers assets |
| Radar images | yes | synced from committed `assets/radar/` at build |
| **Demo payloads** | **no** | published by hand: `demo-export -upload` / `demo-catalog put` |

Demo payloads are the deliberate exception. They are built from `.dem` files
that never enter the repo, so CI cannot produce them and has nothing to verify
them against. Publishing data stays something a person does on purpose from the
machine that has the demos.

## One-time setup

### 1. Supabase

Create a project in the **same region as the Northflank service** (both are
`us-east-2` today); every sign-in is a round trip between them. On the create
form, **untick Enable Data API** and leave GitHub unconnected: the Go API is
the only client, and Atlas, not Supabase, owns the migrations. Keep the
generated database password in a password manager — it is shown once.

Then **Connect → Direct → Session pooler** gives the one connection string
everything uses: `postgresql://postgres.<ref>:<password>@aws-1-<region>.pooler.supabase.com:5432/postgres`.

- **Not the direct connection** (`db.<ref>.supabase.co`): IPv6-only on the free
  plan, and GitHub's runners are IPv4.
- **Not the transaction pooler** (port 6543): it hands a connection to another
  client between transactions, which breaks pgx's prepared-statement cache
  ("prepared statement already exists") and the session state Atlas needs.

For Atlas (the GitHub secret and your local `.env`), append
`?search_path=public&sslmode=require`, which keeps it out of Supabase's own
schemas. Create the tables once with `./migrate.sh migrate apply` from
`packages/api`; after that `deploy-api.yml` applies new migrations.

The free plan **pauses a project after seven days with no activity**. Restore
it from the dashboard; `/api/health` reports the database unreachable until
then.

### 2. GHCR

Nothing to create; `deploy-api.yml` pushes with the built-in `GITHUB_TOKEN`.
Images land at `ghcr.io/<owner>/cs2-demo-analyzer/api`.

**The package starts private — make it public** so Northflank can pull it
without a stored credential. After the first `deploy-api.yml` run has pushed an
image:

> GitHub → your profile → **Packages** → `cs2-demo-analyzer/api` → **Package
> settings** → Change visibility → **Public**

The image holds no secrets — every setting comes from the environment at
runtime — so the thing being made public is a compiled Go binary. In exchange
the deploy needs no personal access token to rotate and no credential id to get
wrong.

To keep it private instead: create a GitHub PAT (classic, scope
`read:packages`), add it under Northflank's **team-level** Integrations →
Registries (not project level), and pass the registry's id back to the deploy
step as the action's optional `credentials-id`. Find that id with:

```sh
curl -s -H "Authorization: Bearer $NORTHFLANK_API_KEY"   https://api.northflank.com/v1/integrations/registries
```

### 3. Northflank

Create a **deployment service** — not a combined build-and-deploy one. CI has
already built and tested the image; Northflank's job is only to run the exact
digest it is handed:

- Source: external image, `ghcr.io/<owner>/cs2-demo-analyzer/api`. No registry
  credential needed once the package is public.
- Port **8080**, HTTP, public.
- Health check: `GET /health`.
- Environment variables:

  | Variable | Value |
  |---|---|
  | `DATABASE_URL` | Supabase **session pooler** URL, port 5432 (see above) |
  | `SESSION_KEY` | `openssl rand -hex 32` — rotating it logs everyone out |
  | `PUBLIC_URL` | `https://<your-domain>/api` |
  | `WEB_ORIGIN` | `https://<your-domain>` |
  | `PORT` | `8080` |
  | `STEAM_API_KEY` | optional — [steamcommunity.com/dev/apikey](https://steamcommunity.com/dev/apikey); names and avatars at sign-in |
  | `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET` | for uploads: the same R2 token as `packages/api/.env` ([Publishing](#publishing-demo-data)) |
  | `UPLOAD_MAX_BYTES`, `UPLOAD_DAILY_LIMIT` | optional — default 1 GB and 20 a day per uploader |

  Uploads are off until the R2 variables are set: the API still starts and
  serves sign-in, logs `uploads off` with what is missing, and the site shows
  no Upload button. Everything else is the upload worker's job
  (`docs/uploads.md`).

  Who may upload is not a variable but the `uploaders` table, so inviting
  someone needs no redeploy: `go run ./demo-catalog uploaders add <steamid>
  -note "who"` from `packages/`, and `uploaders remove` to stop them. Until
  November 2026 it was an `UPLOADERS` variable here; the API now ignores it
  and logs a warning while it is still set.

  Without `STEAM_API_KEY` sign-in still works, but OpenID returns only the
  steamid, so that is the name everyone is shown. With it, each sign-in reads
  the player's current Steam name and avatar; a failed lookup keeps whatever
  was stored and never blocks the sign-in. The key goes in a query string, so
  the API rebuilds every lookup error without the URL - keep it that way.

  `PUBLIC_URL` ends in `/api` on purpose: it is what the API hands Steam as the
  OpenID realm and return URL, and Steam sends the browser there — so it has to
  be the address the browser uses, which is the Worker's, not Northflank's.

Note the project and service IDs from the URL; they become
`NORTHFLANK_PROJECT_ID` and `NORTHFLANK_SERVICE_ID`.

### 4. Cloudflare

```sh
npx wrangler r2 bucket create cs2-demo-payloads
```

Then edit `packages/web/wrangler.jsonc` and replace `API_ORIGIN` with the real
Northflank service URL (both occurrences — the top-level one and the `preview`
env).

An API token with **Workers Scripts: Edit** and **Workers R2 Storage: Edit**,
from My Profile → API Tokens, becomes `CLOUDFLARE_API_TOKEN`. The account ID is
on the dashboard sidebar.

Browsers upload demos straight to the bucket with a presigned URL, so the
bucket needs CORS for the site's origin, and a rule that clears uploads
nobody finished:

```sh
cd packages/web
npx wrangler r2 bucket cors set cs2-demo-payloads --file r2-cors.json
npx wrangler r2 bucket lifecycle add cs2-demo-payloads abandoned-uploads incoming/ --expire-days 7
```

`r2-cors.json` allows `PUT` from the deployed origin and from
`localhost:5173`, and nothing else; a new domain goes in that file. (Set
2026-11-26.)

### 5. Repository configuration

**Secrets** (Settings → Secrets and variables → Actions → Secrets):

| Secret | Used by |
|---|---|
| `DATABASE_URL_MIGRATIONS` | production migrations: the session pooler URL with `?search_path=public&sslmode=require` |
| `NORTHFLANK_API_KEY` | API rollout |
| `CLOUDFLARE_API_TOKEN` | Worker publish |
| `CLOUDFLARE_ACCOUNT_ID` | Worker publish |

**Variables** (same page → Variables). Not secrets — they are identifiers, and
keeping them out of the secret store means you can read them in a log:

| Variable | Example |
|---|---|
| `NORTHFLANK_PROJECT_ID` | `cs2-demo-analyzer` |
| `NORTHFLANK_SERVICE_ID` | `api` |
| `API_HEALTH_URL` | `https://<your-domain>/api/health` (optional) |

**Environment** (Settings → Environments): create one named `production`. The
migrate and deploy jobs reference it, so adding a required reviewer there turns
every schema change into something a human approves. Without it they run
unattended, which is a valid choice — just make it on purpose.

## The workflows

| File | Trigger | Does |
|---|---|---|
| `ci.yml` | every push and PR | Go build/vet/test/gofmt, web test + build, Docker build without pushing |
| `deploy-api.yml` | main, on API/collector/Dockerfile paths | test → image → GHCR → migrate Supabase → Northflank → poll `/health` |
| `deploy-web.yml` | main, on web/radar paths | test → build → `wrangler deploy` |
| `migrations.yml` | PRs touching migrations | apply every migration to an empty Postgres 17 on the runner, validate `atlas.sum` |

Both deploy workflows also accept `workflow_dispatch`, which is how you roll
back: re-run the deploy from the last good commit.

### Order of operations on an API deploy

Migrations run **before** the new image rolls out, so the code that needs a
column never starts before the column exists. That holds as long as migrations
are additive: during a rollout the old revision is still serving and must still
be able to read the database.

A destructive change breaks that and has to be split across two deploys:

1. add the new column, backfill it, deploy code that writes both;
2. once nothing reads the old one, drop it.

## Publishing demo data

```sh
cd packages
go run ./demo-export -visibility private -owner <steamid> -upload ../data/nukepug.dem
go run ./demo-catalog list                        # what is published, and to whom
go run ./demo-catalog put nukepug                 # publish web/public/data/nukepug/
go run ./demo-catalog set nukepug -visibility public -sample true
go run ./demo-catalog unlist x                    # take a demo off the site
go run ./demo-catalog publish -dry-run            # what the index would change
```

`npm run upload-data [names]` and `--unlist` still work, as a wrapper over
`demo-catalog`.

**The database is the catalogue** (since November 2026, `docs/uploads.md`).
Every demo the site shows is a `demos` row: its declaration (who sees it,
what it was) and a `summary` measured from its payload. `data/index.json` is
generated from those rows and written to R2 by Go after every change, never
edited on its own. Publishing needs `DATABASE_URL` and an R2 API token, read
from `packages/api/.env` (or the environment):

| Variable | What |
|---|---|
| `R2_ACCOUNT_ID` | the Cloudflare account id; the S3 endpoint is `<id>.r2.cloudflarestorage.com` |
| `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | an **Account** API token (R2 → Manage API tokens), Object Read & Write, on this bucket only |
| `R2_BUCKET` | `cs2-demo-payloads`, the bucket `wrangler.jsonc` binds |

**Any machine with those can publish**, because no machine's disk is the list.
Publishing writes the payloads first, then the row, then the index, so a demo
is never listed before its files are there. The index is regenerated under a
Postgres advisory lock, so two publishers cannot interleave into an index
missing one of them, and it is **refused if it would drop a demo the published
index lists** that is not being unlisted - an empty or wrong database cannot
hide the site's demos. An unlisted demo is no longer served (the Worker serves
only what the index lists), though its files stay in the bucket.

A demo's declaration comes from its `info.json` on first publish. After that
the row is the truth: a re-publish applies only what `info.json` declares and
keeps the rest, so publishing from a machine whose `info.json` says less does
not make a public demo private. Change a published demo with
`demo-catalog set`; `npm run set-info` edits the local `info.json`, which is
what local dev and the next publish read. Invalid declarations - a typo in a
visibility, a private sample - are refused by `demo-catalog` and again by
constraints on the table.

Keys mirror local paths: `public/data/nukepug/movement.json` becomes
`data/nukepug/movement.json` in the bucket, which is the same URL the app
fetches in dev off Vite's static server. Nothing in the app knows the
difference.

### Who sees a demo

Each demo's `info.json` declares `visibility` (`public`, `link`, `private` —
the same words as the `demo_visibility` enum) and `owner` (a steamid), set with
`demo-export -visibility ... -owner ...`, and is stored on the demo's row.
Once published, `demo-catalog set` changes it. The generated `index.json`
carries both, and the Worker
(`worker/access.ts`) uses that one file to decide, per request:

- a visitor who is not signed in sees the samples (`"sample": true`, which
  must also be public) and nothing else, so `public` means every signed-in
  account - the Pros section is for accounts, for now, and `signedOutMay` is
  the one function to change when that loosens;
- `/data/index.json` is answered per viewer, listing only what they may see;
- a file inside a demo's folder is served only if the viewer may open it, and
  otherwise answers 404 exactly like a demo that does not exist;
- a demo with no declared visibility is private, a private demo with no owner
  opens for nobody, and a demo missing from the index is not served at all.

The Worker knows who is asking by verifying the API's `sid` cookie itself, so
it needs the API's key as a secret — **the same value** Northflank has:

```sh
npx wrangler secret put SESSION_KEY
```

Without it every visitor reads as signed out and only the samples are
served. Rotating `SESSION_KEY` therefore means setting it in both places.

When changing access, upload before deploying a Worker that depends on the new
fields: an index without them reads as all-private, which hides demos rather
than leaking them, but hides them from you too.

## Why the proxy

The API sets its session cookie `SameSite=Lax`. Steam login finishes with a
cross-site redirect from `steamcommunity.com` back to us, and on a Lax cookie a
browser sends that only when the request lands on the same site that set it.

Point the browser straight at the Northflank hostname and the cookie is
third-party: login appears to work, the redirect completes, and the session is
silently missing — with nothing in any log to explain it. Behind the Worker the
cookie is first-party and the problem cannot occur. CORS stops mattering for
the same reason.

The cost is one hop of latency on API calls, which for an endpoint that answers
logins is not a real cost.

## When it breaks

**`required flag(s) "dev-url" not set`** — Atlas is resolving `atlas.hcl`'s
`db` env, which declares a dev database. The workflows pass explicit
`--dir`/`--url`/`--revisions-schema` flags instead of `--env db` precisely to
avoid this. If you add an Atlas step, do the same.

**`connected database is not clean`** — the connection string is missing
`?search_path=public`, so Atlas is looking at the whole database and finding
Supabase's own schemas (`auth`, `storage`, ...). Add it rather than reaching
for `--allow-dirty`.

**`prepared statement "stmtcache_..." already exists`** — `DATABASE_URL` is
the transaction pooler (port 6543). Use the session pooler, port 5432.

**`/api/health` reports the database unreachable after a quiet week** — the
free plan has paused the project. Restore it from the Supabase dashboard; it
takes about a minute.

**Login redirects but `/api/me` says unauthenticated** — `PUBLIC_URL` is
probably the Northflank hostname rather than `https://<your-domain>/api`. Steam
checks `return_to` against the realm, and the cookie has to be set on the host
the browser is actually using.

**502 from `/api/*` right after a deploy** — Northflank scales to zero; the
first request pays a cold start. The Worker returns a JSON `api unreachable`
body rather than a blank error so this is distinguishable from a crash.

**Worker deploys but serves a stale bundle** — `wrangler deploy` uploads
`dist/`, and `npm run build` must have run in the same job. `deploy-web.yml`
does that; a manual deploy from a dirty tree may not have.

## Not covered yet

- **The upload worker.** Uploads reach the `jobs` queue (phase C of
  `docs/uploads.md`); `demo-worker`, which processes them, is phase D.
- **`packages/fly.toml`** is the previous host's config and is now dead weight.
  Left in place rather than deleted as part of a CI change.
