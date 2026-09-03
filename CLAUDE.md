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
   │  round-collector (Go)
   ▼
out/foo/                 18 MB   9 CSV files: every player, every tick
   │  export_movement.py (Python)
   ▼
public/data/foo/         1.2 MB  movement.json, resampled to 4 samples/second
   │  fetched by the browser
   ▼
packages/web                     the page that draws it
```

**The one rule: Go writes files, and nothing calls back.** The parser does not
know the website exists. The website never opens a `.dem`. Every arrow above is
a file being written and later read by something else. That is what lets you
change the viewer without touching the parser, and query the same data from a
notebook.

## Layout

| Path | What lives there |
|---|---|
| `packages/collector/` | Go. Reads a `.dem`, writes CSVs. The parsing core. |
| `packages/round-collector/` | Go. Command-line wrapper around the collector. |
| `packages/api/` | Go. Small HTTP server: Steam login. Talks to Postgres. |
| `packages/web/` | TypeScript + React. The viewer and the home page. |
| `playground/python/…/demo_output_processor/` | Python. Turns CSVs into `movement.json`. |
| `scripts/` | `dev.sh` — runs the whole site locally. |
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

# --- parse a demo into CSVs
cd packages
go run ./round-collector -demo ../data/foo.dem -out ../out

# --- turn those CSVs into what the web app reads
cd playground/python/src/demo_output_processor
./venv/Scripts/python.exe export_movement.py --demo foo     # Windows
python export_movement.py --demo foo                        # elsewhere

# --- web
cd packages/web
npm test                         # vitest
npm run typecheck                # app + Cloudflare Worker
npm run build                    # typecheck + bundle
npm run upload-data              # publish demo payloads to R2 (needs wrangler login)

# --- database migrations (see packages/api/README.md first)
cd packages/api
./migrate.sh migrate diff <name>
./migrate.sh migrate apply
```

## How the pieces fit together in production

Six services. The browser only ever talks to the first one.

- **Cloudflare Worker** — serves the website, and routes everything else:
  `/api/*` goes to the Go server, `/data/*` comes from file storage, the rest
  is the app itself.
- **Cloudflare R2** — file storage holding the finished `movement.json`
  payloads. Uploaded by hand, never by CI.
- **Northflank** — runs the Go API in a container, around the clock.
- **Neon** — Postgres. Currently just a row per person who signed in.
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

**Changing the payload means re-exporting and re-uploading.** The web app and
the demo data deploy separately. If you add a field to `movement.json`, the
deployed site gets the new code immediately but keeps the old data until you
run `npm run upload-data`. Make new fields optional in `types.ts` so an old
payload degrades instead of crashing.

**Don't `source` the `.env` file in a shell script.** Neon connection strings
contain `&`, which the shell reads as "run this in the background" — the
variable silently ends up empty. Parse the file line by line instead;
`scripts/dev.sh` shows how.

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
- **Schema changes are additive.** `major.minor`; appending a column bumps the
  minor and old readers keep working. If a change alters the *meaning* of an
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
