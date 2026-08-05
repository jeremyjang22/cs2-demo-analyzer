# round-collector design

**Date:** 2026-08-05
**Status:** approved, ready for implementation planning

## Purpose

Turn a CS2 `.dem` file into a queryable, round-by-round record of what every
player did on every tick. It is the substrate layer for everything downstream:
mistake-finding, counterstrafe scoring, spray analysis, heatmaps.

The design goal is to parse once and never again. Parsing a 667 MB demo takes
minutes; filtering a dumped file takes milliseconds. So the collector captures
generously and leaves selection to the query.

## Decisions

| Decision | Choice | Reason |
|---|---|---|
| Shape | Library + thin CLI | Future analyzers import the library; the CLI is one consumer |
| Materialization | Per round | Analysis is windowed; a round bounds memory at ~14 MB |
| Row schema | Movement + mechanics payload | Captures the counterstrafe/spray substrate up front |
| On-disk format | Gzipped CSV + manifest | Stdlib only, inspectable, ~50-80 MB; behind an interface |
| Round window | All three phases, tagged | Frozen rows compress to nothing; filter in SQL, not by re-parsing |
| Sampling | Every tick | A counterstrafe spans 5-10 ticks; downsampling destroys the signal |

## Scale

Grounded on `data/mega_ot_mirage.dem` (667 MB, overtime match):

- 64 ticks/sec x 10 players = 640 rows per second of gameplay
- ~110s per round (freeze + live + post) → ~70k samples per round
- ~50-70 rounds → **~3-4M tick rows**
- Peak memory: one round, ~70k x ~200 B ≈ **14 MB**, flat regardless of match length
- Output: **~50-80 MB** gzipped (vs ~400-650 MB plain CSV, ~1.2-2 GB JSONL)

## Package layout

Follows the existing `packages/<name>/` convention. `round-collector/main.go`
stays the CLI entry point; reusable logic moves into a library package.

```
packages/
  go.mod                       # fix module path (currently says .../playground/go)
  collector/                   # package collector
    collector.go               # Collector, New, OnRound, Run
    tick.go                    # PlayerTick - the schema's single source of truth
    round.go                   # Round, RoundMeta, PlayerRound
    phase.go                   # Phase enum
    sink.go                    # Sink interface
    csvsink/
      csvsink.go               # gzip CSV + manifest implementation
  round-collector/
    main.go                    # CLI: flags, wiring, progress
  event-processor/
    main.go                    # existing, untouched
```

`packages/go.mod` currently declares
`module github.com/jeremyjang22/cs2-demo-analyzer/playground/go` — a copy-paste
from the playground. Fixing it to `.../packages` is a prerequisite, since two
modules sharing a path will break cross-imports.

## Data model

`PlayerTick` is the schema. CSV column order derives from struct field order,
so adding a column is a one-line change here and nowhere else. This is what
makes the schema evolvable.

```go
type Phase uint8   // freeze | live | postround

type PlayerTick struct {
    Round   int32
    Tick    int32          // ingame tick
    Phase   Phase
    SteamID uint64
    Team    common.Team    // on the tick row - it changes at halftime

    X, Y, Z          float32   // world position (Hammer units)
    Yaw, Pitch       float32   // view angles

    VelX, VelY, VelZ float32
    Speed            float32   // XY magnitude, derived

    Buttons          uint64    // raw bitmask - the counterstrafe substrate

    IsDucking, IsWalking, IsAirborne, IsScoped bool

    Health, Armor  int16
    IsAlive        bool
    FlashRemaining float32
    ActiveWeapon   string
    Place          string     // LastPlaceName()
}

type Round struct {
    Meta  RoundMeta
    Ticks []PlayerTick   // all players, all phases, tick-ordered
}

type RoundMeta struct {
    Number                                             int32
    StartTick, FreezeEndTick, EndTick, OfficialEndTick int32
    Winner        common.Team
    Reason        events.RoundEndReason
    TimeoutBefore bool
    TimeoutTeam   common.Team
    Complete      bool          // false if the demo cut off mid-round
    Players       []PlayerRound
}

type PlayerRound struct {
    SteamID               uint64
    Team                  common.Team
    MoneyAtFreezeEnd      int32
    EquipValueAtFreezeEnd int32
    Survived              bool
}
```

Player names are deliberately absent from the tick row: repeating a name across
~4M rows is waste, and names change mid-match. `SteamID` is the stable key and
`players.csv` maps it to a name. This mirrors the lock-to-SteamID approach
already used in `playground/go/cmd/track-player/main.go:111`.

### Open item: velocity source

demoinfocs v5 exposes no `Player.Velocity()` — only `GrenadeProjectile` has one
(`common/common.go:47`). Two ways to populate `VelX/Y/Z`:

1. Read `m_vecVelocity` off the player pawn entity via the raw-property escape
   hatch (`Entity.PropertyValueMust`). True server-side value, but the property
   name is unconfirmed for CS2 pawns.
2. Difference positions between consecutive ticks. Guaranteed to work; accurate
   enough for stop-detection at 64 Hz.

**Plan:** spike option 1 first (dump `PlayerPawnEntity().Properties()` and grep),
fall back to option 2. Whichever wins is recorded in the manifest as
`velocity_source` so downstream analysis knows what it is reading. `Speed` is
populated either way, so nothing downstream depends on the outcome.

## Data flow

```
.dem --> demoinfocs parser --> collector handlers --> *Round --> Sink(s)
                                      |                 |
                                 accumulates       released after
                                 current round     each callback
```

| Event | Action |
|---|---|
| `AnnouncementMatchStarted` | arm live gating |
| `RoundStart` | open new `Round`, phase → `freeze` |
| `RoundFreezetimeEnd` | phase → `live`, snapshot per-player economy |
| `RoundEnd` | phase → `postround`, record winner + reason |
| `RoundEndOfficial` | finalize, fire `OnRound`, release memory |
| `FrameDone` | append one `PlayerTick` per playing participant |

Live gating reuses the strategy proven in `playground/go/cmd/hello-demo` and
`track-player`: `AnnouncementMatchStarted` as floor plus `!IsWarmupPeriod()`,
which filters warmup and the false-positive tick-0 `RoundStart`.

### Edge cases

Each of these produces silently wrong data if unhandled:

- **Final round never fires `RoundEndOfficial`** (demos commonly cut off).
  Flush the pending round at parse end with `Complete: false`.
- **Round restarts** (`mp_restartgame`) fire a second `RoundStart` with no
  intervening end. Discard the partial round rather than emitting a truncated one.
- **Timeouts** have unverified ordering relative to `RoundStart` — see below.

### Timeouts

demoinfocs has no timeout event, no timeout `GamePhase`, and no timeout method.
The properties exist in the demo but are an explicit TODO in the library at
`datatables.go:1279`:

```
// TODO: timeout data
// "m_bTerroristTimeOutActive"  "m_bCTTimeOutActive"
// "m_flTerroristTimeOutRemaining"  "m_flCTTimeOutRemaining"
// "m_nTerroristTimeOuts"  "m_nCTTimeOuts"
```

They are reachable via `GameState().Rules().Entity().PropertyValueMust(...)`.

Timeouts occur between rounds, so they fall outside the capture window under
this design. **Unverified:** whether CS2 fires `RoundStart` before a timeout
begins. If it does, a 30s timeout injects ~19k frozen rows tagged `freeze` and
inflates that round's apparent freezetime. Harmless for any analysis filtering
to `phase = 'live'`, but confusing if unexplained.

Resolution: record `TimeoutBefore` / `TimeoutTeam` at round level (~10 lines),
and verify the ordering empirically against `mega_ot_mirage.dem` during
implementation by logging round boundary ticks and looking for outliers.

## Output format

```
out/mega_ot_mirage/
  manifest.json        # schema version, map, tickrate, column lists, counts
  players.csv          # one row per (steamid, name) observed
  rounds.csv           # one row per round
  round_players.csv    # one row per (round, player)
  ticks.csv.gz         # the big one
```

```json
{
  "schema_version": 1,
  "demo_file": "mega_ot_mirage.dem",
  "map": "de_mirage",
  "tick_rate": 64,
  "rounds": 62,
  "tick_rows": 3847221,
  "complete": true,
  "velocity_source": "entity_property",
  "columns": {
    "ticks": ["round", "tick", "phase", "steamid", "team", "x", "y", "z", "..."],
    "rounds": ["number", "start_tick", "freeze_end_tick", "winner", "..."],
    "round_players": ["round", "steamid", "money_at_freeze_end", "..."],
    "players": ["steamid", "name", "first_seen_tick"]
  }
}
```

`players.csv` is an observation table rather than a keyed lookup: one row per
distinct `(steamid, name)` pair with the tick it was first seen. A player who
changes name mid-match produces two rows. This keeps name history without
putting names on ~4M tick rows.

`velocity_source` records how velocity was obtained. `complete` is false when a
demo cut off, so a truncated dump is never mistaken for a whole match.

**Versioning rule:** appending a trailing column bumps minor and stays
backward-compatible for header-aware readers (DuckDB, pandas). Removing or
reordering columns bumps major. Readers check the manifest.

The `Sink` interface keeps the format decision reversible:

```go
type Sink interface {
    Round(*Round) error
    Close() error
}
```

`csvsink` implements it today; Parquet later is a new file, not a rewrite.

## CLI

```
round-collector -demo <path> [-out <dir>] [-max-rounds N] [-quiet]
```

- `-demo` — required, path to the `.dem`
- `-out` — output directory, defaults to `./out/<demo-basename>/`
- `-max-rounds N` — calls `parser.Cancel()` after N rounds. A development
  affordance: iterating against a 667 MB demo takes seconds instead of minutes.
- `-quiet` — suppress per-round progress output

## Error handling

The library returns errors; the CLI decides to exit. `track-player` currently
calls `os.Exit` from inside its logic (`main.go:383`), which is fine for a
playground script but makes code untestable and unusable as a library.

- **Missing entity property** → log once, degrade to the fallback, record in the
  manifest. Never fail a run over one absent field.
- **Sink write failure** → `parser.Cancel()`, return the error, leave partial
  output with `"complete": false`.
- **Truncated demo** (`ErrUnexpectedEndOfDemo`) → partial success. Flush the
  pending round, mark it incomplete. A cut-off demo should still yield its
  usable rounds.

## Testing

demoinfocs ships a mock parser at `pkg/demoinfocs/fake` with
`MockEventsFrame(frame, events...)` that satisfies the `demoinfocs.Parser`
interface. The constructor takes that interface so the state machine is testable
with no demo files:

```go
func New(p demoinfocs.Parser) *Collector    // takes the interface - testable
func NewFromReader(r io.Reader) *Collector  // convenience wrapper
```

**Unit tests** (via `fake.Parser`) — these cover exactly where data corrupts
silently:

- phase transitions across a normal round
- round restart discards the partial round
- final round with no `RoundEndOfficial` still flushes, marked incomplete
- warmup and the tick-0 false-positive `RoundStart` are filtered
- economy snapshot is taken at freezetime end, not round start

**Integration test** — one small demo fixture (the 667 MB file is impractical
for CI) asserting round count, column headers, and known values.

**Manual verification** — run against `mega_ot_mirage.dem`, confirm round count
matches the scoreboard, output size lands in the 50-80 MB range, and the file
loads in DuckDB.

## Out of scope

Deliberately excluded to keep this focused:

- Analysis of any kind — no counterstrafe scoring, no spray metrics, no mistake
  detection. This produces the substrate those will consume.
- Parquet output (interface makes it a later drop-in)
- Grenade trajectories, inferno shapes, hostage state
- Multi-demo batch processing
- Any visualization

## Prerequisites

1. Fix `packages/go.mod` module path from `.../playground/go` to `.../packages`
2. Spike the velocity property question (~10 min)
