# Deployment

Repo to production: GitHub Actions builds and gates, Neon holds the database,
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
   atlas migrate ──► Neon           ▼
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
| **Demo payloads** | **no** | pushed to R2 by hand: `npm run upload-data` |

Demo payloads are the deliberate exception. They are built from `.dem` files
that never enter the repo, so CI cannot produce them and has nothing to verify
them against. Publishing data stays something a person does on purpose from the
machine that has the demos.

## One-time setup

### 1. Neon

Two things CI needs, both from an existing project:

- **`NEON_PROJECT_ID`** — Project settings → General.
- **`NEON_API_KEY`** — Account settings → API keys.

Also grab the **direct** connection string for the production branch — the
endpoint *without* `-pooler`. Migrations need real session state that a
connection pooler does not provide, and this is the single most common way to
get a confusing failure here.

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
  | `DATABASE_URL` | Neon **pooled** URL (the app pools; migrations do not) |
  | `SESSION_KEY` | `openssl rand -hex 32` — rotating it logs everyone out |
  | `PUBLIC_URL` | `https://<your-domain>/api` |
  | `WEB_ORIGIN` | `https://<your-domain>` |
  | `PORT` | `8080` |

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

### 5. Repository configuration

**Secrets** (Settings → Secrets and variables → Actions → Secrets):

| Secret | Used by |
|---|---|
| `NEON_API_KEY` | PR branches |
| `NEON_DATABASE_URL_DIRECT` | production migrations |
| `NORTHFLANK_API_KEY` | API rollout |
| `CLOUDFLARE_API_TOKEN` | Worker publish |
| `CLOUDFLARE_ACCOUNT_ID` | Worker publish |

**Variables** (same page → Variables). Not secrets — they are identifiers, and
keeping them out of the secret store means you can read them in a log:

| Variable | Example |
|---|---|
| `NEON_PROJECT_ID` | `dawn-rain-12345678` |
| `NEON_PRODUCTION_BRANCH` | `main` (optional; defaults to `main`) |
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
| `deploy-api.yml` | main, on API/collector/Dockerfile paths | test → image → GHCR → migrate Neon → Northflank → poll `/health` |
| `deploy-web.yml` | main, on web/radar paths | test → build → `wrangler deploy` |
| `neon-pr-branch.yml` | PRs touching migrations | branch prod's database, apply migrations to it, validate, comment; delete on close |

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
cd packages/web
python ../../playground/python/src/demo_output_processor/export_movement.py --demo nukepug
npm run upload-data -- --dry-run     # see what would go
npm run upload-data nukepug          # or omit the name for everything
```

Keys mirror local paths: `public/data/nukepug/movement.json` becomes
`data/nukepug/movement.json` in the bucket, which is the same URL the app
fetches in dev off Vite's static server. Nothing in the app knows the
difference.

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
`neon` env, which declares a dev database. The workflows pass explicit
`--dir`/`--url`/`--revisions-schema` flags instead of `--env neon` precisely to
avoid this. If you add an Atlas step, do the same.

**`connected database is not clean: found schema neon_auth`** — Neon puts a
`neon_auth` schema in every database. `--allow-dirty` is correct on a fresh
branch (the PR workflow passes it) and correct on a first production apply. It
is not correct as a permanent fixture on production: once the revision table
exists you want to know if something unexpected appears.

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

- **The parse pipeline.** `round-collector` and `export_movement.py` still run
  locally. Uploading a `.dem` through the API and parsing it server-side is the
  feature the `jobs` table in `schema/schema.sql` is waiting for; when it
  lands, the worker shares this image and this deploy.
- **`packages/fly.toml`** is the previous host's config and is now dead weight.
  Left in place rather than deleted as part of a CI change.
