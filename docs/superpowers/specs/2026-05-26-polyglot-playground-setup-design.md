# Polyglot Playground Setup — Design

**Date:** 2026-05-26
**Status:** Approved (pending user review of this written spec)
**Scope:** Per-language playground for CS2 `.dem` exploration in Python, Go, and Rust.

## Goal

Lower the activation energy to experiment with any of three demo-parsing ecosystems
(Python via `demoparser2`, Go via `demoinfocs-golang`, Rust via the `demoparser2`
crate) against the user's local `.dem` files. This is exploratory scaffolding, not
a production system — it should be deletable or absorbed into `services/*` later
without regret.

## Non-goals

- Not a production architecture. Naming and structure are deliberately marked as
  exploratory so production code can grow next to the playground without rename
  pressure.
- Not building the analyzer. No mechanical-skill metrics, no aggregation, no DB,
  no UI. Only "open a demo, prove the parser works."
- Not building tests, CI, pre-commit hooks, or shared schemas. Add when they
  earn their place.

## Architecture

### Repo layout

```
cs2-demo-analyzer/
  playground/
    python/
      pyproject.toml          # uv-managed
      uv.lock
      src/hello_demo.py
      README.md
    go/
      go.mod
      go.sum
      cmd/hello-demo/main.go
      README.md
    rust/
      Cargo.toml
      Cargo.lock
      src/main.rs
      README.md
  data/                       # existing, gitignored
  docs/                       # existing
  .gitattributes              # new
  .editorconfig               # new
  .gitignore                  # extended
```

Sibling directories under `playground/` rather than at the repo root: the
`playground/` prefix is an explicit "this is exploratory" marker. Production
code will grow alongside as `services/parser/`, `services/api/`, etc., with
no rename collision.

Each language's directory is self-contained with its own dependency manifest
and entry point. No shared code between them; the cross-language comparison
happens at runtime by reading their stdout, not via shared modules.

### `hello_demo` behavior (all three languages)

Single CLI argument: path to a `.dem` file. No default — explicit paths only.

Output, in this order:

```
== Header ==
Map:          <map name>
Tick rate:    <int>
Duration:     <human-readable>
Players:
  - <name>  (<steamid64>)
  - ...

== Tick 0 (raw first tick) ==
  <player>  x=...  y=...  z=...
  ...

== First live-round tick ==
Tick: <N>
  <player>  x=...  y=...  z=...
  ...
```

The two-tick output is intentional: the "live round" detection logic differs
per parser, and seeing the same intent expressed three ways is part of the
learning value. Each README will call out where in code the live-round filter
lives.

### Parser library choices

| Language | Library | Style |
|---|---|---|
| Python | `demoparser2` (LaihoE) | DataFrame-oriented; `DemoParser(path).parse_header()`, `parse_ticks([...])` returning Polars frames |
| Go | `demoinfocs-golang` (markus-wa) | Event-driven; register handlers, stream through demo |
| Rust | `demoparser2` crate (subject to verification — see Open questions) | Same Rust core as the Python binding, used natively |

Polars (not pandas) for Python — it's what `demoparser2` returns and the
modern default.

## Toolchain installs

### Installs to perform on this Mac

- Verify `uv` is installed; install via `curl -LsSf https://astral.sh/uv/install.sh | sh` if missing
- Install `rustup` via `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
- Install Go via `brew install go`
- Add `ruff` to the Python project as a dev dependency (single-binary Python linter)

`rustfmt` and `clippy` come bundled with `rustup` by default. `gofmt` ships
with Go.

### `docs/dev-setup.md` updates

Fill in the existing "TBD" sections for Go and Rust, following the
three-platform pattern (macOS / Windows native / Windows WSL2) already used
for `uv`:

**Rust:**
- macOS / WSL2: `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
- Windows (native): download `rustup-init.exe` from [rustup.rs](https://rustup.rs)

**Go:**
- macOS: `brew install go`
- Windows (native): `winget install GoLang.Go`
- Windows (WSL2): follow the official [go.dev/dl](https://go.dev/dl) tarball
  install (Ubuntu's apt version often lags)

## Repo hygiene additions

These files are already specified in `docs/dev-setup.md` but don't yet exist
on disk. Land them in the same change so the playground starts on solid
footing.

### `.gitattributes`

Verbatim from `dev-setup.md` lines 121–136. Prevents Windows checkouts from
CRLF-corrupting source files and marks `.dem` / Parquet / SQLite as binary.

### `.editorconfig`

Verbatim from `dev-setup.md` lines 140–156. LF + UTF-8 + 4-space default,
with 2-space for JS/TS/JSON/YAML/MD and tabs for Makefiles.

### `.gitignore` extensions

Current content is just `data/`. Add:

```
# Python
__pycache__/
*.py[cod]
.venv/

# Go
playground/go/bin/

# Rust
playground/rust/target/

# Demo files (defense in depth)
*.dem

# OS / editor
.DS_Store
Thumbs.db
.idea/
```

## What we are explicitly NOT adding

- Tests, CI workflows, pre-commit hooks — premature for playground scope.
- Language-version pin files (`.python-version`, `rust-toolchain.toml`) — for
  a two-machine playground, "whatever modern version you have" is fine. The
  `go.mod` minimum-version line covers Go.
- A `schemas/` or `contracts/` directory — meaningful only when two services
  start exchanging data. They don't yet.
- A `Makefile` or `justfile` task runner — each playground README documents
  its own three-line invocation. One layer of indirection is enough.

## Verification

After scaffolding and installs:

1. Each `hello_demo` runs against `data/mega_ot_mirage.dem` without errors.
2. All three print `Map: de_mirage` (or whichever Mirage variant the demo is).
3. Player rosters match across all three outputs.
4. Disagreement is not a blocker — it's the first interesting finding to
   investigate.

## Open questions / deferred decisions

- Rust's place in the long-term plan. The phased plan in
  `docs/research/parsing-ecosystem.md` has Python (Phase 1) → Go (Phase 2) →
  TypeScript (Phase 3), with no Rust phase. The `playground/rust/` directory
  is either (a) a learning exercise that gets deleted later, or (b) the seed
  of a future Rust-native parser service that displaces Go. Decision deferred
  — both stay open while exploring.
- Whether to keep using the demo file path as a CLI arg or eventually move to
  a config file. CLI arg is fine for now; revisit when demo count grows.
- `demoparser2` Rust crate availability. The Python package is on PyPI, but
  whether the underlying crate is published on crates.io is unverified.
  Implementation must check; fallback options are a git dependency on
  LaihoE's repo, or pivoting to a different CS2 Rust parser if none works
  cleanly.
