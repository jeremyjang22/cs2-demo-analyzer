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
    print("\nPlayers:")
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
    # The most reliable marker is the round_freeze_end game event: it fires
    # exactly when the freeze period ends and free play begins each round.
    # The very first round_freeze_end is the start of the first live round.
    print("\n== First live-round tick ==")
    freeze_end_df = parser.parse_event("round_freeze_end")
    if len(freeze_end_df) == 0:
        print("  (no round_freeze_end events found — cannot determine first live tick)")
        return 0

    first_live_tick = int(freeze_end_df["tick"].min())
    positions = parser.parse_ticks(
        ["X", "Y", "Z", "name"],
        ticks=[first_live_tick],
    )
    print(f"Tick: {first_live_tick}  (first round_freeze_end)")
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
