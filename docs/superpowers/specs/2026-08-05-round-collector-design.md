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

    VelX, VelY, VelZ float32   // derived by position differencing - see below
    Speed            float32   // XY magnitude, units/sec

    Buttons          uint64    // raw bitmask - the counterstrafe substrate
                               // NOTE: bound to m_nButtonDownMaskPrev, the PREVIOUS
                               // command's mask, so it lags position by one tick

    ShotsFired           int16   // m_iShotsFired - spray index, resets between bursts
    PunchYaw, PunchPitch float32 // m_vecCsViewPunchAngle - recoil kick on the view
    AccuracyPenalty      float32 // m_fAccuracyPenalty on the active weapon - live
                                 // inaccuracy; higher = worse. Replaced MaxSpeed.

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

### Resolved: velocity comes from position differencing

**Spiked 2026-08-05 against `mega_ot_mirage.dem` — settled.** A `CCSPlayerPawn`
carries 324 network properties and **none of them is a current-velocity vector.**
The only velocity-named properties are:

```
m_vecBaseVelocity                          [0 0 0]   external pushes only, always zero
m_flVelocityModifier                       1         speed penalty scalar
m_pMovementServices.m_flFallVelocity       0
m_pMovementServices.m_flLastJumpVelocityZ  257.37
m_pMovementServices.m_flLastLandedVelocityX/Y/Z
```

CS2 does not network current player velocity — it is client-side predicted, so it
never reaches the demo. Position differencing is therefore the only option, not a
fallback:

```
dt   = (tick - prevTick) / tickRate
VelX = (X - prevX) / dt        // units/sec
Speed = hypot(VelX, VelY)      // XY plane; Z excluded so jumping doesn't inflate it
```

`velocity_source` in the manifest is permanently `position_diff`. The first tick
of each player's presence in a round has no predecessor, so velocity is 0 and a
`vel_valid` bool marks it — consumers filter on that rather than seeing a bogus
zero. Velocity also resets on respawn and after death gaps.

### Properties the spike surfaced

The same probe found three properties the public API does not expose, all of
which serve the mechanical-skill goals directly and are expensive to backfill
(re-parsing 667 MB), so they are captured now:

- **`m_iShotsFired`** → `ShotsFired`. The spray index — resets between bursts, so
  it identifies bullet 1 vs bullet 12. This is the spine of any recoil-control
  metric.
- ~~**`m_pMovementServices.m_flMaxspeed`** → `MaxSpeed`~~ — **REMOVED, spiked
  2026-08-05.** The premise was wrong. That property reads **260.00 for every
  weapon** — knife, Glock, USP, AK-47, grenades — so the column carried zero
  information. A follow-up probe of the active weapon entity (`CAK47`, **355
  properties**) found no max-speed field either: the weapon-adjusted cap is a
  static item-schema value the game reads from its own files and never networks.
  It is not obtainable from a demo at any price.

  Decision: drop the column rather than hardcode a per-weapon table inside the
  parser, which would go stale on any Valve rebalance and silently produce wrong
  normalization. Consumers can join the existing `active_weapon` column against a
  table they maintain, where the assumption is visible. **Do not re-run this
  probe** — the answer is recorded here.

- **`m_fAccuracyPenalty`** (on the ACTIVE WEAPON entity, not the pawn; note
  `m_f`, not `m_fl`) → `AccuracyPenalty`. Found during the max-speed probe and
  captured in its place. The weapon's live inaccuracy value: it rises while
  firing and moving, decays as the weapon settles. The most direct spray-quality
  signal in the data.
- **`m_pCameraServices.m_vecCsViewPunchAngle`** → `PunchYaw`, `PunchPitch`. The
  recoil kick applied to the view. Diffed against actual aim angles, it shows
  whether a player is compensating for the pattern or fighting it.

Other useful properties found and deliberately deferred (YAGNI — recorded so the
next person does not re-run the probe): `m_pMovementServices.m_bDucked` /
`m_bDucking` / `m_flDuckAmount` (finer duck state than `IsDucking`),
`m_hGroundEntity` (ground contact), `m_pBulletServices.m_totalHitsOnServer`,
`m_bSpottedByMask.*` (per-player visibility bitmask), `m_bIsDefusing`,
`m_flVelocityModifier` (post-hit slowdown).

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

**RESOLVED empirically, 2026-08-05.** Timeouts sit **inside the following
round's freezetime**, not in a gap between rounds — `RoundEndOfficial` and the
next `RoundStart` fire on the same tick, back to back, so there is no gap to sit
in. Measured on `mega_ot_mirage.dem`: the two timeout rounds have freezetime
spans of **7,378 and 3,755 ticks against 1,280-1,696 for the other 46**.

So the risk flagged above is real: a timeout round does carry thousands of extra
frozen-player rows tagged `freeze`. They compress to almost nothing and are
harmless to any analysis filtering `phase = 'live'`, and `timeout_before` now
labels exactly which rounds they are.

Two implementation notes, both learned the hard way:

1. **The property names need a `m_pGameRules.` prefix.** demoinfocs resolves
   every game-rules property through that prefix, so the bare names quoted from
   the library's TODO comment always return `ok=false`. The first implementation
   shipped a permanently-zero column because of this.
2. **A one-shot read at `RoundStart` does not work.** A timeout is a window, not
   a state at an instant. Detection watches for the flag's rising edge per frame
   and attributes it to the round open at that moment (not the literal next
   `RoundStart`, which lands one round late).

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
  "schema_version": "1.0",
  "tick_rate_source": "measured",
  "demo_file": "mega_ot_mirage.dem",
  "map": "de_mirage",
  "tick_rate": 64,
  "rounds": 62,
  "tick_rows": 3847221,
  "complete": true,
  "velocity_source": "position_diff",
  "columns": {
    "ticks": ["round", "tick", "phase", "steamid", "team", "x", "y", "z", "..."],
    "rounds": ["number", "start_tick", "freeze_end_tick", "winner", "..."],
    "round_players": ["round", "steamid", "money_at_freeze_end", "..."],
    "players": ["steamid", "name", "first_seen_tick"]
  }
}
```

`players.csv` **as shipped** is one row per SteamID holding their last-seen name,
sorted by SteamID for reproducibility. Names live here rather than on ~3M tick
rows because repeating them is waste.

The full `(steamid, name, first_seen_tick)` observation table described earlier
in drafts is **deliberately deferred** — it only matters for a demo with a
mid-match rename, and none has been observed yet. `players` is consequently
absent from the manifest's `columns` map.

`tick_rate_source` records whether the rate was `"measured"` from
`CSVCMsg_ServerInfo` or `"assumed"` because the demo never reported one. This
exists because an early version silently defaulted to 64 and recorded `-1`,
making every velocity in the file unverifiable after the fact.

`velocity_source` records how velocity was obtained (always `position_diff` —
see the velocity section). `complete` is false when a demo cut off, so a
truncated dump is never mistaken for a whole match.

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

**`fake.Parser` was NOT used.** It embeds `testify/mock`, which would add a
dependency and turn every state-machine test into mock-expectation plumbing.
Instead the round-assembly logic lives in a pure `assembler` with no parser
dependency, tested with plain Go and zero mocking. The constructor still takes
the `demoinfocs.Parser` interface rather than an `io.Reader`, which keeps the
door open:

```go
func New(p demoinfocs.Parser) *Collector    // takes the interface - testable
func NewFromReader(r io.Reader) *Collector  // convenience wrapper
```

**Unit tests** cover the pure core, where the ordering rules live:

- phase transitions across a normal round
- round restart discards the partial round
- final round with no `RoundEndOfficial` still flushes, marked incomplete
- duplicate `roundEnd` is ignored; stray events before the first `RoundStart`
  neither panic nor emit
- velocity arithmetic, including XY-only speed and the no-predecessor cases
- CSV column/value alignment for every output file

**Known coverage gap — read this before trusting a green suite.** The row
production path (`sampleFrame`, `readPawnProps`, `pollTimeout`,
`snapshotEconomy`, `markSurvivors`) has NO unit tests, because it needs a live
parser and mocking was ruled out. **Every serious defect found in this project
lived in that region**, and none was catchable by a unit test as the package is
structured: `buttons` 100% zero, `max_speed` constant, timeouts dead, velocity
computed on a fallback tick rate, phantom SteamID-0 rows, duplicate primary
keys, one-round-stale equipment values, phantom origin rows on reconnect.

The structural fix is to extract per-player row construction into a pure
function over a small interface (position, health, alive, property getter),
testable with a hand-written stub and no dependency. Until that exists, treat
artifact-level assertions as the real gate.

**Artifact assertions** — the checks that actually catch this class of bug. Run
them against regenerated output, not just the test suite:

- `(round, tick, steamid)` is unique
- every `(round, steamid)` in ticks has a row in `round_players.csv`
- no player's entire presence in a round sits at one fixed position
- no eco round reports rifle-priced equipment values
- `vel_valid = 0` count equals exactly one first-sample per (round, player)
- warmup is excluded: round count matches the match scoreboard

**Integration test** — a small demo fixture (the 667 MB file is impractical for
CI) asserting round count, column headers, and known values. **Not yet written.**

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
2. ~~Spike the velocity property question~~ — done 2026-08-05, resolved to
   position differencing (see the velocity section)
