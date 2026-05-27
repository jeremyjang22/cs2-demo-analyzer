# Go playground

CS2 demo exploration using [demoinfocs-golang](https://github.com/markus-wa/demoinfocs-golang).

## Run

```sh
go build -o bin/hello-demo ./cmd/hello-demo
./bin/hello-demo ../../data/mega_ot_mirage.dem
```

Or in one shot:

```sh
go run ./cmd/hello-demo ../../data/mega_ot_mirage.dem
```

Output has the same five sections as the Python playground (Header,
Players, Tick 0, First live-round tick, Playback Info) but in a
slightly different order — see "Section ordering" below.

## Where the "live-round" filter lives

`cmd/hello-demo/main.go`, search for `AnnouncementMatchStarted`.
demoinfocs is event-driven, so the filter uses two guards inside the
`events.RoundStart` handler:

1. `announcementSeen` — a floor set by `events.AnnouncementMatchStarted`.
   Prevents the false-positive `RoundStart` at tick 0 where
   `IsWarmupPeriod()` is `false` only because the warmup flag hasn't been
   initialised yet.
2. `parser.GameState().IsWarmupPeriod()` — skips any remaining warmup rounds
   after the announcement.

This is analogous to the Python playground's `begin_new_match` floor. The
resulting tick (2543 for `mega_ot_mirage.dem`) differs from Python's (4238)
because demoparser2 uses `round_freeze_end` while demoinfocs uses
`RoundStart` — different points in the same round lifecycle.

## v5 API note: no ParseHeader()

demoinfocs-golang v5 removed the public `ParseHeader()` method. Header data
is now surfaced via net-message handlers:

- `CDemoFileHeader` → map name, server name (fires before first frame)
- `CSVCMsg_ServerInfo` → tick interval, host name (fires early in parsing)
- `TickRate()` / `CurrentTime()` → tick rate and duration (reliable only after `ParseToEnd()`)

FACEIT demos tested here do not include a `CDemoFileInfo` message, so
playback tick count and time come from `TickRate()` and `CurrentTime()`.

## Go shows 11 players, Python shows 10 — SourceTV

When listing participants, Go's demoinfocs exposes 11 entries for a standard
10-player match. The extra entry is `SourceTV (0)` — the observer/replay bot
embedded in every CS2 server. demoinfocs's `Participants().All()` includes it,
while Python's demoparser2 filters it out automatically.

This is the kind of cross-library difference the playground is designed to
surface. The Go code intentionally does not filter SourceTV so the discrepancy
stays visible.

## Section ordering — Go vs Python

Go's output order is:

1. `== Header ==`
2. `== Tick 0 (raw first tick) ==`  (empty — game state not yet populated)
3. `== Players ==`
4. `== First live-round tick ==`
5. `== Playback Info ==`

Python's order is Header → Players → Tick 0 → First live-round tick.
The divergence is structural: demoparser2 parses the whole demo up
front and prints in any order it likes, while demoinfocs streams and
must print things as it learns them. The earliest reliable moment for
"who is playing" in demoinfocs is `AnnouncementMatchStarted` — which
fires after the first FrameDone, so Players ends up landing after
the empty Tick 0 section. Treat the ordering as another cross-library
finding the playground is meant to surface.

## Why a binary in `cmd/hello-demo/`

Standard Go layout — `cmd/<name>/main.go` per binary, leaving room for
additional CLIs (`cmd/inspect-ticks/`, etc.) without restructuring.
