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
- Player positions on the first tick where `round_freeze_end` fires (start of round 1 live play)

## Where the "live-round" filter lives

`src/hello_demo.py`, search for `parse_event("round_freeze_end")`. The
first tick in that DataFrame is used as the live-round anchor. This is
more reliable than scanning the `game_phase` tick prop because
`round_freeze_end` fires exactly once per round when buy-time ends.

## API notes (demoparser2 v0.41.x)

- `parse_header()` returns a plain `dict`
- `parse_player_info()` returns a **pandas** DataFrame with columns
  `steamid`, `name`, `team_number`
- `parse_ticks(props, ticks=...)` returns a **pandas** DataFrame;
  every row is one player × one tick
- `parse_event(event_name)` returns a **pandas** DataFrame with a
  `tick` column and any event-specific fields

## Why not Polars?

`demoparser2` v0.41.x returns pandas DataFrames, not Polars. The
`polars` package is still listed as a dependency because demoparser2
uses it internally; `polars-runtime-32` (also installed) handles
ARM/Rosetta compatibility. Switching to a pandas-only analysis layer
here keeps the code straightforward.
