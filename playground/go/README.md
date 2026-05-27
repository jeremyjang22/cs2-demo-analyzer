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

Output mirrors the Python and Rust playgrounds: header, players, tick-0
positions, first-live-round-tick positions, and a playback summary.

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

## Why a binary in `cmd/hello-demo/`

Standard Go layout — `cmd/<name>/main.go` per binary, leaving room for
additional CLIs (`cmd/inspect-ticks/`, etc.) without restructuring.
