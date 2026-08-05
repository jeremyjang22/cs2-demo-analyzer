# round-collector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a CS2 `.dem` into per-round CSV dumps of every player's position and mechanical state on every tick.

**Architecture:** A pure round-assembly state machine (`assembler`) with no parser dependency, wrapped by a thin adapter (`Collector`) that translates demoinfocs events into calls on it. Completed rounds are handed to a `Sink` interface; `csvsink` writes gzipped CSV plus a JSON manifest. Functional core, imperative shell — the core is unit-testable with plain Go and no mocks.

**Tech Stack:** Go 1.26, `github.com/markus-wa/demoinfocs-golang/v5` v5.2.0, stdlib only otherwise (`encoding/csv`, `compress/gzip`, `encoding/json`, `testing`).

**Spec:** `docs/superpowers/specs/2026-08-05-round-collector-design.md`

## Global Constraints

- Go 1.26; module path `github.com/jeremyjang22/cs2-demo-analyzer/packages`
- demoinfocs-golang v5.2.0 is the ONLY non-stdlib dependency. Do not add testify, cobra, or any assertion library — tests use stdlib `testing` with `if got != want { t.Errorf(...) }`.
- Library code NEVER calls `os.Exit` or `panic`. It returns errors. Only `round-collector/main.go` exits.
- Velocity is ALWAYS derived by position differencing. CS2 does not network player velocity — do not search for `m_vecVelocity`, it does not exist (verified 2026-08-05 across 324 pawn properties).
- Indentation: 4-space per `.editorconfig`, but Go files use gofmt (tabs). Run `gofmt -w` on every file before committing.
- Column names in CSV are `snake_case`. Go field names are `PascalCase`.

---

### Task 1: Module setup and core types

**Files:**
- Modify: `packages/go.mod` (module path line)
- Create: `packages/collector/phase.go`
- Create: `packages/collector/tick.go`
- Create: `packages/collector/round.go`
- Test: `packages/collector/tick_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Phase` (uint8 enum with `PhaseFreeze`/`PhaseLive`/`PhasePostRound`, `String() string`); `PlayerTick` struct; `TickColumns() []string`; `(*PlayerTick).AppendRow(dst []string) []string`; `Round`, `RoundMeta`, `PlayerRound` structs

- [ ] **Step 1: Fix the module path**

`packages/go.mod` currently declares the playground's module path — a copy-paste. Change the first line:

```
module github.com/jeremyjang22/cs2-demo-analyzer/packages
```

Leave the `go 1.26` line and all `require` blocks exactly as they are.

- [ ] **Step 2: Write the failing test**

Create `packages/collector/tick_test.go`:

```go
package collector

import "testing"

func TestPhaseString(t *testing.T) {
    cases := []struct {
        phase Phase
        want  string
    }{
        {PhaseFreeze, "freeze"},
        {PhaseLive, "live"},
        {PhasePostRound, "postround"},
    }
    for _, c := range cases {
        if got := c.phase.String(); got != c.want {
            t.Errorf("Phase(%d).String() = %q, want %q", c.phase, got, c.want)
        }
    }
}

// The CSV header and the row emitter must stay in lockstep. If they drift,
// every column after the drift point silently holds the wrong data - the
// nastiest possible bug in a data pipeline, because nothing errors.
func TestAppendRowMatchesColumns(t *testing.T) {
    tick := PlayerTick{Round: 1, Tick: 100, SteamID: 76561198000000000}
    row := tick.AppendRow(nil)
    if len(row) != len(TickColumns()) {
        t.Fatalf("AppendRow produced %d values, TickColumns has %d",
            len(row), len(TickColumns()))
    }
}

func TestAppendRowReusesBuffer(t *testing.T) {
    tick := PlayerTick{Round: 1}
    buf := make([]string, 0, len(TickColumns()))
    for i := 0; i < 3; i++ {
        buf = tick.AppendRow(buf[:0])
        if len(buf) != len(TickColumns()) {
            t.Fatalf("iteration %d: got %d values, want %d",
                i, len(buf), len(TickColumns()))
        }
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd packages && go test ./collector/`
Expected: FAIL — `undefined: Phase`, `undefined: PlayerTick`

- [ ] **Step 4: Create phase.go**

```go
package collector

// Phase identifies which part of a round a tick belongs to.
//
// Note that buy-ability is deliberately NOT a phase: the buy window
// (mp_buytime) spans all of freezetime and the first few seconds of live play,
// so it cuts across PhaseFreeze and PhaseLive. It is derivable from
// GameState().Rules().ConVars() if ever needed.
type Phase uint8

const (
    // PhaseFreeze is freezetime: players are frozen at spawn and can buy.
    PhaseFreeze Phase = iota
    // PhaseLive is live play, from RoundFreezetimeEnd to RoundEnd.
    PhaseLive
    // PhasePostRound is after the win condition, before RoundEndOfficial.
    // Players can still move and kill; this data is real but analytically noisy.
    PhasePostRound
)

func (p Phase) String() string {
    switch p {
    case PhaseFreeze:
        return "freeze"
    case PhaseLive:
        return "live"
    case PhasePostRound:
        return "postround"
    default:
        return "unknown"
    }
}
```

- [ ] **Step 5: Create tick.go**

The struct, the column list, and `AppendRow` live in one file deliberately: adding a field means editing three adjacent places, and `TestAppendRowMatchesColumns` fails loudly if you forget one.

```go
package collector

import (
    "strconv"

    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// PlayerTick is one player's complete state at one tick. This struct IS the
// schema: TickColumns and AppendRow below must stay aligned with it.
type PlayerTick struct {
    Round   int32
    Tick    int32 // ingame tick, not demo frame
    Phase   Phase
    SteamID uint64
    Team    common.Team // on the tick row because it changes at halftime

    X, Y, Z    float32 // world position, Hammer units
    Yaw, Pitch float32 // view angles, degrees

    // Velocity is derived by differencing positions - CS2 does not network it.
    // VelValid is false on a player's first sampled tick in a round and after
    // any gap (death, respawn), where there is no predecessor to difference against.
    VelX, VelY, VelZ float32
    Speed            float32 // XY magnitude, units/sec; Z excluded so jumps don't inflate it
    MaxSpeed         float32 // m_flMaxspeed - varies by weapon; Speed/MaxSpeed is comparable
    VelValid         bool

    Buttons uint64 // raw input bitmask - see common.ButtonBitMask

    ShotsFired           int16   // m_iShotsFired - spray index, resets between bursts
    PunchYaw, PunchPitch float32 // m_vecCsViewPunchAngle - recoil kick on the view

    IsDucking, IsWalking, IsAirborne, IsScoped bool

    Health, Armor  int16
    IsAlive        bool
    FlashRemaining float32 // seconds
    ActiveWeapon   string
    Place          string // LastPlaceName(), e.g. "Palace"
}

// TickColumns is the CSV header for ticks.csv.gz, in AppendRow's emit order.
func TickColumns() []string {
    return []string{
        "round", "tick", "phase", "steamid", "team",
        "x", "y", "z", "yaw", "pitch",
        "vel_x", "vel_y", "vel_z", "speed", "max_speed", "vel_valid",
        "buttons", "shots_fired", "punch_yaw", "punch_pitch",
        "is_ducking", "is_walking", "is_airborne", "is_scoped",
        "health", "armor", "is_alive", "flash_remaining",
        "active_weapon", "place",
    }
}

// AppendRow appends this tick's CSV fields to dst and returns the extended
// slice. Callers pass buf[:0] to reuse one allocation across millions of rows.
func (t *PlayerTick) AppendRow(dst []string) []string {
    return append(dst,
        i32(t.Round),
        i32(t.Tick),
        t.Phase.String(),
        strconv.FormatUint(t.SteamID, 10),
        strconv.Itoa(int(t.Team)),
        f32(t.X), f32(t.Y), f32(t.Z),
        f32(t.Yaw), f32(t.Pitch),
        f32(t.VelX), f32(t.VelY), f32(t.VelZ),
        f32(t.Speed), f32(t.MaxSpeed),
        b(t.VelValid),
        strconv.FormatUint(t.Buttons, 10),
        strconv.Itoa(int(t.ShotsFired)),
        f32(t.PunchYaw), f32(t.PunchPitch),
        b(t.IsDucking), b(t.IsWalking), b(t.IsAirborne), b(t.IsScoped),
        strconv.Itoa(int(t.Health)), strconv.Itoa(int(t.Armor)),
        b(t.IsAlive),
        f32(t.FlashRemaining),
        t.ActiveWeapon,
        t.Place,
    )
}

// Two decimals is sub-millimeter precision in Hammer units and saves ~30% of
// the file size versus full float32 precision.
func f32(v float32) string { return strconv.FormatFloat(float64(v), 'f', 2, 32) }
func i32(v int32) string   { return strconv.FormatInt(int64(v), 10) }

func b(v bool) string {
    if v {
        return "1"
    }
    return "0"
}
```

- [ ] **Step 6: Create round.go**

```go
package collector

import (
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Round is one fully materialized round: metadata plus every sampled tick for
// every player, in tick order. Peak memory is roughly 14 MB for a normal round.
type Round struct {
    Meta  RoundMeta
    Ticks []PlayerTick
}

type RoundMeta struct {
    Number          int32
    StartTick       int32
    FreezeEndTick   int32
    EndTick         int32 // RoundEnd - win condition met
    OfficialEndTick int32 // RoundEndOfficial - players frozen for next round

    Winner common.Team
    Reason events.RoundEndReason

    // TimeoutBefore records whether a tactical timeout preceded this round.
    // demoinfocs has no timeout support (see datatables.go:1279); these come
    // from raw properties on the game rules entity.
    TimeoutBefore bool
    TimeoutTeam   common.Team

    // Complete is false when the demo ended before RoundEndOfficial fired.
    // Consumers should treat incomplete rounds as truncated, not as losses.
    Complete bool

    Players []PlayerRound
}

// PlayerRound is one player's per-round summary, snapshotted at freezetime end
// so it reflects what they actually took into the round.
type PlayerRound struct {
    SteamID               uint64
    Team                  common.Team
    MoneyAtFreezeEnd      int32
    EquipValueAtFreezeEnd int32
    Survived              bool
}

// RoundColumns is the CSV header for rounds.csv.
func RoundColumns() []string {
    return []string{
        "number", "start_tick", "freeze_end_tick", "end_tick", "official_end_tick",
        "winner", "reason", "timeout_before", "timeout_team", "complete", "tick_rows",
    }
}

// RoundPlayerColumns is the CSV header for round_players.csv.
func RoundPlayerColumns() []string {
    return []string{
        "round", "steamid", "team",
        "money_at_freeze_end", "equip_value_at_freeze_end", "survived",
    }
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd packages && gofmt -w ./collector/ && go test ./collector/ -v`
Expected: PASS — `TestPhaseString`, `TestAppendRowMatchesColumns`, `TestAppendRowReusesBuffer`

- [ ] **Step 8: Commit**

```bash
git add packages/go.mod packages/collector/
git commit -m "feat(collector): add core types for round-collector

PlayerTick is the schema's single source of truth; TickColumns and
AppendRow are kept in lockstep by test. Also fixes the packages module
path, which was copy-pasted from the playground."
```

---

### Task 2: Round assembly state machine

The heart of the collector. Kept free of any demoinfocs parser dependency so it tests with plain Go — no mocks, no demo files.

**Files:**
- Create: `packages/collector/assembler.go`
- Test: `packages/collector/assembler_test.go`

**Interfaces:**
- Consumes: `Round`, `RoundMeta`, `PlayerRound`, `PlayerTick`, `Phase` from Task 1
- Produces: `newAssembler(emit func(*Round)) *assembler` and methods `roundStart(tick, number int32)`, `freezeEnd(tick int32, economy []PlayerRound)`, `roundEnd(tick int32, winner common.Team, reason events.RoundEndReason)`, `roundEndOfficial(tick int32)`, `finish()`, `phase() Phase`, `active() bool`, `appendTick(t PlayerTick)`

- [ ] **Step 1: Write the failing tests**

Create `packages/collector/assembler_test.go`:

```go
package collector

import (
    "testing"

    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// collect returns an assembler plus a pointer to the slice it emits into.
func collect() (*assembler, *[]*Round) {
    var got []*Round
    a := newAssembler(func(r *Round) { got = append(got, r) })
    return a, &got
}

func TestNormalRoundEmitsOnceComplete(t *testing.T) {
    a, got := collect()

    a.roundStart(1000, 1)
    if a.phase() != PhaseFreeze {
        t.Errorf("after roundStart phase = %v, want freeze", a.phase())
    }

    a.freezeEnd(2000, []PlayerRound{{SteamID: 7, MoneyAtFreezeEnd: 800}})
    if a.phase() != PhaseLive {
        t.Errorf("after freezeEnd phase = %v, want live", a.phase())
    }

    a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
    if a.phase() != PhasePostRound {
        t.Errorf("after roundEnd phase = %v, want postround", a.phase())
    }

    a.roundEndOfficial(3300)

    if len(*got) != 1 {
        t.Fatalf("emitted %d rounds, want 1", len(*got))
    }
    m := (*got)[0].Meta
    if !m.Complete {
        t.Error("Complete = false, want true")
    }
    if m.Number != 1 || m.StartTick != 1000 || m.FreezeEndTick != 2000 ||
        m.EndTick != 3000 || m.OfficialEndTick != 3300 {
        t.Errorf("boundary ticks wrong: %+v", m)
    }
    if m.Winner != common.TeamTerrorists {
        t.Errorf("Winner = %v, want T", m.Winner)
    }
    if len(m.Players) != 1 || m.Players[0].MoneyAtFreezeEnd != 800 {
        t.Errorf("economy snapshot wrong: %+v", m.Players)
    }
}

// mp_restartgame fires a second RoundStart with no intervening end. The partial
// round must be discarded, not emitted as a truncated round.
func TestRestartDiscardsPartialRound(t *testing.T) {
    a, got := collect()

    a.roundStart(1000, 1)
    a.freezeEnd(2000, nil)
    a.appendTick(PlayerTick{Tick: 2100})

    a.roundStart(5000, 1) // restart

    if len(*got) != 0 {
        t.Fatalf("emitted %d rounds on restart, want 0", len(*got))
    }

    a.freezeEnd(6000, nil)
    a.roundEnd(7000, common.TeamCounterTerrorists, events.RoundEndReasonCTWin)
    a.roundEndOfficial(7300)

    if len(*got) != 1 {
        t.Fatalf("emitted %d rounds, want 1", len(*got))
    }
    if (*got)[0].Meta.StartTick != 5000 {
        t.Errorf("StartTick = %d, want 5000 (the restarted round)",
            (*got)[0].Meta.StartTick)
    }
    if n := len((*got)[0].Ticks); n != 0 {
        t.Errorf("restarted round carried %d stale ticks, want 0", n)
    }
}

// Demos routinely cut off before RoundEndOfficial on the final round. That
// round is still worth having - it just must be marked incomplete.
func TestFinishFlushesPendingRoundAsIncomplete(t *testing.T) {
    a, got := collect()

    a.roundStart(1000, 12)
    a.freezeEnd(2000, nil)
    a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
    a.finish()

    if len(*got) != 1 {
        t.Fatalf("emitted %d rounds, want 1", len(*got))
    }
    if (*got)[0].Meta.Complete {
        t.Error("Complete = true, want false for a flushed partial round")
    }
}

func TestFinishWithNoPendingRoundEmitsNothing(t *testing.T) {
    a, got := collect()
    a.finish()
    if len(*got) != 0 {
        t.Fatalf("emitted %d rounds, want 0", len(*got))
    }
}

// Stray events before the first RoundStart must not panic or emit.
func TestEventsBeforeRoundStartAreIgnored(t *testing.T) {
    a, got := collect()

    a.freezeEnd(100, nil)
    a.roundEnd(200, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
    a.roundEndOfficial(300)
    a.appendTick(PlayerTick{Tick: 400})

    if len(*got) != 0 {
        t.Fatalf("emitted %d rounds, want 0", len(*got))
    }
    if a.active() {
        t.Error("active() = true with no round open")
    }
}

func TestAppendTickOnlyWhenRoundOpen(t *testing.T) {
    a, got := collect()

    a.appendTick(PlayerTick{Tick: 1}) // dropped - no round open
    a.roundStart(1000, 1)
    a.appendTick(PlayerTick{Tick: 1001})
    a.appendTick(PlayerTick{Tick: 1002})
    a.roundEnd(2000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
    a.roundEndOfficial(2300)

    if n := len((*got)[0].Ticks); n != 2 {
        t.Errorf("round holds %d ticks, want 2", n)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd packages && go test ./collector/ -run TestNormalRound -v`
Expected: FAIL — `undefined: newAssembler`

- [ ] **Step 3: Create assembler.go**

```go
package collector

import (
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// assembler builds Rounds from an ordered stream of round-lifecycle calls.
//
// It deliberately knows nothing about demoinfocs parsers or entities: Collector
// translates parser events into these calls. That keeps every tricky ordering
// rule - restarts, missing official ends, stray events - testable without mocks.
type assembler struct {
    cur  *Round
    emit func(*Round)

    // phaseNow tracks progression within the open round. It is not derived
    // from tick numbers because RoundEnd can arrive at any tick.
    phaseNow Phase
}

func newAssembler(emit func(*Round)) *assembler {
    return &assembler{emit: emit}
}

func (a *assembler) active() bool { return a.cur != nil }

func (a *assembler) phase() Phase { return a.phaseNow }

// roundStart opens a new round. If a round is already open it was never
// officially ended - a restart - so the partial round is discarded.
func (a *assembler) roundStart(tick, number int32) {
    a.cur = &Round{Meta: RoundMeta{Number: number, StartTick: tick}}
    a.phaseNow = PhaseFreeze
}

func (a *assembler) freezeEnd(tick int32, economy []PlayerRound) {
    if a.cur == nil {
        return
    }
    a.cur.Meta.FreezeEndTick = tick
    a.cur.Meta.Players = economy
    a.phaseNow = PhaseLive
}

func (a *assembler) roundEnd(tick int32, winner common.Team, reason events.RoundEndReason) {
    if a.cur == nil {
        return
    }
    // Guard against a duplicate RoundEnd overwriting the real one.
    if a.phaseNow == PhasePostRound {
        return
    }
    a.cur.Meta.EndTick = tick
    a.cur.Meta.Winner = winner
    a.cur.Meta.Reason = reason
    a.phaseNow = PhasePostRound
}

func (a *assembler) roundEndOfficial(tick int32) {
    if a.cur == nil {
        return
    }
    a.cur.Meta.OfficialEndTick = tick
    a.cur.Meta.Complete = true
    a.flush()
}

// finish emits any round still open when parsing ends, marked incomplete.
func (a *assembler) finish() {
    if a.cur == nil {
        return
    }
    a.cur.Meta.Complete = false
    a.flush()
}

func (a *assembler) flush() {
    r := a.cur
    a.cur = nil // release before emitting so the sink owns the memory
    a.emit(r)
}

func (a *assembler) appendTick(t PlayerTick) {
    if a.cur == nil {
        return
    }
    a.cur.Ticks = append(a.cur.Ticks, t)
}

func (a *assembler) setTimeout(team common.Team) {
    if a.cur == nil {
        return
    }
    a.cur.Meta.TimeoutBefore = true
    a.cur.Meta.TimeoutTeam = team
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd packages && gofmt -w ./collector/ && go test ./collector/ -v`
Expected: PASS — all six assembler tests plus the three from Task 1

(`events.RoundEndReasonCTWin` = 8 and `events.RoundEndReasonTerroristsWin` = 9 are verified to exist at `events/events.go:62-63`.)

- [ ] **Step 5: Commit**

```bash
git add packages/collector/assembler.go packages/collector/assembler_test.go
git commit -m "feat(collector): add round assembly state machine

Pure state machine with no parser dependency, so the tricky ordering
rules (restarts, missing RoundEndOfficial, stray pre-match events) are
testable with plain Go and no mocks."
```

---

### Task 3: Velocity derivation

Separated from tick sampling because the math is worth testing on its own and has nothing to do with parsing.

**Files:**
- Create: `packages/collector/velocity.go`
- Test: `packages/collector/velocity_test.go`

**Interfaces:**
- Consumes: nothing from prior tasks
- Produces: `newVelocityTracker(tickRate float64) *velocityTracker` with methods `compute(steamID uint64, tick int32, x, y, z float32) (vx, vy, vz, speed float32, valid bool)`, `reset()`, `forget(steamID uint64)`

- [ ] **Step 1: Write the failing tests**

Create `packages/collector/velocity_test.go`:

```go
package collector

import (
    "math"
    "testing"
)

func TestFirstSampleIsInvalid(t *testing.T) {
    v := newVelocityTracker(64)
    _, _, _, _, valid := v.compute(7, 100, 0, 0, 0)
    if valid {
        t.Error("valid = true on first sample, want false (no predecessor)")
    }
}

func TestVelocityOverOneTick(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    // 10 units in 1 tick at 64 tick/s -> 640 units/sec
    vx, vy, vz, speed, valid := v.compute(7, 101, 10, 0, 0)
    if !valid {
        t.Fatal("valid = false on second sample, want true")
    }
    if math.Abs(float64(vx)-640) > 0.01 {
        t.Errorf("vx = %v, want 640", vx)
    }
    if vy != 0 || vz != 0 {
        t.Errorf("vy, vz = %v, %v, want 0, 0", vy, vz)
    }
    if math.Abs(float64(speed)-640) > 0.01 {
        t.Errorf("speed = %v, want 640", speed)
    }
}

// Speed must exclude Z, or a player jumping straight up reads as "moving fast"
// and any counterstrafe check based on it breaks.
func TestSpeedExcludesVertical(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    _, _, vz, speed, _ := v.compute(7, 101, 0, 0, 5)
    if speed != 0 {
        t.Errorf("speed = %v for purely vertical movement, want 0", speed)
    }
    if math.Abs(float64(vz)-320) > 0.01 {
        t.Errorf("vz = %v, want 320", vz)
    }
}

func TestMultiTickGapScalesCorrectly(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    // 20 units over 2 ticks = same 640 units/sec as 10 units over 1 tick
    vx, _, _, _, _ := v.compute(7, 102, 20, 0, 0)
    if math.Abs(float64(vx)-640) > 0.01 {
        t.Errorf("vx = %v, want 640", vx)
    }
}

func TestPlayersTrackedIndependently(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    v.compute(8, 100, 500, 500, 0)
    vx, _, _, _, valid := v.compute(7, 101, 10, 0, 0)
    if !valid || math.Abs(float64(vx)-640) > 0.01 {
        t.Errorf("player 7 vx = %v (valid=%v), want 640, true", vx, valid)
    }
}

func TestResetInvalidatesEveryone(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    v.reset()
    _, _, _, _, valid := v.compute(7, 101, 10, 0, 0)
    if valid {
        t.Error("valid = true after reset, want false")
    }
}

func TestForgetInvalidatesOnePlayer(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    v.compute(8, 100, 0, 0, 0)
    v.forget(7)

    if _, _, _, _, valid := v.compute(7, 101, 10, 0, 0); valid {
        t.Error("player 7 valid = true after forget, want false")
    }
    if _, _, _, _, valid := v.compute(8, 101, 10, 0, 0); !valid {
        t.Error("player 8 valid = false, want true (forget must not affect others)")
    }
}

// A repeated tick would divide by zero.
func TestSameTickIsInvalid(t *testing.T) {
    v := newVelocityTracker(64)
    v.compute(7, 100, 0, 0, 0)
    _, _, _, _, valid := v.compute(7, 100, 10, 0, 0)
    if valid {
        t.Error("valid = true for a zero-duration sample, want false")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd packages && go test ./collector/ -run TestVelocity -v`
Expected: FAIL — `undefined: newVelocityTracker`

- [ ] **Step 3: Create velocity.go**

```go
package collector

import "math"

// velocityTracker derives player velocity by differencing positions between
// ticks. This is not a fallback: CS2 does not network current player velocity
// (it is client-side predicted), so 324 properties on CCSPlayerPawn contain no
// velocity vector. Differencing is the only source.
type velocityTracker struct {
    tickRate float64
    last     map[uint64]sample
}

type sample struct {
    tick    int32
    x, y, z float32
}

func newVelocityTracker(tickRate float64) *velocityTracker {
    if tickRate <= 0 {
        tickRate = 64 // demos with a corrupt header report -1
    }
    return &velocityTracker{tickRate: tickRate, last: make(map[uint64]sample, 16)}
}

// compute returns velocity in units/sec. valid is false when there is no usable
// predecessor - a player's first tick in a round, or after a death gap - so
// consumers can filter rather than trusting a bogus zero.
func (v *velocityTracker) compute(steamID uint64, tick int32, x, y, z float32) (vx, vy, vz, speed float32, valid bool) {
    prev, ok := v.last[steamID]
    v.last[steamID] = sample{tick: tick, x: x, y: y, z: z}

    if !ok || tick <= prev.tick {
        return 0, 0, 0, 0, false
    }

    dt := float32(float64(tick-prev.tick) / v.tickRate)
    vx = (x - prev.x) / dt
    vy = (y - prev.y) / dt
    vz = (z - prev.z) / dt

    // XY only: a jump is vertical, and counting it would make a stationary
    // jumping player look like they are moving.
    speed = float32(math.Hypot(float64(vx), float64(vy)))

    return vx, vy, vz, speed, true
}

// reset drops all history. Called at round boundaries, where players teleport
// to spawn and any differenced velocity would be meaningless.
func (v *velocityTracker) reset() {
    clear(v.last)
}

// forget drops one player's history, used when they die so the respawn does not
// produce a teleport-sized velocity.
func (v *velocityTracker) forget(steamID uint64) {
    delete(v.last, steamID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd packages && gofmt -w ./collector/ && go test ./collector/ -v`
Expected: PASS — all eight velocity tests plus everything prior

- [ ] **Step 5: Commit**

```bash
git add packages/collector/velocity.go packages/collector/velocity_test.go
git commit -m "feat(collector): derive velocity by position differencing

CS2 does not network player velocity, so differencing is the only
source. Speed excludes Z so jumping does not read as movement."
```

---

### Task 4: CSV sink

**Files:**
- Create: `packages/collector/sink.go`
- Create: `packages/collector/csvsink/csvsink.go`
- Test: `packages/collector/csvsink/csvsink_test.go`

**Interfaces:**
- Consumes: `Round`, `PlayerTick`, `TickColumns()`, `RoundColumns()`, `RoundPlayerColumns()`, `(*PlayerTick).AppendRow`
- Produces: `collector.Sink` interface (`Round(*Round) error`, `Close() error`); `csvsink.New(dir string, meta Meta) (*Sink, error)`; `csvsink.Meta` struct with fields `DemoFile, Map string`, `TickRate float64`

- [ ] **Step 1: Create the Sink interface**

`packages/collector/sink.go`:

```go
package collector

// Sink consumes completed rounds. Implementations must tolerate being called
// once per round in order, and must be closed to flush buffered output.
//
// This interface is the seam that keeps the output format reversible: csvsink
// writes gzipped CSV today, and a Parquet implementation later is a new package
// rather than a rewrite.
type Sink interface {
    Round(*Round) error
    Close() error
}
```

- [ ] **Step 2: Write the failing test**

Create `packages/collector/csvsink/csvsink_test.go`:

```go
package csvsink

import (
    "compress/gzip"
    "encoding/csv"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

func testRound() *collector.Round {
    return &collector.Round{
        Meta: collector.RoundMeta{
            Number: 1, StartTick: 1000, FreezeEndTick: 2000,
            EndTick: 3000, OfficialEndTick: 3300, Complete: true,
            Players: []collector.PlayerRound{
                {SteamID: 7, MoneyAtFreezeEnd: 800, EquipValueAtFreezeEnd: 1000, Survived: true},
            },
        },
        Ticks: []collector.PlayerTick{
            {Round: 1, Tick: 2001, SteamID: 7, X: 100, Y: 200, Z: 50, Speed: 240, IsAlive: true},
            {Round: 1, Tick: 2002, SteamID: 7, X: 110, Y: 200, Z: 50, Speed: 250, IsAlive: true},
        },
    }
}

func readGzCSV(t *testing.T, path string) [][]string {
    t.Helper()
    f, err := os.Open(path)
    if err != nil {
        t.Fatalf("open %s: %v", path, err)
    }
    defer f.Close()
    gz, err := gzip.NewReader(f)
    if err != nil {
        t.Fatalf("gzip %s: %v", path, err)
    }
    defer gz.Close()
    rows, err := csv.NewReader(gz).ReadAll()
    if err != nil {
        t.Fatalf("csv %s: %v", path, err)
    }
    return rows
}

func TestWritesAllFourFiles(t *testing.T) {
    dir := t.TempDir()
    s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    if err := s.Round(testRound()); err != nil {
        t.Fatalf("Round: %v", err)
    }
    if err := s.Close(); err != nil {
        t.Fatalf("Close: %v", err)
    }

    for _, name := range []string{"manifest.json", "rounds.csv", "round_players.csv", "ticks.csv.gz"} {
        if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
            t.Errorf("missing output file %s: %v", name, err)
        }
    }
}

func TestTicksFileHasHeaderAndRows(t *testing.T) {
    dir := t.TempDir()
    s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
    s.Round(testRound())
    s.Close()

    rows := readGzCSV(t, filepath.Join(dir, "ticks.csv.gz"))
    if len(rows) != 3 { // header + 2 ticks
        t.Fatalf("ticks.csv.gz has %d rows, want 3", len(rows))
    }
    want := collector.TickColumns()
    if len(rows[0]) != len(want) {
        t.Fatalf("header has %d columns, want %d", len(rows[0]), len(want))
    }
    for i := range want {
        if rows[0][i] != want[i] {
            t.Errorf("column %d = %q, want %q", i, rows[0][i], want[i])
        }
    }
}

func TestManifestRecordsCounts(t *testing.T) {
    dir := t.TempDir()
    s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
    s.Round(testRound())
    s.Close()

    raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
    if err != nil {
        t.Fatalf("read manifest: %v", err)
    }
    var m manifest
    if err := json.Unmarshal(raw, &m); err != nil {
        t.Fatalf("unmarshal manifest: %v", err)
    }

    if m.Rounds != 1 {
        t.Errorf("Rounds = %d, want 1", m.Rounds)
    }
    if m.TickRows != 2 {
        t.Errorf("TickRows = %d, want 2", m.TickRows)
    }
    if m.VelocitySource != "position_diff" {
        t.Errorf("VelocitySource = %q, want position_diff", m.VelocitySource)
    }
    if !m.Complete {
        t.Error("Complete = false, want true")
    }
    if m.Map != "de_mirage" {
        t.Errorf("Map = %q, want de_mirage", m.Map)
    }
}

// An incomplete round must poison the manifest's Complete flag, so a truncated
// dump is never mistaken for a whole match.
func TestIncompleteRoundMarksManifestIncomplete(t *testing.T) {
    dir := t.TempDir()
    s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
    r := testRound()
    r.Meta.Complete = false
    s.Round(r)
    s.Close()

    raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
    var m manifest
    json.Unmarshal(raw, &m)
    if m.Complete {
        t.Error("manifest Complete = true, want false when a round is incomplete")
    }
}

func TestRoundPlayersRowsWritten(t *testing.T) {
    dir := t.TempDir()
    s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
    s.Round(testRound())
    s.Close()

    f, _ := os.Open(filepath.Join(dir, "round_players.csv"))
    defer f.Close()
    rows, _ := csv.NewReader(f).ReadAll()
    if len(rows) != 2 { // header + 1 player
        t.Fatalf("round_players.csv has %d rows, want 2", len(rows))
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd packages && go test ./collector/csvsink/`
Expected: FAIL — `undefined: New`

- [ ] **Step 4: Create csvsink.go**

```go
// Package csvsink writes collector Rounds to gzipped CSV plus a JSON manifest.
//
// Layout:
//
//	<dir>/manifest.json        schema version, map, tickrate, column lists, counts
//	<dir>/players.csv          one row per steamid, with their last-seen name
//	<dir>/rounds.csv           one row per round
//	<dir>/round_players.csv    one row per (round, player)
//	<dir>/ticks.csv.gz         one row per (round, tick, player)
package csvsink

import (
    "compress/gzip"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strconv"

    "github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// schemaVersion follows the rule in the design doc: appending a trailing column
// bumps the minor part and stays readable by header-aware consumers; removing
// or reordering columns bumps the major part.
const schemaVersion = "1.0"

// Meta is the run-level information the manifest records.
type Meta struct {
    DemoFile string
    Map      string
    TickRate float64
}

type manifest struct {
    SchemaVersion  string              `json:"schema_version"`
    DemoFile       string              `json:"demo_file"`
    Map            string              `json:"map"`
    TickRate       float64             `json:"tick_rate"`
    Rounds         int                 `json:"rounds"`
    TickRows       int64               `json:"tick_rows"`
    Complete       bool                `json:"complete"`
    VelocitySource string              `json:"velocity_source"`
    Columns        map[string][]string `json:"columns"`
}

// Sink implements collector.Sink.
type Sink struct {
    dir  string
    meta Meta

    ticksFile *os.File
    gz        *gzip.Writer
    ticks     *csv.Writer

    roundsFile   *os.File
    rounds       *csv.Writer
    rpFile       *os.File
    roundPlayers *csv.Writer

    buf []string // reused across every tick row to avoid millions of allocations

    nRounds  int
    nTicks   int64
    complete bool
}

var _ collector.Sink = (*Sink)(nil)

func New(dir string, meta Meta) (*Sink, error) {
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, fmt.Errorf("create output dir: %w", err)
    }

    s := &Sink{
        dir:      dir,
        meta:     meta,
        buf:      make([]string, 0, len(collector.TickColumns())),
        complete: true,
    }

    var err error
    if s.ticksFile, err = os.Create(filepath.Join(dir, "ticks.csv.gz")); err != nil {
        return nil, fmt.Errorf("create ticks.csv.gz: %w", err)
    }
    s.gz = gzip.NewWriter(s.ticksFile)
    s.ticks = csv.NewWriter(s.gz)
    if err := s.ticks.Write(collector.TickColumns()); err != nil {
        return nil, fmt.Errorf("write ticks header: %w", err)
    }

    if s.roundsFile, err = os.Create(filepath.Join(dir, "rounds.csv")); err != nil {
        return nil, fmt.Errorf("create rounds.csv: %w", err)
    }
    s.rounds = csv.NewWriter(s.roundsFile)
    if err := s.rounds.Write(collector.RoundColumns()); err != nil {
        return nil, fmt.Errorf("write rounds header: %w", err)
    }

    if s.rpFile, err = os.Create(filepath.Join(dir, "round_players.csv")); err != nil {
        return nil, fmt.Errorf("create round_players.csv: %w", err)
    }
    s.roundPlayers = csv.NewWriter(s.rpFile)
    if err := s.roundPlayers.Write(collector.RoundPlayerColumns()); err != nil {
        return nil, fmt.Errorf("write round_players header: %w", err)
    }

    return s, nil
}

func (s *Sink) Round(r *collector.Round) error {
    m := r.Meta

    if err := s.rounds.Write([]string{
        strconv.Itoa(int(m.Number)),
        strconv.Itoa(int(m.StartTick)),
        strconv.Itoa(int(m.FreezeEndTick)),
        strconv.Itoa(int(m.EndTick)),
        strconv.Itoa(int(m.OfficialEndTick)),
        strconv.Itoa(int(m.Winner)),
        strconv.Itoa(int(m.Reason)),
        boolStr(m.TimeoutBefore),
        strconv.Itoa(int(m.TimeoutTeam)),
        boolStr(m.Complete),
        strconv.Itoa(len(r.Ticks)),
    }); err != nil {
        return fmt.Errorf("write round %d: %w", m.Number, err)
    }

    for _, p := range m.Players {
        if err := s.roundPlayers.Write([]string{
            strconv.Itoa(int(m.Number)),
            strconv.FormatUint(p.SteamID, 10),
            strconv.Itoa(int(p.Team)),
            strconv.Itoa(int(p.MoneyAtFreezeEnd)),
            strconv.Itoa(int(p.EquipValueAtFreezeEnd)),
            boolStr(p.Survived),
        }); err != nil {
            return fmt.Errorf("write round_player r%d p%d: %w", m.Number, p.SteamID, err)
        }
    }

    for i := range r.Ticks {
        s.buf = r.Ticks[i].AppendRow(s.buf[:0])
        if err := s.ticks.Write(s.buf); err != nil {
            return fmt.Errorf("write tick r%d: %w", m.Number, err)
        }
    }

    s.nRounds++
    s.nTicks += int64(len(r.Ticks))
    if !m.Complete {
        s.complete = false
    }
    return nil
}

// Close flushes every writer and writes the manifest. The manifest is written
// last because it records counts that are only final once all rounds are in.
func (s *Sink) Close() error {
    s.ticks.Flush()
    if err := s.ticks.Error(); err != nil {
        return fmt.Errorf("flush ticks: %w", err)
    }
    if err := s.gz.Close(); err != nil {
        return fmt.Errorf("close gzip: %w", err)
    }
    if err := s.ticksFile.Close(); err != nil {
        return fmt.Errorf("close ticks file: %w", err)
    }

    s.rounds.Flush()
    if err := s.rounds.Error(); err != nil {
        return fmt.Errorf("flush rounds: %w", err)
    }
    if err := s.roundsFile.Close(); err != nil {
        return fmt.Errorf("close rounds file: %w", err)
    }

    s.roundPlayers.Flush()
    if err := s.roundPlayers.Error(); err != nil {
        return fmt.Errorf("flush round_players: %w", err)
    }
    if err := s.rpFile.Close(); err != nil {
        return fmt.Errorf("close round_players file: %w", err)
    }

    return s.writeManifest()
}

func (s *Sink) writeManifest() error {
    m := manifest{
        SchemaVersion:  schemaVersion,
        DemoFile:       s.meta.DemoFile,
        Map:            s.meta.Map,
        TickRate:       s.meta.TickRate,
        Rounds:         s.nRounds,
        TickRows:       s.nTicks,
        Complete:       s.complete,
        VelocitySource: "position_diff",
        Columns: map[string][]string{
            "ticks":         collector.TickColumns(),
            "rounds":        collector.RoundColumns(),
            "round_players": collector.RoundPlayerColumns(),
        },
    }

    raw, err := json.MarshalIndent(m, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal manifest: %w", err)
    }
    if err := os.WriteFile(filepath.Join(s.dir, "manifest.json"), raw, 0o644); err != nil {
        return fmt.Errorf("write manifest: %w", err)
    }
    return nil
}

func boolStr(v bool) string {
    if v {
        return "1"
    }
    return "0"
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd packages && gofmt -w ./collector/ && go test ./collector/... -v`
Expected: PASS — all six csvsink tests plus everything prior

Note: `players.csv` is documented in the package comment but not yet written — it needs names, which only arrive with the parser in Task 5. Task 5 adds it.

- [ ] **Step 6: Commit**

```bash
git add packages/collector/sink.go packages/collector/csvsink/
git commit -m "feat(collector): add gzipped CSV sink with JSON manifest

Sink interface keeps the output format reversible. The manifest records
counts and a Complete flag so a truncated dump is never mistaken for a
whole match."
```

---

### Task 5: Collector adapter and CLI

Wires demoinfocs to the assembler, samples ticks, and exposes it as a command.

**Files:**
- Create: `packages/collector/collector.go`
- Modify: `packages/collector/csvsink/csvsink.go` (add `Players` method for players.csv)
- Modify: `packages/round-collector/main.go` (currently only `package main`)

**Interfaces:**
- Consumes: everything from Tasks 1-4
- Produces: `collector.New(p demoinfocs.Parser, sink Sink) *Collector`; `(*Collector).Run() error`; `(*Collector).SetMaxRounds(n int)`; `(*Collector).OnRound(func(*Round))`; `(*Collector).Stats() (rounds int, ticks int64)`; `csvsink.(*Sink).Players(map[uint64]string) error`

- [ ] **Step 1: Add players.csv and SetMap to csvsink**

Append to `packages/collector/csvsink/csvsink.go`:

```go
// SetMap records the map name. The name arrives from a net-message during
// parsing, after the sink is constructed, so it cannot be passed to New. Safe
// to call any time before Close, which is when the manifest is written.
func (s *Sink) SetMap(name string) { s.meta.Map = name }
```

and:

```go
// Players writes players.csv: one row per steamid, holding their last-seen
// name. Names live here rather than on tick rows because repeating them across
// millions of rows is waste.
//
// Note this collapses mid-match name changes to the final name. The design doc
// specifies a full (steamid, name, first_seen_tick) observation table; that is
// deliberately deferred until a demo with a rename shows it matters.
func (s *Sink) Players(names map[uint64]string) error {
    f, err := os.Create(filepath.Join(s.dir, "players.csv"))
    if err != nil {
        return fmt.Errorf("create players.csv: %w", err)
    }
    defer f.Close()

    w := csv.NewWriter(f)
    if err := w.Write([]string{"steamid", "name"}); err != nil {
        return fmt.Errorf("write players header: %w", err)
    }
    for id, name := range names {
        if err := w.Write([]string{strconv.FormatUint(id, 10), name}); err != nil {
            return fmt.Errorf("write player %d: %w", id, err)
        }
    }
    w.Flush()
    return w.Error()
}
```

- [ ] **Step 2: Create collector.go**

```go
package collector

import (
    "fmt"
    "io"

    demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Collector translates demoinfocs events into assembler calls and samples
// per-tick player state. It is the imperative shell around the pure core.
type Collector struct {
    parser demoinfocs.Parser
    sink   Sink
    asm    *assembler
    vel    *velocityTracker

    // matchStarted gates out warmup and the false-positive RoundStart at tick 0.
    // Strategy proven in playground/go/cmd/hello-demo.
    matchStarted bool

    names     map[uint64]string
    maxRounds int
    onRound   func(*Round)

    nRounds int
    nTicks  int64
    err     error // first sink error; aborts the parse
}

// New wires a Collector to a parser. Taking the demoinfocs.Parser interface
// rather than an io.Reader keeps this constructible from a fake in tests.
func New(p demoinfocs.Parser, sink Sink) *Collector {
    c := &Collector{
        parser: p,
        sink:   sink,
        vel:    newVelocityTracker(p.TickRate()),
        names:  make(map[uint64]string, 16),
    }
    c.asm = newAssembler(c.emitRound)
    c.register()
    return c
}

// NewFromReader is the convenience path for callers that just have a file.
func NewFromReader(r io.Reader, sink Sink) *Collector {
    return New(demoinfocs.NewParser(r), sink)
}

// SetMaxRounds stops parsing after n complete rounds. Zero means no limit.
// A development affordance: iterating on a 667MB demo takes seconds, not minutes.
func (c *Collector) SetMaxRounds(n int) { c.maxRounds = n }

// OnRound registers an extra consumer, called before the sink writes. Future
// analyzers (counterstrafe, spray) hook in here without touching this file.
func (c *Collector) OnRound(f func(*Round)) { c.onRound = f }

func (c *Collector) Stats() (int, int64) { return c.nRounds, c.nTicks }

// Names returns the observed steamid -> name map, for players.csv.
func (c *Collector) Names() map[uint64]string { return c.names }

func (c *Collector) Run() error {
    err := c.parser.ParseToEnd()

    // A demo that cuts off mid-stream is a partial success: flush what we have.
    if err != nil && err != demoinfocs.ErrCancelled {
        c.asm.finish()
        if c.err != nil {
            return c.err
        }
        return fmt.Errorf("parse: %w", err)
    }

    c.asm.finish()
    return c.err
}

func (c *Collector) emitRound(r *Round) {
    if c.err != nil {
        return
    }
    if c.onRound != nil {
        c.onRound(r)
    }
    if err := c.sink.Round(r); err != nil {
        c.err = err
        c.parser.Cancel()
        return
    }
    c.nRounds++
    c.nTicks += int64(len(r.Ticks))

    if c.maxRounds > 0 && c.nRounds >= c.maxRounds {
        c.parser.Cancel()
    }
}

func (c *Collector) live() bool {
    return c.matchStarted && !c.parser.GameState().IsWarmupPeriod()
}

func (c *Collector) tick() int32 {
    return int32(c.parser.GameState().IngameTick())
}

func (c *Collector) register() {
    c.parser.RegisterEventHandler(func(events.AnnouncementMatchStarted) {
        c.matchStarted = true
    })

    c.parser.RegisterEventHandler(func(events.RoundStart) {
        if !c.live() {
            return
        }
        c.asm.roundStart(c.tick(), int32(c.parser.GameState().TotalRoundsPlayed()+1))
        c.vel.reset() // players teleport to spawn; prior positions are meaningless
        c.readTimeout()
    })

    c.parser.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
        c.asm.freezeEnd(c.tick(), c.snapshotEconomy())
    })

    c.parser.RegisterEventHandler(func(e events.RoundEnd) {
        c.asm.roundEnd(c.tick(), e.Winner, e.Reason)
        c.markSurvivors()
    })

    c.parser.RegisterEventHandler(func(events.RoundEndOfficial) {
        c.asm.roundEndOfficial(c.tick())
    })

    // Death leaves a gap; without forgetting, the respawn differences into a
    // teleport-sized velocity spike.
    c.parser.RegisterEventHandler(func(e events.Kill) {
        if e.Victim != nil {
            c.vel.forget(e.Victim.SteamID64)
        }
    })

    c.parser.RegisterEventHandler(func(events.FrameDone) {
        c.sampleFrame()
    })
}

func (c *Collector) sampleFrame() {
    if !c.asm.active() {
        return
    }
    tick := c.tick()
    phase := c.asm.phase()
    round := c.asm.cur.Meta.Number

    for _, p := range c.parser.GameState().Participants().Playing() {
        if p == nil {
            continue
        }
        if p.Name != "" {
            c.names[p.SteamID64] = p.Name
        }

        pos := p.Position()
        x, y, z := float32(pos.X), float32(pos.Y), float32(pos.Z)

        vx, vy, vz, speed, valid := float32(0), float32(0), float32(0), float32(0), false
        if p.IsAlive() {
            vx, vy, vz, speed, valid = c.vel.compute(p.SteamID64, tick, x, y, z)
        } else {
            c.vel.forget(p.SteamID64)
        }

        t := PlayerTick{
            Round: round, Tick: tick, Phase: phase,
            SteamID: p.SteamID64, Team: p.Team,
            X: x, Y: y, Z: z,
            Yaw: p.ViewDirectionX(), Pitch: p.ViewDirectionY(),
            VelX: vx, VelY: vy, VelZ: vz, Speed: speed, VelValid: valid,
            IsDucking:  p.IsDucking(),
            IsWalking:  p.IsWalking(),
            IsAirborne: p.IsAirborne(),
            IsScoped:   p.IsScoped(),
            Health:     int16(p.Health()),
            Armor:      int16(p.Armor()),
            IsAlive:    p.IsAlive(),
            Place:      p.LastPlaceName(),
        }

        if fd := p.FlashDurationTimeRemaining(); fd > 0 {
            t.FlashRemaining = float32(fd.Seconds())
        }
        if w := p.ActiveWeapon(); w != nil {
            t.ActiveWeapon = w.String()
        }

        c.readPawnProps(p, &t)
        c.asm.appendTick(t)
    }
}

// readPawnProps pulls the three properties demoinfocs does not wrap. Missing
// properties degrade to zero rather than failing the run - a demo from a
// different CS2 build may not carry all of them.
func (c *Collector) readPawnProps(p *common.Player, t *PlayerTick) {
    ent := p.PlayerPawnEntity()
    if ent == nil {
        return
    }
    if v, ok := ent.PropertyValue("m_iShotsFired"); ok {
        t.ShotsFired = int16(v.Int())
    }
    if v, ok := ent.PropertyValue("m_pMovementServices.m_flMaxspeed"); ok {
        t.MaxSpeed = v.Float()
    }
    if v, ok := ent.PropertyValue("m_pCameraServices.m_vecCsViewPunchAngle"); ok {
        punch := v.R3Vec()
        t.PunchPitch = float32(punch.X)
        t.PunchYaw = float32(punch.Y)
    }
}

func (c *Collector) snapshotEconomy() []PlayerRound {
    players := c.parser.GameState().Participants().Playing()
    out := make([]PlayerRound, 0, len(players))
    for _, p := range players {
        if p == nil {
            continue
        }
        out = append(out, PlayerRound{
            SteamID:               p.SteamID64,
            Team:                  p.Team,
            MoneyAtFreezeEnd:      int32(p.Money()),
            EquipValueAtFreezeEnd: int32(p.EquipmentValueFreezeTimeEnd()),
        })
    }
    return out
}

func (c *Collector) markSurvivors() {
    if !c.asm.active() {
        return
    }
    alive := make(map[uint64]bool, 10)
    for _, p := range c.parser.GameState().Participants().Playing() {
        if p != nil && p.IsAlive() {
            alive[p.SteamID64] = true
        }
    }
    players := c.asm.cur.Meta.Players
    for i := range players {
        players[i].Survived = alive[players[i].SteamID]
    }
}

// readTimeout reads timeout state from the game rules entity. demoinfocs has no
// timeout support (see datatables.go:1279 "TODO: timeout data"), but the raw
// properties are present.
func (c *Collector) readTimeout() {
    rules := c.parser.GameState().Rules()
    if rules == nil {
        return
    }
    ent := rules.Entity()
    if ent == nil {
        return
    }
    if v, ok := ent.PropertyValue("m_bTerroristTimeOutActive"); ok && v.BoolVal() {
        c.asm.setTimeout(common.TeamTerrorists)
        return
    }
    if v, ok := ent.PropertyValue("m_bCTTimeOutActive"); ok && v.BoolVal() {
        c.asm.setTimeout(common.TeamCounterTerrorists)
    }
}
```

- [ ] **Step 3: Build to verify it compiles**

Run: `cd packages && gofmt -w ./collector/ && go build ./...`
Expected: no output (success)

(`demoinfocs.ErrCancelled` is verified to exist at `parsing.go:28`. `SetMaxRounds` cancels the parse deliberately, so that error means success, not failure.)

- [ ] **Step 4: Write the CLI**

Replace `packages/round-collector/main.go` entirely:

```go
// round-collector dumps a CS2 demo to per-round CSV: one row per
// (round, tick, player) plus round metadata and economy.
//
// Usage:
//
//	round-collector -demo <path> [-out <dir>] [-max-rounds N] [-quiet]
//
// Output lands in <out>/<demo-basename>/ as manifest.json, players.csv,
// rounds.csv, round_players.csv and ticks.csv.gz. See
// docs/superpowers/specs/2026-08-05-round-collector-design.md.
package main

import (
    "flag"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
    "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

    "github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
    "github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
)

func main() {
    if err := run(); err != nil {
        fmt.Fprintf(os.Stderr, "round-collector: %v\n", err)
        os.Exit(1)
    }
}

func run() error {
    var (
        demoPath  = flag.String("demo", "", "path to the .dem file (required)")
        outDir    = flag.String("out", "out", "output directory")
        maxRounds = flag.Int("max-rounds", 0, "stop after N rounds (0 = all)")
        quiet     = flag.Bool("quiet", false, "suppress per-round progress")
    )
    flag.Parse()

    if *demoPath == "" {
        flag.Usage()
        return fmt.Errorf("-demo is required")
    }

    f, err := os.Open(*demoPath)
    if err != nil {
        return fmt.Errorf("open demo: %w", err)
    }
    defer f.Close()

    parser := demoinfocs.NewParser(f)
    defer parser.Close()

    base := strings.TrimSuffix(filepath.Base(*demoPath), filepath.Ext(*demoPath))
    dir := filepath.Join(*outDir, base)

    sink, err := csvsink.New(dir, csvsink.Meta{
        DemoFile: filepath.Base(*demoPath),
        TickRate: parser.TickRate(),
    })
    if err != nil {
        return err
    }

    // The map name arrives via a net-message before the first frame; v5 removed
    // the public ParseHeader (see playground/go/cmd/hello-demo for the details).
    // Registered after the sink exists so it can be handed straight through.
    var mapName string
    parser.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
        mapName = m.GetMapName()
        sink.SetMap(mapName)
    })

    c := collector.New(parser, sink)
    c.SetMaxRounds(*maxRounds)

    start := time.Now()
    if !*quiet {
        c.OnRound(func(r *collector.Round) {
            fmt.Printf("round %2d  ticks %6d  %s\n",
                r.Meta.Number, len(r.Ticks), completeness(r))
        })
    }

    runErr := c.Run()

    if err := sink.Players(c.Names()); err != nil {
        return err
    }
    if err := sink.Close(); err != nil {
        return err
    }
    if runErr != nil {
        return runErr
    }

    rounds, ticks := c.Stats()
    fmt.Printf("\n%d rounds, %d tick rows, map %s, %.1fs -> %s\n",
        rounds, ticks, mapName, time.Since(start).Seconds(), dir)
    return nil
}

func completeness(r *collector.Round) string {
    if r.Meta.Complete {
        return ""
    }
    return "(incomplete)"
}
```

- [ ] **Step 5: Build and run the full test suite**

Run: `cd packages && gofmt -w ./... && go build ./... && go test ./... -v`
Expected: build succeeds, all tests PASS

- [ ] **Step 6: Integration run against the real demo**

Run:
```bash
cd packages && go run ./round-collector -demo ../data/mega_ot_mirage.dem -max-rounds 3
```

Expected: three `round N ticks NNNNN` lines, then a summary. Verify:

```bash
ls -la out/mega_ot_mirage/
cat out/mega_ot_mirage/manifest.json
zcat out/mega_ot_mirage/ticks.csv.gz | head -3
zcat out/mega_ot_mirage/ticks.csv.gz | wc -l
```

Sanity checks — if any fails, stop and investigate rather than committing:
- `manifest.json` has `"rounds": 3` and a non-empty `"map"`
- the header row matches `TickColumns()`
- `speed` values are plausible (0 to ~260, not 6-digit numbers — a huge value means the velocity `dt` is wrong)
- `max_speed` is non-zero (if it is always 0, the property name is wrong)
- `shots_fired` is non-zero somewhere in a live round

- [ ] **Step 7: Full run and size check**

Run:
```bash
cd packages && time go run ./round-collector -demo ../data/mega_ot_mirage.dem -quiet
du -h out/mega_ot_mirage/
```

Expected: round count matching the match scoreboard, `ticks.csv.gz` in the 50-80 MB range predicted by the spec. A wildly different size means the sampling or phase logic is off.

- [ ] **Step 8: Commit**

```bash
git add packages/collector/collector.go packages/collector/csvsink/csvsink.go packages/round-collector/main.go
git commit -m "feat(round-collector): wire parser to assembler and add CLI

Collector translates demoinfocs events into assembler calls and samples
per-tick state, including the three raw pawn properties the public API
does not expose (m_iShotsFired, m_flMaxspeed, m_vecCsViewPunchAngle)."
```

- [ ] **Step 9: Add output to .gitignore**

Append to `.gitignore`:

```
# round-collector output
packages/out/
out/
```

```bash
git add .gitignore
git commit -m "chore: ignore round-collector output"
```

---

## Verification checklist

Run before considering this complete:

- [ ] `cd packages && go build ./...` succeeds
- [ ] `cd packages && go test ./... -v` — all tests pass
- [ ] `cd packages && go vet ./...` — clean
- [ ] `gofmt -l packages/` prints nothing
- [ ] Full run on `mega_ot_mirage.dem` completes without error
- [ ] `manifest.json` round count matches the match scoreboard
- [ ] `ticks.csv.gz` is 50-80 MB
- [ ] Output loads in DuckDB: `SELECT phase, count(*) FROM 'ticks.csv.gz' GROUP BY phase;` returns all three phases
- [ ] `SELECT max(speed) FROM 'ticks.csv.gz' WHERE vel_valid;` is under ~450 (a boosted/jumping player can exceed 260, but not by 10x)

## Deferred

Recorded so they are not silently lost:

- `players.csv` holds only the last-seen name per SteamID, not the full `(steamid, name, first_seen_tick)` observation table the spec describes. Name changes overwrite. Upgrade when a demo with a mid-match rename shows it matters.
- Timeout ordering relative to `RoundStart` is still unverified. `readTimeout` is called on `RoundStart`; if timeouts turn out to precede it, the flag will never fire and the read needs to move.
- No Parquet sink. The `Sink` interface is the seam when it is wanted.
- **Warmup gating has no unit test.** `Collector.live()` (`matchStarted && !IsWarmupPeriod()`) is what filters warmup rounds and the false-positive `RoundStart` at tick 0. Unit-testing it would need `fake.Parser`, which drags in `testify/mock` and was deliberately avoided. It is covered instead by the integration check "round count matches the match scoreboard" — broken gating inflates the round count with warmup rounds, so that assertion catches it. If gating ever gets more complex than two conditions, extract it into a pure function and test it directly.
