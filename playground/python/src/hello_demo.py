"""Open a CS2 .dem and print header + tick-0 + first-live-tick positions."""

import sys
import warnings
from pathlib import Path

# demoparser2 returns pandas DataFrames; suppress polars CPU-compat warning
# that fires when running under Rosetta (x86 Python on Apple Silicon).
warnings.filterwarnings("ignore", category=RuntimeWarning, module="polars")

from demoparser2 import DemoParser  # noqa: E402


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
    # parse_player_info() returns a pandas DataFrame with columns:
    #   steamid, name, team_number
    players = parser.parse_player_info()
    print("\n== Players ==")
    for _, row in players.iterrows():
        name = row.get("name", "<unknown>")
        steamid = row.get("steamid", "<unknown>")
        print(f"  - {name}  ({steamid})")
    print(f"  ({len(players)} total)")

    # --- Tick 0 (raw) ---
    print("\n== Tick 0 (raw first tick) ==")
    # parse_ticks() returns a pandas DataFrame with a row per player per tick.
    # Columns always include: tick, steamid, name, plus whatever props you asked for.
    tick0 = parser.parse_ticks(["X", "Y", "Z", "name"], ticks=[0])
    if len(tick0) == 0:
        print("  (no rows returned for tick 0 — players may not be spawned yet)")
    else:
        for _, row in tick0.iterrows():
            print(
                f"  {row['name']:<24}  "
                f"x={row['X']:>8.1f}  y={row['Y']:>8.1f}  z={row['Z']:>8.1f}"
            )

    # --- First live-round tick ---
    # CS2 fires round_freeze_end during warmup too, so the smallest such tick
    # is a warmup-era event, not the first competitive round.
    #
    # Strategy: locate the tick where the actual match begins via
    # begin_new_match, then take the first round_freeze_end AFTER that tick.
    #
    # begin_new_match fires twice in a typical FACEIT demo:
    #   - once at tick 0 (a demo-start artifact)
    #   - once at the real match start (after the warmup-end knife round or
    #     ready-up phase)
    # We use the maximum begin_new_match tick as the warmup-end floor.
    print("\n== First live-round tick ==")
    freeze_end_df = parser.parse_event("round_freeze_end")
    if len(freeze_end_df) == 0:
        print("  (no round_freeze_end events found — cannot determine first live tick)")
        return 0

    match_start_df = parser.parse_event("begin_new_match")
    if len(match_start_df) == 0:
        # Fallback: no begin_new_match found — use smallest round_freeze_end,
        # but note it may land inside warmup.
        match_start_tick = 0
        print("  WARNING: begin_new_match not found; live-round anchor may be warmup-era")
    else:
        # Take the largest tick (ignores the tick-0 demo artifact).
        match_start_tick = int(match_start_df["tick"].max())

    live_freeze_ends = freeze_end_df[freeze_end_df["tick"] > match_start_tick]
    if len(live_freeze_ends) == 0:
        print(
            f"  WARNING: no round_freeze_end after begin_new_match "
            f"(tick {match_start_tick}); falling back to global minimum"
        )
        first_live_tick = int(freeze_end_df["tick"].min())
    else:
        first_live_tick = int(live_freeze_ends["tick"].min())

    positions = parser.parse_ticks(
        ["X", "Y", "Z", "name"],
        ticks=[first_live_tick],
    )
    print(
        f"Tick: {first_live_tick}  "
        f"(first round_freeze_end after begin_new_match at tick {match_start_tick})"
    )
    if len(positions) == 0:
        print("  (no position rows at that tick)")
    else:
        for _, row in positions.iterrows():
            print(
                f"  {row['name']:<24}  "
                f"x={row['X']:>8.1f}  y={row['Y']:>8.1f}  z={row['Z']:>8.1f}"
            )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
