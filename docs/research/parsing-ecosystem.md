# CS2 Demo Parsing Ecosystem

A living reference of what's available for parsing CS2 `.dem` files, organized by language. Use this to evaluate trade-offs when picking a stack for any phase of the project.

> **Verify before depending.** This document was written from Claude's knowledge (early 2026). The space moves fast — confirm activity, license, and CS2-format support on the upstream repo before committing to a library.

## At a glance

| Library | Language | CS2 support | Tick-level data | Event API | Best for |
|---|---|---|---|---|---|
| `demoparser2` | Rust (with Python/Node bindings) | ✅ first-class | ✅ vectorized, fast | ✅ | Bulk analytics, pandas/Polars workflows, mechanical-skill features |
| `awpy` | Python (wraps demoparser2) | ✅ | ✅ via parser | ✅ + higher-level helpers | Notebook research, "batteries-included" analytics |
| `demoinfocs-golang` | Go | ✅ | ✅ | ✅ (callback/event-driven) | Long-running services, real-time pipelines, single-binary deployment |
| `demofile-net` (Saul Rennison) | TypeScript/Node | ✅ | ✅ | ✅ | Full-stack JS/TS apps, browser-adjacent tooling |
| `DemoFile.Net` | C# / .NET | ✅ | ✅ | ✅ | .NET shops, Windows-friendly tooling |
| Valve `demoinfo2` (Source 2 SDK fragments) | C++ | partial / DIY | ✅ | minimal | Last-resort reference, low-level research |
| HLAE / Source2Viewer / VRF | C# / C++ | n/a (asset / replay tooling) | n/a | n/a | Map/asset extraction, frame-accurate replay capture — not gameplay events |

---

## Concepts that apply across all libraries

CS2 demos are **Source 2 format**, different from CS:GO's older format. Libraries that only ever supported CS:GO need explicit CS2 work; not all have caught up.

What a parser exposes, conceptually, in roughly increasing depth:

1. **Match metadata** — map, server, players, FACEIT match ID (when embedded).
2. **Game events** — high-level `player_death`, `bomb_planted`, `round_end`, `weapon_fire`, etc. Easy to consume, sometimes lossy.
3. **Entity / tick-level state** — every player's position, velocity, view angles, weapon state, health, money on every server tick (~64 Hz on FACEIT). This is what you need for mechanical-skill metrics.
4. **User commands** — the raw input the player's client sent: buttons (W/A/S/D, attack, jump), aim deltas, etc. CS2 demos include these for the recording player and limited info for others. This is what you need to verify counterstrafe / movement timing.
5. **Net packets / string tables** — under-the-hood protocol. Almost no analyzer needs this directly; the parser handles it.

A well-built analyzer rarely touches level 5 and only sometimes touches level 1 directly — most useful work lives at levels 2–4.

---

## Python ecosystem

### `demoparser2` (LaihoE)

- **What it is:** Rust core with first-class Python bindings (`pip install demoparser2`). Vectorized — parses a whole demo and hands you Arrow/pandas tables of ticks, events, and headers.
- **Strengths:** Speed (10–50× faster than naive Python parsers). Clean DataFrame output ideal for analytics. Active maintenance. Good CS2 support including user_cmd-derived button states for tick data.
- **Limits:** Less of an event-driven callback API and more of a "parse → DataFrame" model. If you want streaming/online processing, demoinfocs-golang fits better.
- **Where it shines for this project:** Phase 1 Python parser. Bulk-extract tick data for mechanical-skill metrics; load into SQLite/Postgres for queries.

### `awpy` (pnxenopoulos)

- **What it is:** Python library that *wraps* `demoparser2` and adds higher-level analytics: round summaries, kill/damage tables, grenade trajectories, nav-mesh utilities, mapping helpers.
- **Strengths:** "Notebook-friendly" — one import gives you a parser plus most stats you'd want. Used in academic research papers.
- **Limits:** Some helpers are CS:GO-era and may not all work cleanly on CS2 yet — check per-feature. You're somewhat at the mercy of awpy's data shapes.
- **Where it shines for this project:** Quick exploration and prototyping. If a metric awpy already computes is "close enough," save the work. For *transparent* counterstrafe-style metrics, you'll likely drop down to demoparser2 directly.

### `pandas` / `polars`

- Not parsers — but worth naming because once data is in DataFrames, this is where the actual *analysis* lives in a Python workflow. Polars is faster, lazier, and increasingly idiomatic; pandas is what every tutorial uses.

### Python tradeoffs overall

- **Pros:** Fastest path to working code given the rich ecosystem. Best for ML / agentic AI integrations later. SQLAlchemy or raw `psycopg`/`duckdb` give you strong DB skills.
- **Cons:** Single-binary deployment is awkward. Performance is good *only because* the hot path is Rust under the hood.

---

## Go ecosystem

### `demoinfocs-golang` (markus-wa)

- **What it is:** The mature, callback/event-driven Go parser. You register handlers (`parser.RegisterEventHandler(func(e events.Kill) { ... })`) and the parser streams the demo through them.
- **Strengths:** Stable, well-documented, broad event coverage. CS2 support is solid. Streaming model — low memory even for huge demos. Compiles to a single binary, trivial to deploy as a microservice or CLI.
- **Limits:** No batteries-included analytics layer — you write the aggregation yourself. Bulk-export to columnar formats (Parquet) is DIY.
- **Where it shines for this project:** Phase 2 parser. Replace the Python parser with a Go binary that produces Parquet/JSON. Teaches service boundaries, IPC, and Go itself.

### Go tradeoffs overall

- **Pros:** Performance, deployment simplicity, strong typing, excellent stdlib, real concurrency primitives. The "professional service" language for this domain.
- **Cons:** Fewer libraries for visualization, ML, or AI integration — you'd typically hand off to Python for those.

---

## TypeScript / Node ecosystem

### `demofile-net` (Saul Rennison)

- **What it is:** Actively maintained TypeScript parser with CS2 support. Event-driven API similar in shape to demoinfocs.
- **Strengths:** Lets you do everything in one language end-to-end (Node backend + Next.js frontend). Good if you want isomorphic types between parser and UI.
- **Limits:** Smaller community than demoinfocs/demoparser2. ML/AI tooling is weaker in JS land (though improving rapidly).
- **Where it shines for this project:** If you ever want a *single-language* full-stack version, or if you want to run a lightweight parser in a Cloudflare Worker / Vercel function.

### TypeScript tradeoffs overall

- **Pros:** Single language across stack. Strong frontend story (Next.js, React). Modern type system.
- **Cons:** Slower than Go/Rust for parsing. ML/agent ecosystem less mature than Python.

---

## Rust ecosystem

### `demoparser2` (native)

- Same library as the Python binding above, used directly in Rust. Use this if you want maximum speed and no Python in the loop.

### Rust tradeoffs overall

- **Pros:** Steepest engineering rigor payoff. Memory-safety + speed.
- **Cons:** Steepest learning curve. Probably overkill unless Rust itself is the learning goal.

---

## C# / .NET ecosystem

### `DemoFile.Net`

- **What it is:** C# port maintained for CS2.
- **Where it shines:** Windows-native tooling, Unity-adjacent work, or if you already use .NET professionally.

---

## C++ / native

### Valve `demoinfo2` and Source 2 SDK code

- Pieces are public via the SDK and various community forks. Not a friendly API — useful only as a reference when other parsers disagree on something obscure.

### `Source2Viewer` / VRF (ValveResourceFormat)

- For *asset* extraction (maps, models, materials), not gameplay events. Mention it because if you ever want to render an actual map overlay for heatmaps, this is how you get the map geometry.

---

## Adjacent tools (not libraries, but useful to know)

- **CS Demo Manager** (akiver, formerly CSGO Demos Manager) — desktop GUI with built-in analytics. Useful as a *sanity-check oracle*: parse the same demo, compare your computed metrics to its UI, find your bugs.
- **Leetify, Scope.gg, CS Stats** — hosted analytics. Useful as inspiration and as the "black-box baseline" to compare your transparent metrics against.
- **HLAE** (Half-Life Advanced Effects) — frame-accurate recording / cinematic tooling. Not relevant for stats, but the community overlaps.

---

## How this maps to the project's polyglot phase plan

| Phase | Language(s) | Library |
|---|---|---|
| 1 — local Python end-to-end | Python | `demoparser2` directly (skip awpy initially so you understand the raw data) |
| 2 — replace parser with Go binary | Go | `demoinfocs-golang` producing Parquet/JSON |
| 3 — TypeScript web UI | TypeScript (frontend) + existing Python API | `demofile-net` only if a JS-side parser is needed (likely not) |
| 4 — agentic AI layer | Python | LLM speaks to the same Postgres via tool/function calls |
| 5 — ML, multi-user, scale | Python (ML), revisit Go for hot paths | |

---

## Open questions / things to verify before committing

- Latest CS2 demo format changes (Valve has shipped breaking updates a few times — confirm whichever parser you pick handles your specific FACEIT demos).
- Whether FACEIT demos differ from MM demos in any meaningful way (FACEIT often serves at 128 tick instead of 64; verify what your demos actually are).
- Specific user_cmd / button-state availability — this is critical for transparent counterstrafe and movement-timing metrics. Verify each parser actually exposes this for the recording player on a real demo before betting on it.
