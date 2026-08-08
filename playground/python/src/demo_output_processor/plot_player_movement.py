"""Plot one player's path through one round on top of the map's radar image.

    python plot_player_movement.py --player solitude --round 1
    python plot_player_movement.py --player solitude --round 1 --animate

The path is colored by time so direction is readable without arrows: the
colorbar runs from round start to round end.
"""

import argparse
import json
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np
import pandas as pd
from matplotlib.collections import LineCollection

from radar import RadarMap

REPO_ROOT = Path(__file__).resolve().parents[4]
DEFAULT_DEMO_DIR = REPO_ROOT / "out" / "05-11-2026_mirage_44-32-10"
RADAR_DIR = REPO_ROOT / "data" / "radar"

# Only the columns the plot needs. ticks.csv.gz is ~3.2M rows, and skipping the
# other 24 columns is the difference between a snappy run and a slow one.
TICK_COLUMNS = ["round", "tick", "phase", "steamid", "team", "x", "y", "is_alive"]


def resolve_steamid(players_df: pd.DataFrame, player_name: str) -> int:
    matches = players_df.loc[players_df["name"] == player_name, "steamid"].unique()
    if len(matches) == 0:
        raise SystemExit(
            f"no player named {player_name!r}. "
            f"available: {sorted(players_df['name'].unique())}"
        )
    if len(matches) > 1:
        raise SystemExit(f"{player_name!r} maps to multiple steamids: {matches}")
    return matches[0]


def load_track(demo_dir: Path, steamid: int, round_number: int, include_freeze: bool):
    """Return the player's alive ticks for one round, ordered by tick."""
    ticks = pd.read_csv(demo_dir / "ticks.csv.gz", usecols=TICK_COLUMNS)

    track = ticks[
        (ticks["steamid"] == steamid) & (ticks["round"] == round_number)
    ].sort_values("tick")

    if track.empty:
        rounds_present = sorted(ticks.loc[ticks["steamid"] == steamid, "round"].unique())
        raise SystemExit(
            f"no ticks for steamid {steamid} in round {round_number}. "
            f"rounds available for this player: {rounds_present}"
        )

    # A player standing in spawn during freeze adds a dense blob of identical
    # points that skews the time colormap, so drop it unless asked for.
    if not include_freeze:
        track = track[track["phase"] != "freeze"]

    died = (track["is_alive"] == 0).any()
    return track[track["is_alive"] == 1], died


def plot_path(ax, track: pd.DataFrame, tick_rate: int, died: bool):
    """Draw the trajectory as a time-colored line with start/end markers."""
    xs = track["x"].to_numpy()
    ys = track["y"].to_numpy()
    ticks = track["tick"].to_numpy()
    seconds = (ticks - ticks[0]) / tick_rate

    # One colored segment per tick pair. LineCollection keeps this fast even at
    # a few thousand points, where plotting per-segment would not be.
    points = np.array([xs, ys]).T.reshape(-1, 1, 2)
    segments = np.concatenate([points[:-1], points[1:]], axis=1)

    line = LineCollection(
        segments, cmap="plasma", linewidth=2.4, zorder=3,
        norm=plt.Normalize(seconds.min(), seconds.max()),
    )
    line.set_array(seconds[:-1])
    ax.add_collection(line)

    ax.plot(xs[0], ys[0], "o", color="#00e676", markersize=11,
            markeredgecolor="black", markeredgewidth=1.2, zorder=4, label="start")

    end_style = dict(markeredgecolor="black", markeredgewidth=1.2, zorder=4)
    if died:
        ax.plot(xs[-1], ys[-1], "X", color="#ff1744", markersize=15,
                label="death", **end_style)
    else:
        ax.plot(xs[-1], ys[-1], "s", color="#2979ff", markersize=10,
                label="survived", **end_style)

    return line, seconds


def animate_path(fig, ax, track, tick_rate, out_path, fps, trail_seconds):
    """Write a gif where a comet-tail head walks the path in real time."""
    from matplotlib.animation import FuncAnimation, PillowWriter

    xs = track["x"].to_numpy()
    ys = track["y"].to_numpy()
    ticks = track["tick"].to_numpy()
    seconds = (ticks - ticks[0]) / tick_rate

    # Sample the tick series down to the gif's frame rate instead of drawing
    # every tick, so playback stays real-time rather than 64 fps of slow motion.
    frame_times = np.arange(0, seconds[-1], 1 / fps)
    frame_idx = np.searchsorted(seconds, frame_times)

    trail = LineCollection([], cmap="plasma", linewidth=2.4, zorder=3,
                           norm=plt.Normalize(seconds.min(), seconds.max()))
    ax.add_collection(trail)
    (head,) = ax.plot([], [], "o", color="white", markersize=9,
                      markeredgecolor="black", markeredgewidth=1.2, zorder=5)
    clock = ax.text(0.02, 0.98, "", transform=ax.transAxes, va="top",
                    color="white", fontsize=13, family="monospace",
                    bbox=dict(facecolor="black", alpha=0.6, edgecolor="none"))

    def update(i):
        end = frame_idx[i]
        start = np.searchsorted(seconds, max(0, seconds[end] - trail_seconds))
        window = slice(start, end + 1)

        pts = np.array([xs[window], ys[window]]).T.reshape(-1, 1, 2)
        if len(pts) > 1:
            trail.set_segments(np.concatenate([pts[:-1], pts[1:]], axis=1))
            trail.set_array(seconds[window][:-1])

        head.set_data([xs[end]], [ys[end]])
        clock.set_text(f"{seconds[end]:5.1f}s")
        return trail, head, clock

    anim = FuncAnimation(fig, update, frames=len(frame_idx), interval=1000 / fps, blit=False)
    anim.save(out_path, writer=PillowWriter(fps=fps))


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--player", default="solitude", help="player name from players.csv")
    parser.add_argument("--round", type=int, default=1, dest="round_number")
    parser.add_argument("--demo-dir", type=Path, default=DEFAULT_DEMO_DIR)
    parser.add_argument("--out", type=Path, help="output path (default: alongside the demo output)")
    parser.add_argument("--include-freeze", action="store_true",
                        help="keep freeze-time ticks instead of starting at the live round")
    parser.add_argument("--full-map", action="store_true",
                        help="show the whole radar instead of cropping to the path")
    parser.add_argument("--animate", action="store_true", help="write an animated gif")
    parser.add_argument("--fps", type=int, default=20, help="gif frame rate (default: 20)")
    parser.add_argument("--trail", type=float, default=6.0,
                        help="seconds of trail behind the head in the gif (default: 6)")
    args = parser.parse_args()

    manifest = json.loads((args.demo_dir / "manifest.json").read_text())
    map_name, tick_rate = manifest["map"], manifest["tick_rate"]

    players_df = pd.read_csv(args.demo_dir / "players.csv")
    steamid = resolve_steamid(players_df, args.player)
    print(f"{args.player}: {steamid} on {map_name} @ {tick_rate} tick")

    track, died = load_track(args.demo_dir, steamid, args.round_number, args.include_freeze)
    duration = (track["tick"].iloc[-1] - track["tick"].iloc[0]) / tick_rate
    print(f"round {args.round_number}: {len(track)} alive ticks, {duration:.1f}s, "
          f"{'died' if died else 'survived'}")

    radar = RadarMap(map_name, RADAR_DIR)
    fig, ax = plt.subplots(figsize=(10, 10))
    fig.patch.set_facecolor("#11131a")
    radar.draw(ax)

    line, seconds = plot_path(ax, track, tick_rate, died)

    if not args.full_map:
        pad = 350  # world units of breathing room around the path
        ax.set_xlim(track["x"].min() - pad, track["x"].max() + pad)
        ax.set_ylim(track["y"].min() - pad, track["y"].max() + pad)

    bar = fig.colorbar(line, ax=ax, fraction=0.035, pad=0.02)
    bar.set_label("seconds into round", color="white")
    bar.ax.yaxis.set_tick_params(color="white", labelcolor="white")

    ax.set_title(f"{args.player} — {map_name} round {args.round_number}",
                 color="white", fontsize=15)
    legend = ax.legend(loc="upper right", facecolor="#11131a", edgecolor="#444")
    for text in legend.get_texts():
        text.set_color("white")

    suffix = "gif" if args.animate else "png"
    out = args.out or (args.demo_dir / f"{args.player}_round{args.round_number}.{suffix}")

    if args.animate:
        line.remove()  # the animation redraws the path frame by frame
        animate_path(fig, ax, track, tick_rate, out, args.fps, args.trail)
    else:
        fig.savefig(out, dpi=140, bbox_inches="tight", facecolor=fig.get_facecolor())

    print(f"wrote {out}")


if __name__ == "__main__":
    main()
