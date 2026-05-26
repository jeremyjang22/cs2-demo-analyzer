# Polyglot Playground Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scaffold three sibling playground directories (Python, Go, Rust) that each open a CS2 `.dem` file and print header + first-tick + first-live-tick info, plus install the macOS toolchains and document Windows install paths.

**Architecture:** Three self-contained playground subdirectories under `playground/` — each with its own dependency manifest, no shared code. Cross-language comparison happens at runtime by reading stdout, not via shared modules. The `playground/` prefix marks this as exploratory; production `services/*` will grow next to it later.

**Tech Stack:** Python (`uv` + `demoparser2` + Polars), Go (`demoinfocs-golang`), Rust (`demoparser2` crate — pending availability check, otherwise git dep).

**Spec:** `docs/superpowers/specs/2026-05-26-polyglot-playground-setup-design.md`

**Test demo:** `data/mega_ot_mirage.dem` (Mirage overtime, ~700MB)

**About "tests":** The spec explicitly excludes unit tests for playground scope. Each task's "verification" is running the binary against the real demo and checking stdout. Treat that as the test.

---

## Task 1: Add repo hygiene files

**Files:**
- Create: `.gitattributes`
- Create: `.editorconfig`
- Modify: `.gitignore`

- [ ] **Step 1.1: Create `.gitattributes`**

Write to `/Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/.gitattributes`:

```gitattributes
* text=auto eol=lf

# Binary files: never touch
*.dem      binary
*.png      binary
*.jpg      binary
*.parquet  binary
*.sqlite   binary
*.db       binary

# Platform-specific files
*.bat      text eol=crlf
*.ps1      text eol=crlf
*.sh       text eol=lf
```

- [ ] **Step 1.2: Create `.editorconfig`**

Write to `/Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/.editorconfig`:

```editorconfig
root = true

[*]
charset                  = utf-8
end_of_line              = lf
insert_final_newline     = true
trim_trailing_whitespace = true
indent_style             = space
indent_size              = 4

[*.{js,jsx,ts,tsx,json,yml,yaml,md}]
indent_size = 2

[Makefile]
indent_style = tab
```

- [ ] **Step 1.3: Extend `.gitignore`**

The current `.gitignore` contains only `data/`. Replace its contents with:

```
# Data — never commit demo files or local databases
data/
*.dem
*.sqlite
*.db
*.parquet

# Python
__pycache__/
*.py[cod]
.venv/
.python-version

# Go
playground/go/bin/

# Rust
playground/rust/target/

# Editor / OS
.DS_Store
Thumbs.db
.idea/
.vscode/*
!.vscode/extensions.json
!.vscode/settings.json
```

- [ ] **Step 1.4: Verify git renormalize doesn't shred files**

Run:

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer
git add --renormalize .
git status --short
```

Expected: only the three new/modified files (`.gitattributes`, `.editorconfig`, `.gitignore`) in the status. No accidental modifications to existing files in `docs/`.

If existing files show up, inspect with `git diff --cached` — they're probably line-ending normalizations, which is desired. Commit anyway.

- [ ] **Step 1.5: Commit**

```bash
git add .gitattributes .editorconfig .gitignore
git commit -m "$(cat <<'EOF'
add repo hygiene files specified in dev-setup.md

.gitattributes enforces LF on source and marks .dem as binary;
.editorconfig pins indent/charset/EOL across editors; .gitignore
expanded for Python, Go, Rust, and editor artifacts.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Update `docs/dev-setup.md` with Go + Rust install instructions

**Files:**
- Modify: `docs/dev-setup.md` (lines 107–109 — the "TBD" Go and TypeScript sections)

- [ ] **Step 2.1: Read the existing "Language toolchains" section**

Open `docs/dev-setup.md` and locate the section starting at the line "**Python (when we adopt it):**". The Python block exists; Go and Node/TypeScript are marked TBD. Rust isn't yet mentioned.

- [ ] **Step 2.2: Replace the Go TBD line and add a Rust block**

Find:

```
**Go (when Phase 2 arrives):** TBD.
```

Replace with:

````
**Go:**
Install via the OS package manager — Go itself is a single-binary toolchain.

macOS:
```sh
brew install go
```

Windows (native, PowerShell):
```powershell
winget install GoLang.Go
```
Restart your terminal so `go` is on `PATH`.

Windows (WSL2 Ubuntu):
Don't use `apt install golang` — Ubuntu's package often lags by a major
version. Use the official tarball instead:
```sh
# Replace x.y.z with the current stable version from https://go.dev/dl
curl -LO https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**Rust:**
Install via `rustup` — works the same across all three platforms. It manages
the Rust compiler, `cargo`, `rustfmt`, and `clippy`.

macOS / WSL2:
```sh
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```
Accept the default installation. After it finishes, either restart your
shell or run `source "$HOME/.cargo/env"` to pick up `cargo` on `PATH`.

Windows (native):
Download and run `rustup-init.exe` from [rustup.rs](https://rustup.rs/).
The installer also installs the MSVC build tools prerequisite if missing.

````

- [ ] **Step 2.3: Verify Markdown rendering**

Re-read the file and confirm the new sections are well-formed (no broken code fences, no missing blank lines). The section should preserve the existing tone of the doc (informal, with platform tags).

- [ ] **Step 2.4: Commit**

```bash
git add docs/dev-setup.md
git commit -m "$(cat <<'EOF'
fill in Go and Rust install instructions in dev-setup.md

Replaces the Go TBD line and adds a Rust block, both with macOS,
Windows native, and Windows WSL2 paths matching the doc's existing
three-platform convention.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Install macOS toolchains

**Note:** This task is local to the current Mac. Nothing here gets committed. Windows installs are documented in Task 2 and are not part of this plan's execution.

**Files:**
- No files modified. Tooling state only.

- [ ] **Step 3.1: Verify `uv` is installed**

Run:

```bash
uv --version
```

Expected: a version string (e.g., `uv 0.5.x` or higher).

If "command not found":

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
exec $SHELL -l    # pick up the new PATH
uv --version
```

- [ ] **Step 3.2: Install Go via Homebrew**

Run:

```bash
brew install go
go version
```

Expected: a version string like `go version go1.22.x darwin/arm64`. If `brew` is missing, install from https://brew.sh first.

- [ ] **Step 3.3: Install Rust via rustup**

Run:

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

Accept the default install ("1) Proceed with standard installation"). After it completes:

```bash
source "$HOME/.cargo/env"
cargo --version
rustc --version
rustfmt --version
cargo clippy --version
```

Expected: all four print version strings.

- [ ] **Step 3.4: Confirm all three toolchains are on PATH in a fresh shell**

Open a new terminal tab (or `exec $SHELL -l`) and run:

```bash
uv --version && go version && cargo --version
```

Expected: three version strings, no "command not found." If any fail, the shell rc files weren't sourced — investigate before proceeding.

---

## Task 4: Scaffold the Python playground

**Files:**
- Create: `playground/python/pyproject.toml` (via `uv init`)
- Create: `playground/python/uv.lock`
- Create: `playground/python/src/hello_demo.py`
- Create: `playground/python/README.md`

- [ ] **Step 4.1: Create the directory and initialize the uv project**

Run:

```bash
mkdir -p /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
uv init --name cs2-demo-playground-python --no-readme --bare
```

Expected: `pyproject.toml` is created. `--bare` skips the `hello.py` sample so we own the entry point. `--no-readme` skips uv's README since we'll write our own.

- [ ] **Step 4.2: Add runtime dependencies**

Run:

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
uv add demoparser2 polars
```

Expected: `pyproject.toml` updated with both deps, `uv.lock` created. Both packages download and resolve cleanly.

- [ ] **Step 4.3: Add dev dependency (ruff)**

Run:

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
uv add --dev ruff
```

Expected: `pyproject.toml` updated, `uv.lock` updated.

- [ ] **Step 4.4: Create the `src/` directory and `hello_demo.py`**

```bash
mkdir -p /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python/src
```

Write to `playground/python/src/hello_demo.py`:

```python
"""Open a CS2 .dem and print header + tick-0 + first-live-tick positions."""

import sys
from pathlib import Path

import polars as pl
from demoparser2 import DemoParser


def main() -> int:
    if len(sys.argv) != 2:
        print("Usage: hello_demo <path-to-dem>", file=sys.stderr)
        return 1

    path = Path(sys.argv[1]).resolve()
    if not path.exists():
        print(f"Demo not found: {path}", file=sys.stderr)
        return 1

    parser = DemoParser(str(path))

    # --- Header ---
    header = parser.parse_header()
    print("== Header ==")
    print(f"Map:          {header.get('map_name', '<unknown>')}")
    print(f"Server:       {header.get('server_name', '<unknown>')}")
    print(f"Demo type:    {header.get('demo_version_name', '<unknown>')}")

    # --- Players ---
    # parse_player_info returns a Polars DataFrame of all players seen in the demo.
    players = parser.parse_player_info()
    print("Players:")
    for row in players.iter_rows(named=True):
        name = row.get("name", "<unknown>")
        steamid = row.get("steamid", "<unknown>")
        print(f"  - {name}  ({steamid})")
    print(f"  ({len(players)} total)")

    # --- Tick 0 (raw) ---
    print("\n== Tick 0 (raw first tick) ==")
    tick0 = parser.parse_ticks(["X", "Y", "Z", "name"], ticks=[0])
    if len(tick0) == 0:
        print("  (no rows returned for tick 0 — players may not be spawned yet)")
    else:
        for row in tick0.iter_rows(named=True):
            print(f"  {row['name']:<24}  x={row['X']:>8.1f}  y={row['Y']:>8.1f}  z={row['Z']:>8.1f}")

    # --- First live-round tick ---
    # demoparser2 exposes "game_phase" as a tick prop in recent versions; we
    # scan a generous tick range and pick the smallest tick where phase == "live".
    # If the prop name differs in your version of demoparser2, check
    # `parser.list_game_events()` and the project README for the current name.
    print("\n== First live-round tick ==")
    scan_max = 200_000
    phase_df = parser.parse_ticks(
        ["game_phase"],
        ticks=list(range(0, scan_max)),
    )
    live = phase_df.filter(pl.col("game_phase") == "live")
    if len(live) == 0:
        print(f"  (no 'live' game_phase seen in first {scan_max} ticks)")
        return 0

    first_live_tick = int(live["tick"].min())
    positions = parser.parse_ticks(
        ["X", "Y", "Z", "name"],
        ticks=[first_live_tick],
    )
    print(f"Tick: {first_live_tick}")
    for row in positions.iter_rows(named=True):
        print(f"  {row['name']:<24}  x={row['X']:>8.1f}  y={row['Y']:>8.1f}  z={row['Z']:>8.1f}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 4.5: Register the script entry point in `pyproject.toml`**

Open `playground/python/pyproject.toml` and add (or merge into) a `[project.scripts]` table so `uv run hello-demo` works without typing the full path:

```toml
[project.scripts]
hello-demo = "hello_demo:main"
```

Also add a `[tool.setuptools]` or `[tool.hatch.build.targets.wheel]` packages entry pointing at `src/`. The simplest cross-build-backend solution is to set the source layout. Since `uv init --bare` uses `hatchling` by default, add this section:

```toml
[tool.hatch.build.targets.wheel]
packages = ["src"]
```

And tell hatch that the `src/` dir is the module root. If `uv init` chose a different build backend, adapt accordingly — the goal is that `import hello_demo` works after `uv sync`.

- [ ] **Step 4.6: Run `uv sync` to install the entry point**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
uv sync
```

Expected: no errors. The `hello-demo` script becomes available via `uv run hello-demo`.

- [ ] **Step 4.7: Run against the real demo**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/python
uv run hello-demo ../../data/mega_ot_mirage.dem
```

Expected: output begins with `== Header ==`, contains `Map: de_mirage` (or however the demo names Mirage), shows ~10 players, then tick-0 positions, then a "First live-round tick" section with the chosen tick number and positions.

If the script errors on a method name (`parse_player_info`, `parse_ticks` prop names like `"X"`/`"Y"`/`"Z"`/`"game_phase"`), check the installed version's API:

```bash
uv run python -c "from demoparser2 import DemoParser; help(DemoParser)"
```

Adjust the script to match the actual API surface and re-run. The two highest-risk bits are (a) the player-info accessor name, and (b) the exact game-phase prop. Fix inline.

- [ ] **Step 4.8: Write `playground/python/README.md`**

```markdown
# Python playground

CS2 demo exploration using [demoparser2](https://github.com/LaihoE/demoparser).

## Run

```sh
uv sync                                          # one-time after clone
uv run hello-demo ../../data/mega_ot_mirage.dem  # any .dem path
```

Output:
- Header (map, server, demo type, players)
- Player positions on tick 0
- Player positions on the first tick where `game_phase == "live"`

## Where the "live-round" filter lives

`src/hello_demo.py`, search for `pl.col("game_phase") == "live"`. The
filter is a Polars expression on a tick-level DataFrame returned by
`parser.parse_ticks(["game_phase"], ticks=...)`.

## Why Polars and not pandas

`demoparser2` returns Polars frames natively, so using Polars avoids a
conversion step. The API surface we use here (`iter_rows`, `filter`,
`col`) is small enough that switching to pandas later would be trivial.
```

- [ ] **Step 4.9: Commit**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer
git add playground/python/
git commit -m "$(cat <<'EOF'
add Python playground using demoparser2 + Polars

uv-managed project under playground/python/. hello-demo opens a .dem
and prints header, tick-0 positions, and first-live-round-tick
positions. Verified against data/mega_ot_mirage.dem.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Scaffold the Go playground

**Files:**
- Create: `playground/go/go.mod`
- Create: `playground/go/go.sum`
- Create: `playground/go/cmd/hello-demo/main.go`
- Create: `playground/go/README.md`

- [ ] **Step 5.1: Initialize the Go module**

```bash
mkdir -p /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
go mod init github.com/jeremyjang22/cs2-demo-analyzer/playground/go
```

Expected: `go.mod` created with `module github.com/jeremyjang22/cs2-demo-analyzer/playground/go` and a `go 1.22` (or current) directive.

- [ ] **Step 5.2: Add demoinfocs-golang**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
go get github.com/markus-wa/demoinfocs-golang/v5
```

Expected: `go.mod` updated with the dependency, `go.sum` created. If the v5 major version doesn't resolve (the project occasionally bumps), try `v4` and adjust import paths accordingly.

- [ ] **Step 5.3: Create the `cmd/hello-demo/` directory and `main.go`**

```bash
mkdir -p /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go/cmd/hello-demo
```

Write to `playground/go/cmd/hello-demo/main.go`:

```go
// hello-demo opens a CS2 .dem and prints header, tick-0 positions,
// and the positions on the first live-round tick.
package main

import (
	"fmt"
	"os"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: hello-demo <path-to-dem>")
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	// --- Header ---
	header, err := parser.ParseHeader()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse header: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("== Header ==")
	fmt.Printf("Map:          %s\n", header.MapName)
	fmt.Printf("Server:       %s\n", header.ServerName)
	fmt.Printf("Playback ticks: %d\n", header.PlaybackTicks)
	fmt.Printf("Playback time:  %s\n", header.PlaybackTime)

	// --- State for the two interesting ticks ---
	var (
		tick0Printed    bool
		liveTickPrinted bool
		playersPrinted  bool
	)

	printPositions := func(label string) {
		fmt.Printf("\n== %s ==\n", label)
		fmt.Printf("Tick: %d\n", parser.GameState().IngameTick())
		for _, p := range parser.GameState().Participants().Playing() {
			pos := p.Position()
			fmt.Printf("  %-24s  x=%8.1f  y=%8.1f  z=%8.1f\n", p.Name, pos.X, pos.Y, pos.Z)
		}
	}

	parser.RegisterEventHandler(func(e events.FrameDone) {
		if !playersPrinted {
			fmt.Println("Players:")
			for _, p := range parser.GameState().Participants().All() {
				fmt.Printf("  - %s  (%d)\n", p.Name, p.SteamID64)
			}
			fmt.Printf("  (%d total)\n", len(parser.GameState().Participants().All()))
			playersPrinted = true
		}
		if !tick0Printed {
			printPositions("Tick 0 (raw first tick)")
			tick0Printed = true
		}
	})

	// "Live round" = the first RoundStart event that fires after warmup ends.
	// demoinfocs distinguishes warmup via GameState().IsWarmupPeriod().
	parser.RegisterEventHandler(func(e events.RoundStart) {
		if liveTickPrinted {
			return
		}
		if parser.GameState().IsWarmupPeriod() {
			return
		}
		printPositions("First live-round tick")
		liveTickPrinted = true
	})

	if err := parser.ParseToEnd(); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	if !liveTickPrinted {
		fmt.Println("\n== First live-round tick ==")
		fmt.Println("  (no non-warmup RoundStart event seen)")
	}
}
```

- [ ] **Step 5.4: `go mod tidy` to lock the dependency graph**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
go mod tidy
```

Expected: no errors, `go.sum` populated.

- [ ] **Step 5.5: Build the binary to confirm it compiles**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
go build ./cmd/hello-demo
```

Expected: a `hello-demo` (or `hello-demo.exe` on Windows) binary in the current directory, no compile errors.

If the build fails on a symbol that doesn't exist (e.g. `SteamID64` was renamed `SteamID`, or `IsWarmupPeriod` is on a different receiver), open the demoinfocs-golang godoc at https://pkg.go.dev/github.com/markus-wa/demoinfocs-golang/v5 and adjust. The two highest-risk APIs here are (a) the player struct field for steam id, and (b) the warmup accessor.

- [ ] **Step 5.6: Run against the real demo**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/go
./hello-demo ../../data/mega_ot_mirage.dem
```

Expected: output begins with `== Header ==`, contains `Map: de_mirage`, ~10 players, then tick-0 positions, then first-live-tick positions. The map name and player roster should match the Python playground's output.

- [ ] **Step 5.7: Write `playground/go/README.md`**

```markdown
# Go playground

CS2 demo exploration using [demoinfocs-golang](https://github.com/markus-wa/demoinfocs-golang).

## Run

```sh
go build ./cmd/hello-demo
./hello-demo ../../data/mega_ot_mirage.dem
```

Or in one shot:

```sh
go run ./cmd/hello-demo ../../data/mega_ot_mirage.dem
```

Output mirrors the Python and Rust playgrounds: header, tick-0
positions, first-live-round-tick positions.

## Where the "live-round" filter lives

`cmd/hello-demo/main.go`, search for `IsWarmupPeriod`. demoinfocs is
event-driven, so the filter is a guard inside the `events.RoundStart`
handler that skips warmup rounds.

## Why a binary in `cmd/hello-demo/`

Standard Go layout — `cmd/<name>/main.go` per binary, leaving room for
additional CLIs (`cmd/inspect-ticks/`, etc.) without restructuring.
```

- [ ] **Step 5.8: Commit**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer
git add playground/go/
git commit -m "$(cat <<'EOF'
add Go playground using demoinfocs-golang

go module under playground/go/. cmd/hello-demo opens a .dem and prints
header, tick-0 positions, and first-non-warmup-round-tick positions.
Verified against data/mega_ot_mirage.dem.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Scaffold the Rust playground

**Files:**
- Create: `playground/rust/Cargo.toml`
- Create: `playground/rust/Cargo.lock`
- Create: `playground/rust/src/main.rs`
- Create: `playground/rust/README.md`

**API risk:** The Rust `demoparser2` crate's public API surface is less polished than the Python bindings. The most likely outcomes for this task are: (a) the crate is on crates.io and works; (b) the crate is published but the public API is narrow and we need a git dep; (c) we need to fall back to a different CS2-supporting Rust parser like `cs-demo-parser-rs` or use the FFI surface of `demoparser2` directly. The verification step below decides.

- [ ] **Step 6.1: Verify `demoparser2` is available as a Rust crate**

Run:

```bash
cargo search demoparser2 --limit 5
```

If a published crate appears, note its name and current version. If nothing appears, the fallback is a git dependency on the upstream repo. Either way, choose:

- **Path A (published):** use `demoparser2 = "<version>"` in `Cargo.toml`.
- **Path B (git only):** use `demoparser2 = { git = "https://github.com/LaihoE/demoparser", branch = "main" }`. The actual crate name inside that repo may differ — open the repo's `Cargo.toml` to confirm the published crate name and any required `package = "..."` rename.
- **Path C (no usable crate):** if neither A nor B works, pause and notify the user before pivoting to a different parser. Do not silently swap libraries.

- [ ] **Step 6.2: Initialize the Cargo project**

```bash
mkdir -p /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cargo init --bin --name hello-demo .
```

Expected: `Cargo.toml`, `src/main.rs`, and an empty `Cargo.lock` skeleton are created. `cargo init .` initializes in the current directory rather than creating a subdirectory.

- [ ] **Step 6.3: Add the demoparser2 dependency**

Edit `playground/rust/Cargo.toml`. Under `[dependencies]`, add the dep from Step 6.1 (path A or B). Example for Path A:

```toml
[dependencies]
demoparser2 = "0.x"   # replace with the version from `cargo search`
```

Or Path B:

```toml
[dependencies]
demoparser2 = { git = "https://github.com/LaihoE/demoparser", branch = "main" }
```

Then:

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cargo fetch
```

Expected: dep resolves and is downloaded, `Cargo.lock` updated.

- [ ] **Step 6.4: Read the crate's public API**

Run:

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cargo doc --no-deps --open=false
```

Note the entry points (the parser struct name, header method, tick iteration API). The Rust API will not mirror the Python API exactly — names like `parse_ticks` may be `parse` returning an iterator, or similar. Adjust Step 6.5's code to match what you see.

- [ ] **Step 6.5: Write `src/main.rs`**

Write to `playground/rust/src/main.rs`. The code below is a *template* matching the same output as the Python and Go playgrounds; method names will likely need adjustment to whatever the crate actually exposes (see Step 6.4):

```rust
//! hello-demo: open a CS2 .dem and print header + tick-0 + first-live-tick positions.

use std::env;
use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = env::args().collect();
    if args.len() != 2 {
        eprintln!("Usage: hello-demo <path-to-dem>");
        return ExitCode::from(1);
    }
    let path = &args[1];

    // --- API placeholder ---
    // The exact method names below depend on the demoparser2 crate's public
    // API as verified in Step 6.4. Adjust if cargo doc shows different names.
    //
    // Typical shape:
    //   let parser = DemoParser::new_from_file(path)?;
    //   let header = parser.parse_header()?;
    //   let ticks: DataFrame = parser.parse_ticks(&["X", "Y", "Z", "name", "game_phase"], None)?;
    //
    // If the Rust crate returns an Arrow RecordBatch instead of a Polars
    // DataFrame, iterate the batch's columns directly.

    let parser = demoparser2::DemoParser::new_from_file(path).expect("open demo");

    let header = parser.parse_header().expect("parse header");
    println!("== Header ==");
    println!("Map:          {}", header.map_name);
    println!("Server:       {}", header.server_name);

    // Players
    let players = parser.parse_player_info().expect("player info");
    println!("Players:");
    for p in &players {
        println!("  - {}  ({})", p.name, p.steamid);
    }
    println!("  ({} total)", players.len());

    // Tick 0 + first live tick: pull X/Y/Z/name/game_phase for ticks 0..=200_000
    // then filter in-memory. Adjust to use the crate's filtering API if it has one.
    let scan_max: u32 = 200_000;
    let tick_props = ["X", "Y", "Z", "name", "game_phase"];
    let ticks = parser
        .parse_ticks(&tick_props, Some((0..scan_max).collect()))
        .expect("parse ticks");

    print_positions("Tick 0 (raw first tick)", ticks.iter().filter(|t| t.tick == 0));

    let first_live = ticks.iter().find(|t| t.game_phase.as_deref() == Some("live"));
    match first_live {
        Some(t) => {
            let tick_n = t.tick;
            let same_tick = ticks.iter().filter(|t| t.tick == tick_n);
            print_positions("First live-round tick", same_tick);
        }
        None => {
            println!("\n== First live-round tick ==");
            println!("  (no 'live' game_phase in first {scan_max} ticks)");
        }
    }

    ExitCode::SUCCESS
}

// `Tick` here stands in for the crate's actual tick row type. Replace with
// whatever the crate returns (likely a struct or a typed row from a frame).
fn print_positions<'a, I, T>(label: &str, rows: I)
where
    I: Iterator<Item = &'a T>,
    T: TickRow + 'a,
{
    println!("\n== {label} ==");
    for r in rows {
        println!(
            "  {:<24}  x={:>8.1}  y={:>8.1}  z={:>8.1}",
            r.name(),
            r.x(),
            r.y(),
            r.z(),
        );
    }
}

trait TickRow {
    fn name(&self) -> &str;
    fn x(&self) -> f32;
    fn y(&self) -> f32;
    fn z(&self) -> f32;
}
```

**Important:** the `TickRow` trait and `parse_ticks` shape above are a template, not verified API. The Rust crate may instead return a `RecordBatch` (Arrow), a Polars `DataFrame` (if the crate re-exports it), or a custom struct. After running Step 6.4, rewrite this file to match the actual API. The output format and intent are what matter — the data access pattern is up to whatever the crate provides.

- [ ] **Step 6.6: Build**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cargo build
```

Expected: clean build (warnings OK). If it fails, the API in Step 6.5 didn't match what the crate provides — go back to Step 6.4, read the docs again, and rewrite. This is expected on the first try and is the main reason Step 6.4 exists.

- [ ] **Step 6.7: Run against the real demo**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer/playground/rust
cargo run --release -- ../../data/mega_ot_mirage.dem
```

`--release` matters here — debug builds of `demoparser2` are slow enough on a 700MB demo to feel broken. Release is fast.

Expected: header with `Map: de_mirage`, ~10 players, tick-0 positions, first-live-tick positions. Map and players should match both the Python and Go outputs.

- [ ] **Step 6.8: Write `playground/rust/README.md`**

```markdown
# Rust playground

CS2 demo exploration using the [demoparser2](https://github.com/LaihoE/demoparser) Rust crate.

## Run

```sh
cargo run --release -- ../../data/mega_ot_mirage.dem
```

`--release` is important: a 700MB demo is meaningfully slower in debug
builds.

Output mirrors the Python and Go playgrounds: header, tick-0
positions, first-live-round-tick positions.

## Where the "live-round" filter lives

`src/main.rs`, search for `game_phase`. Filtering is in-memory over the
parsed tick rows — the crate hands back tick data and we filter
ourselves.

## API caveat

The Rust crate's public API is less stable than the Python bindings.
If the build breaks after a `cargo update`, check the crate's
changelog and adjust this file's API calls.
```

- [ ] **Step 6.9: Commit**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer
git add playground/rust/
git commit -m "$(cat <<'EOF'
add Rust playground using demoparser2 crate

Cargo binary under playground/rust/. hello-demo opens a .dem and
prints header, tick-0 positions, and first-live-round-tick positions.
Verified against data/mega_ot_mirage.dem with --release.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Cross-language consistency check

**Files:**
- No code changes. This is a verification step.

- [ ] **Step 7.1: Run all three playgrounds and capture output**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer

uv run --project playground/python hello-demo data/mega_ot_mirage.dem > /tmp/hello-python.txt
( cd playground/go && go run ./cmd/hello-demo ../../data/mega_ot_mirage.dem ) > /tmp/hello-go.txt
( cd playground/rust && cargo run --release -- ../../data/mega_ot_mirage.dem ) > /tmp/hello-rust.txt
```

- [ ] **Step 7.2: Compare the headers**

```bash
echo "=== python ===" && grep -E '^(Map|Players:|  -|  \()' /tmp/hello-python.txt | head -20
echo "=== go ===" && grep -E '^(Map|Players:|  -|  \()' /tmp/hello-go.txt | head -20
echo "=== rust ===" && grep -E '^(Map|Players:|  -|  \()' /tmp/hello-rust.txt | head -20
```

Expected: same map name and same 10 player names across all three. Steam IDs may format differently (steamid64 vs steamid3 vs name-only), which is acceptable — note the differences as the first concrete cross-parser finding.

- [ ] **Step 7.3: Note disagreements as findings, not bugs**

If anything disagrees (different tick numbers for "first live tick," different player counts, different position coordinates by a small amount), record the difference in a short note. This is the first interesting research output of the project — it tells you which parser is your reference and which need investigation later.

Do not fix the playgrounds to match each other. The point is to surface that "the same intent" produces slightly different concrete outputs.

- [ ] **Step 7.4: Final repo status check**

```bash
cd /Users/jeremyjang/Desktop/Projects/cs2-demo-analyzer
git status --short
git log --oneline -10
```

Expected: working tree clean, recent commits include the spec, hygiene files, dev-setup updates, and three playground scaffolds. No stray uncommitted artifacts.

---

## Self-Review

1. **Spec coverage:**
   - Repo layout with `playground/{python,go,rust}/` → Task 4 / 5 / 6.
   - Per-language `hello_demo` with the three-section output → Steps 4.4 / 5.3 / 6.5.
   - macOS toolchain installs → Task 3.
   - `dev-setup.md` Go + Rust sections for macOS / Win-native / WSL2 → Task 2.
   - `.gitattributes` / `.editorconfig` / `.gitignore` extensions → Task 1.
   - Ruff as dev dep → Step 4.3.
   - No tests, no CI, no version pin files → respected (not added anywhere).
   - Rust crate availability open question → Task 6, Step 6.1 verifies.
   - Verification = "same map + same roster across all three" → Task 7.

2. **Placeholder scan:** The Rust code block in Step 6.5 is explicitly labeled a *template* because the Rust API is unverified at planning time. This is honest framing, not a placeholder. The Step 6.4 instruction directs the executor to read the actual API and rewrite — that's a real, executable step, not a TBD.

3. **Type consistency:** Function/method names are internally consistent within each language's task. Cross-language differences (e.g., `parse_ticks` vs. `RegisterEventHandler`) are intentional and reflect each library's idiom.
