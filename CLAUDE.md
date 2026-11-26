# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository. Written
so someone seeing the project for the first time can be useful in it.

## What this is

A tool for watching Counter-Strike 2 matches back on a top-down map. You give
it a `.dem` file — the replay CS2 records — and it turns that into a web page
where you can scrub through the match and see where everyone was, what they
were holding, where they shot, and where they died.

The whole project is one idea repeated: **a big unreadable file becomes a
smaller readable one, in stages, and each stage writes its result to disk.**

```
data/foo.dem            670 MB   the recording CS2 made
   │  demo-export (Go) ──── -csv ───►  out/foo/   19 MB  11 CSVs + manifest.json
   ▼                                   (round-collector writes the same folder)
public/data/foo/         1.2 MB  movement.json + analytics.json, 4 samples/second
   │  fetched by the browser
   ▼
packages/web                     the page that draws it
```

`demo-export` parses once and builds the payload as the rounds stream past, in
about ten seconds a match. The CSV folder is the stage for notebooks and for
re-exporting without re-parsing; until November 2026 it was the only route,
with Python building the payload from it. The Go builder (`packages/payload`)
replaced that only after matching the Python's output exactly on every demo to
hand — `TestMatchesPythonExport` keeps it that way.

Every CSV folder carries a `manifest.json` beside the CSVs holding the
schema version, a row count per table, and the full column list of each — read
it first when you are unsure what a run produced, rather than opening a 19 MB
CSV. Note that `ticks.csv` is written **gzipped**, so it is `zcat`, not `head`.

**The one rule: Go writes files, and nothing calls back.** The parser does not
know the website exists. The website never opens a `.dem`. Every arrow above is
a file being written and later read by something else. That is what lets you
change the viewer without touching the parser, and query the same data from a
notebook.

## Layout

| Path | What lives there |
|---|---|
| `packages/collector/` | Go. Reads a `.dem` and hands each round to a sink. The parsing core. `csvsink` writes the CSV folder; `csvsource` reads one back as the same rounds. |
| `packages/payload/` | Go. Builds `movement.json` from rounds, as a sink — from a `.dem` or a CSV folder alike. |
| `packages/demo-export/` | Go. A demo to the web app in one step: `movement.json`, `analytics.json`, `info.json`, optionally published. |
| `packages/round-collector/` | Go. Command-line wrapper around the collector that writes the CSV folder alone. |
| `packages/analytics/` | Go. Match statistics from one demo or many: weapon kills, opening kills, clutches, X-vs-Y conversion. |
| `packages/demo-analytics/` | Go. Command-line wrapper around analytics. |
| `packages/clip-list/` | Go. Picks moments worth recording (openings, multikills, clutches) and writes them as clip lists for the recording laptop. See `docs/clip-capture.md`. |
| `packages/clip-overlay/` | Go. Draws the player's keys and mouse buttons over recorded clips (ASS script, burnt in with FFmpeg). |
| `packages/api/` | Go. Small HTTP server: Steam login. Talks to Postgres. |
| `packages/web/` | TypeScript + React. The viewer and the home page. |
| `playground/python/…/demo_output_processor/` | Python. Notebook helpers over the CSVs (`demo_data.py`), and the original `movement.json` exporter — kept as the reference the Go builder is checked against, and for `--merge`. |
| `scripts/` | `dev.sh` — runs the whole site locally. `clips/record-clips.ps1` records clips on the Windows laptop. |
| `docs/` | Output format and deployment, both worth reading before changing either. |
| `assets/radar/` | Map images. Committed; copied into the web build. |

**The Go module root is `packages/`, not the repo root.** Every `go` command
runs from there. This trips people up constantly.

## Commands

```sh
# --- the whole site on localhost (api on :8080, web on :5173)
./scripts/dev.sh                 # needs Git Bash on Windows, not PowerShell
./scripts/dev.sh --web-only      # frontend only, no database needed

# --- Go
cd packages
go build ./...
go test ./...                    # the whole suite
go test ./collector/ -run TestName
go vet ./...
gofmt -l .                       # lists unformatted files; should print nothing

# --- a demo to the web app, in one step (about ten seconds a match)
cd packages
go run ./demo-export ../data/foo.dem             # -> web/public/data/foo/
go run ./demo-export -visibility private -owner <steamid> -date 2026-08-14 ../data/foo.dem
go run ./demo-export -kind pug -visibility public "C:/demos/1-604132bd.dem.zst"
go run ./demo-export ../out/foo                  # re-export a parsed folder, no re-parse
go run ./demo-export -csv ../out ../data/foo.dem # also write the CSV folder, same pass
go run ./demo-export -upload ../data/foo.dem     # ...and publish it (npm run upload-data)
# The info flags are export_movement.py's and set-info's, with the same rules,
# and a re-export keeps what an earlier one declared. Undeclared is private.

# --- the payload checked against the Python exporter (needs parsed demos)
PAYLOAD_GOLDEN=<folder of <demo>/movement.json from export_movement.py> \
  go test ./payload -run Python  # reads ../out; PAYLOAD_OUT overrides

# --- parse a demo into CSVs alone (a full match takes a few minutes)
cd packages
go run ./round-collector -demo ../data/foo.dem -out ../out
go run ./round-collector -demo ../data/foo.dem -out ../out -max-rounds 2
                                 # ^ two rounds, for when you are iterating

# --- match statistics, by player and by team (see docs/demo-analytics.md)
cd packages
go run ./demo-analytics ../out                  # every parsed demo, instantly
go run ./demo-analytics -team "us=name,name,steamid" ../out
                                 # ^ follow one team across demos; the list is
                                 #   a player pool, not a fixed five
go run ./demo-analytics ../data/a.dem ../data/b.dem   # or straight from .dem
go run ./demo-analytics "C:/team demos"     # or a folder of .dem / .dem.zst, left in place
go run ./demo-analytics -web web/public/data ../out
                                 # ^ also writes each demo's analytics.json for
                                 #   the web app's ?demo=<name>&view=analytics page

# --- the Python exporter: the reference, and the only one that merges demos
cd playground/python && uv sync          # one-time after clone
cd src/demo_output_processor
./venv/Scripts/python.exe export_movement.py --demo foo     # Windows
python export_movement.py --demo foo                        # elsewhere
# --hz changes the sample rate (default 4), --out the destination,
# --merge combines several demos into one payload.
# --kind pug|match (plus optional --event, --date, --title) writes info.json
# beside the payload: what the demo cannot say about itself. The Pros section
# filters on it. It is declared, never guessed; edit info.json to correct it.
# --visibility public|link|private and --owner <steamid> go in the same file
# and decide who the deployed site shows the demo to. UNDECLARED MEANS PRIVATE
# TO NOBODY: a pro demo exported without --visibility public will not appear.

# --- declare or backfill info.json without re-exporting
cd packages/web
npm run set-info -- nukepug --date 2026-08-14
npm run set-info -- <demo> --kind pug --visibility public --sample
                                 # ^ a sample is what signed-out visitors see;
                                 #   it must be public, and index-demos refuses
                                 #   to write an index otherwise
npm run set-info -- --dates-from ../../data [--dry-run] [--force]
                                 # ^ backfill dates from each .dem's modified
                                 #   time. Only for games YOUR client recorded:
                                 #   a downloaded FACEIT demo carries its
                                 #   download day, not the day it was played

# --- the Pros section
# A pro is an entry in packages/web/src/pros.json: a slug, a name, and EVERY
# steamid they play on (pros pug on alternates). Which demos a pro is in is
# worked out by index-demos from each roster - never tagged by hand - so a pro
# match lands on every catalogued player's page from one export. Publish one
# payload per demo: the starters view combines a set in the browser, so a
# merged export is not needed for any filter.

# --- web
cd packages/web
npm test                         # vitest
npm run typecheck                # app + Cloudflare Worker
npm run build                    # typecheck + bundle
npm run upload-data              # publish demo payloads to R2 (needs wrangler login)
npm run deploy                   # build + wrangler deploy, if you ever need to
                                 # bypass CI; normally main does this for you

# `dev` and `build` both run `radar` and `index-demos` first. Those regenerate
# the map images and the demo list from what is on disk, so a demo you just
# exported shows up on its own — you do not run them by hand.

# --- database migrations (see packages/api/README.md first)
cd packages/api
cp .env.example .env             # then fill in; see the note below on which URL
./migrate.sh migrate diff <name>
./migrate.sh migrate apply
./migrate.ps1 migrate apply      # same thing from PowerShell
```

## How the pieces fit together in production

Six services. The browser only ever talks to the first one.

- **Cloudflare Worker** — serves the website, and routes everything else:
  `/api/*` goes to the Go server, `/data/*` comes from file storage, the rest
  is the app itself.
- **Cloudflare R2** — file storage holding the finished `movement.json`
  payloads. Uploaded by hand, never by CI.
- **Northflank** — runs the Go API in a container, around the clock.
- **Supabase** — Postgres, and only Postgres: the API connects to it directly
  and none of Supabase's client libraries or its Data API are used. Currently
  just a row per person who signed in. (Moved from Neon in November 2026 over
  free-tier usage; the free plan pauses a project after a week with no
  activity, and the dashboard wakes it.)
- **GitHub Actions** — runs the tests on every push, and deploys on `main`.
- **Steam** — handles login. We never see a password.

The Worker sitting in front is not decoration. The API sets its session cookie
`SameSite=Lax`, and Steam login finishes with a redirect from
`steamcommunity.com` back to us. A browser only sends a Lax cookie to the site
that set it, so if the browser talked to Northflank directly, login would
appear to work and the session would silently not stick. One origin makes that
impossible. `docs/deployment.md` has the full picture.

## Things that will bite you

**Filter tick rows before averaging anything.** Dead players keep emitting rows
until the round ends, frozen at their last living values — a corpse reads as
holding W for thirty seconds. Use `demo_data.load_ticks()`, which applies the
filters for you. `docs/round-collector-schema.md` lists all five traps.

**The struct is the schema.** Each CSV table is one Go struct with its
`Columns()` and `AppendRow()` beside it, plus a test that they stay in the same
order. Add a column at the **end**, never in the middle. A shifted column does
not crash anything — it just silently puts the wrong data under every heading
after it.

**Demo access is enforced by the Worker, and only in production.** Each
demo's `info.json` declares `visibility` and `owner`; `index-demos` copies them
into `index.json`, and `worker/access.ts` filters the index and refuses
payloads per viewer, reading the API's `sid` cookie itself with the same
`SESSION_KEY` (a Worker secret). **Signed out, a visitor sees the samples and
nothing else**: `public` means every signed-in account, and the Pros pages ask
for a sign-in (`signedOutMay` in `access.ts` is the one place that decides).
Local dev serves `public/data` straight off disk and shows everything; only the
page-level gates apply there. The cookie format now lives in two languages,
pinned by one shared test vector in `session_test.go` and `session.test.ts`.

**A kill is an enemy kill.** Since November 2026 a suicide or teamkill is the
victim's death but nobody's kill or assist, and damage to a teammate is not
ADR - in the exporter's `stats` (`player_stats`), the viewer's live board
(`stats.ts`) and the index (`demo-info.mjs`) alike. Before that, `stats.k`
credited them: m0NESY's 31-kill pug read 32 from a post-round bomb death. This
CHANGED THE MEANING of `payload.stats`; a payload exported earlier still holds
the old count until it is re-exported.

**The payload has two builders, and only one is live.** `packages/payload`
(Go) is what `demo-export` and the coming upload worker run;
`movement_payload.py` is frozen as the reference it was checked against. A
change to the payload goes in Go. Change the Python too only to keep
`TestMatchesPythonExport` meaningful, and re-run it when you do.

**Changing the payload means re-exporting and re-uploading.** The web app and
the demo data deploy separately. If you add a field to `movement.json`, the
deployed site gets the new code immediately but keeps the old data until you
run `npm run upload-data`. Make new fields optional in `types.ts` so an old
payload degrades instead of crashing.

**Supabase gives you three connection strings; use the session pooler.**
Port 5432 on `*.pooler.supabase.com`, for the API and for migrations alike. The
direct host is IPv6-only on the free plan, which GitHub's runners cannot reach,
and the transaction pooler (6543) breaks both pgx's prepared statements and the
session Atlas needs. Strings for Atlas end in `?search_path=public` so it never
sees Supabase's own schemas. `DATABASE_DEV_URL` is `docker://postgres/17/dev`,
a throwaway container per diff — never point it at the real database.
`packages/api/.env.example` has the exact shapes.

**Don't `source` the `.env` file in a shell script.** Connection strings
contain `&` (`?search_path=public&sslmode=require`), which the shell reads as
"run this in the background" — the variable silently ends up empty. Parse the
file line by line instead; `scripts/dev.sh` and `migrate.sh` show how.

**Routing is by query parameter, and has to stay that way.** `vite.config.ts`
sets `base: "./"`, so every asset URL is relative. A page served at
`/demo/anubispug` would ask for `/demo/assets/index.js` and get a 404, so the
path stays at the root and the view is chosen by `?demo=`. Adding a path route
breaks asset loading, not routing, which makes it a confusing failure. The
reasoning is written at the top of `route.ts`.

**A local `npm run build` will ship data that CI does not.** `dist/data` picks
up whatever demos are in `public/data` on your machine — megabytes of payloads
that are supposed to come from R2 instead. `prune-data` runs at the end of
`build` to delete it. If you restructure the build, keep that step.

**The parser library reports entity lifetime, not motion.** Three separate
features hit this. Events fire when a thing is created or destroyed, so if you
need to know where something *moved*, poll its position each frame. See
`pollInfernos`, `pollBomb`, and `flightPath` in the collector — all three exist
for that reason.

**Sanity-check derived numbers against the real world.** Two different
plausible ways of computing average damage per round both produced wrong
answers that looked completely reasonable in code (103.6 and 110.9, where a
strong player is about 80). Neither was caught by a test. If you compute a
statistic, compare it to a number you already know.

## Conventions

- **Tests go on pure functions.** The canvas renderer and the parser event
  handlers are hard to test and mostly plumbing; the logic they call —
  `track.ts`, `effects.ts`, `stats.ts`, `timeline.ts`, the Go assembler — is
  where the bugs live and where the tests are. Do not add a mocking framework
  to reach the rest.
- **Comments explain why, not what.** The code says what it does. Comments
  exist for the decision behind it, especially where the obvious approach was
  tried and failed. Most comments here name a specific failure.
- **Schema changes are additive.** Currently `1.6`, recorded in every
  `manifest.json`. Appending a column bumps the minor and old readers keep
  working; changing what an existing column means is a major bump. If a change alters the *meaning* of an
  existing value rather than adding a new one, say so loudly in the docs.
- **Say what is measured and what is inferred.** `kits.csv` is reconstructed,
  not observed, and is documented as such. In six months an inference is
  indistinguishable from a measurement unless someone wrote it down.
- Do not commit demo files, parser output, or `movement.json`. All gitignored;
  `out/` alone is ~100 MB.

## What is not built yet

Uploading demos through the website. The `demos` and `jobs` tables exist in the
schema for it, and Steam login works, but there is no upload endpoint and no
button. Parsing is deliberately something a person runs on their own machine —
a finished payload is 1 MB, but the recording it came from is up to 670 MB, and
turning one into the other costs real computing time.
