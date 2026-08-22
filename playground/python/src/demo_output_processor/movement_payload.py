"""Build the movement payload from round-collector output.

Shared by the standalone HTML generator and the JSON exporter that feeds the
web app, so the two can never disagree about what a payload contains.

Positions are resampled from each round's first live tick. Straight lines are
drawn between samples, so the rate is a geometry setting as much as a size one:
at 1 Hz a sprinting player moves ~250 units between samples and the chord cuts
visibly through corners. 4 Hz keeps 97% of the true path length with a
worst-case 65-unit gap, where a player is 32 units wide.
"""

from pathlib import Path

import numpy as np
import pandas as pd

import demo_data
from radar import RadarMap

REPO_ROOT = Path(__file__).resolve().parents[4]
RADAR_DIR = REPO_ROOT / "assets" / "radar"
OUT_ROOT = REPO_ROOT / "out"

# Categorical slots 1-3 from the validated dark palette, used when the viewer's
# "Accessible" mode is on. Three is the cap for scatter-type forms: past that,
# adjacent pairs stop clearing the colourblind separation floor.
SERIES = ["#3987e5", "#d95926", "#199e70"]

DEFAULT_HZ = 4.0
T, CT = 2, 3

# demoinfocs RoundEndReason values seen in this data.
REASONS = {1: "bomb detonated", 7: "bomb defused", 8: "CT elimination",
           9: "T elimination", 11: "T surrender", 12: "CT surrender"}


def round_events(demo_dir: Path, rounds_meta: dict, tick_rate: float):
    """Kills and utility for each round, timed from that round's live start.

    Both tables carry absolute ticks; the viewer works in seconds within a
    round, so convert here rather than making every consumer redo it. Events
    before freeze end land at a negative time and are dropped: they belong to
    the buy phase, which the movement tracks do not cover.
    """
    kills = demo_data.load_kills(demo_dir)              # live phase, deduped
    util = pd.read_csv(demo_dir / "utility.csv")
    util = util[util["phase"] == "live"]

    by_round_kills, by_round_util = {}, {}
    for number, meta in rounds_meta.items():
        base = meta["_freeze_end"]

        k = kills[kills["round"] == number]
        rows = []
        for r in k.itertuples():
            t = (r.tick - base) / tick_rate
            if t < 0:
                continue
            rows.append({
                "t": round(t, 2),
                "k": str(r.killer_steamid) if r.killer_steamid else None,
                "v": str(r.victim_steamid),
                "a": str(r.assister_steamid) if r.assister_steamid else None,
                "w": r.weapon,
                "hs": int(r.headshot),
                "wb": int(r.penetrated > 0),
            })
        by_round_kills[number] = rows

        u = util[util["round"] == number]
        rows = []
        for r in u.itertuples():
            t = (r.start_tick - base) / tick_rate
            # end_tick 0 means it never expired; run it to the round's end.
            end = r.end_tick if r.end_tick else meta["_end"]
            rows.append({
                "t": round(max(0.0, t), 2),
                "t1": round(max(0.0, (end - base) / tick_rate), 2),
                "kind": r.kind,
                "by": str(r.thrower_steamid) if r.thrower_steamid else None,
                "team": int(r.thrower_team),
                "x": int(round(r.x)),
                "y": int(round(r.y)),
                # Measured spread, not a per-kind constant: a molotov thrown
                # into a corner covers less ground than one on open floor, and
                # the collector records what actually burned.
                "r": int(round(r.radius)) or None,
            })
        by_round_util[number] = rows

    return by_round_kills, by_round_util


def player_stats(demo_dirs) -> dict:
    """Cumulative kills / deaths / assists per steamid across every demo."""
    stats = {}

    def bump(steamid, field):
        if not steamid:
            return
        stats.setdefault(str(steamid), {"k": 0, "d": 0, "a": 0})[field] += 1

    for demo_dir in demo_dirs:
        for row in demo_data.load_kills(demo_dir).itertuples():
            bump(row.killer_steamid, "k")
            bump(row.victim_steamid, "d")
            bump(row.assister_steamid, "a")
    return stats


def split_teams(round_players: pd.DataFrame):
    """Identify the two rosters so score survives the halftime side swap.

    Sides switch at halftime, so counting T wins against CT wins would describe
    two groups that each contain everybody. Anchor on round 1's rosters instead
    and, for every later round, ask which side that roster is currently on.
    """
    first = round_players[round_players["round"] == round_players["round"].min()]
    return set(first.loc[first.team == T, "steamid"]), set(first.loc[first.team == CT, "steamid"])


def load_demo(demo_dir: Path, sample_hz: float, radar: RadarMap):
    """Read one demo's output into (manifest, rounds_meta, tracks_by_player)."""
    manifest = demo_data.read_manifest(demo_dir)
    tick_rate = manifest["tick_rate"]

    rounds_df = demo_data.load_rounds(demo_dir)
    round_players = demo_data.load_round_players(demo_dir)
    names = demo_data.player_names(demo_dir)

    roster_a, _ = split_teams(round_players)
    survived = {(r.round, r.steamid): int(r.survived) for r in round_players.itertuples()}

    rounds_meta, score_a, score_b = {}, 0, 0
    for row in rounds_df.itertuples():
        present = round_players[round_players["round"] == row.number]
        in_a = present[present.steamid.isin(roster_a)]
        # Which side is roster A on this round? Majority vote tolerates a
        # disconnect or a late join without flipping the whole scoreboard.
        side_a = int(in_a.team.mode().iloc[0]) if not in_a.empty else T

        if row.winner == side_a:
            score_a += 1
        else:
            score_b += 1

        rounds_meta[int(row.number)] = {
            "n": int(row.number),
            "dur": round((row.end_tick - row.freeze_end_tick) / tick_rate, 1),
            "w": int(row.winner),
            "why": REASONS.get(int(row.reason), f"reason {row.reason}"),
            "sa": score_a, "sb": score_b,
            "aw": int(row.winner == side_a),      # did roster A take it
            "ok": int(row.complete),
            "_freeze_end": int(row.freeze_end_tick),
            "_end": int(row.end_tick),
        }

    # alive_only stays off: the dead rows are what tell us a player died this
    # round, and they are dropped per-round a few lines below.
    live = demo_data.load_ticks(
        demo_dir, alive_only=False, phase="live", manifest=manifest,
        columns=["round", "tick", "steamid", "team", "x", "y", "z", "yaw",
                 "is_alive", "is_airborne", "is_scoped"],
    )
    step = tick_rate / sample_hz
    sections = radar.section_names

    tracks = {}
    for steamid, per_player in live.groupby("steamid"):
        out = []
        for rnd, g in per_player.groupby("round"):
            g = g.sort_values("tick")
            alive = g[g["is_alive"] == 1]
            if alive.empty or int(rnd) not in rounds_meta:
                continue  # joined late, or a round the collector dropped

            # Snap each whole second to its nearest real tick rather than
            # interpolating here — an interpolated sample can sit inside a wall.
            offsets = (alive["tick"] - alive["tick"].iloc[0]).to_numpy()
            idx = np.searchsorted(offsets, np.arange(0, offsets[-1] + 1, step))
            idx = idx.clip(0, len(offsets) - 1)
            picked = alive.iloc[idx]

            out.append({
                "r": int(rnd),
                "s": int(g["team"].iloc[0]),
                "d": len(idx) - 1 if (g["is_alive"] == 0).any() else -1,
                "sv": survived.get((int(rnd), steamid), 0),
                "x": [int(round(v)) for v in picked["x"]],
                "y": [int(round(v)) for v in picked["y"]],
                # One char per sample. A string costs ~1 byte per sample where a
                # JSON array of ints costs two, and it gzips just as well.
                "air": "".join("1" if v else "0" for v in picked["is_airborne"]),
                "sc": "".join("1" if v else "0" for v in picked["is_scoped"]),
                # Whole degrees is finer than a view cone can show, and keeps
                # this to ~4 bytes a sample instead of ~8.
                "yaw": [int(round(v)) for v in picked["yaw"]],
                # Which radar image each sample belongs on. Always "0" on
                # single-level maps; on Nuke/Vertigo/Train it flips mid-round.
                "lvl": "".join(str(sections.index(radar.section_for(v)))
                               for v in picked["z"]),
            })
        if out:
            tracks[steamid] = (names.get(steamid, str(steamid)), out)

    kills, util = round_events(demo_dir, rounds_meta, tick_rate)
    return manifest, rounds_meta, tracks, roster_a, kills, util


def assign_colors(players):
    """Give each player one of the five minimap colour slots, per starting side.

    The real value lives in the demo — demoinfocs exposes it as
    Player.ColorOrErr() off m_iCompTeammateColor — but the collector does not
    write it to ticks.csv yet, so this stands in with the same shape: five
    slots, assigned within a side. Once the collector emits a colour column,
    read it here instead and the viewer needs no change.
    """
    for side in (T, CT):
        members = [p for p in players if p["rounds"] and p["rounds"][0]["s"] == side]
        for slot, p in enumerate(sorted(members, key=lambda p: p["id"])):
            p["col"] = slot % 5
            p["side0"] = side


def build_payload(demo_dirs, radar: RadarMap, sample_hz: float) -> dict:
    """Merge any number of demos into one payload, joining players by steamid."""
    demos, rounds, players = [], {}, {}
    round_kills, round_util, first_roster = {}, {}, set()
    map_name, tick_rate, max_samples = None, None, 0

    for di, demo_dir in enumerate(demo_dirs):
        manifest, rounds_meta, tracks, roster_a, kills, util = load_demo(
            demo_dir, sample_hz, radar)

        if map_name is None:
            map_name, tick_rate = manifest["map"], manifest["tick_rate"]
        elif manifest["map"] != map_name:
            raise SystemExit(
                f"{demo_dir.name} is {manifest['map']}, not {map_name} — "
                "positions are only comparable within one map"
            )

        demos.append({"i": di, "label": demo_dir.name, "rounds": len(rounds_meta)})

        # Absolute seconds from this demo's first live round, so the viewer can
        # run one continuous match timeline with rounds as chapters.
        origin = min(m["_freeze_end"] for m in rounds_meta.values())
        for n, meta in rounds_meta.items():
            key = f"{di}:{n}"
            public = {k: v for k, v in meta.items() if not k.startswith("_")}
            rounds[key] = {
                **public, "dm": di,
                "t0": round((meta["_freeze_end"] - origin) / manifest["tick_rate"], 1),
            }
            round_kills[key] = kills.get(n, [])
            round_util[key] = util.get(n, [])

        first_roster |= {str(sid) for sid in roster_a} if di == 0 else set()

        for steamid, (name, out) in tracks.items():
            p = players.setdefault(str(steamid), {"id": str(steamid), "name": name, "rounds": []})
            p["name"] = name  # later demos win; players.csv stores last-seen name
            for r in out:
                r["dm"] = di
                r["k"] = f"{di}:{r['r']}"
                max_samples = max(max_samples, len(r["x"]))
            p["rounds"].extend(out)

    ordered = sorted(players.values(), key=lambda p: (-len(p["rounds"]), p["name"].lower()))
    assign_colors(ordered)

    # Group by the round-1 roster rather than by side, so a player stays on the
    # same team across the halftime swap.
    for p in ordered:
        p["team"] = 0 if p["id"] in first_roster else 1
    teams = [
        {"id": 0, "name": "Team 1", "players": [p["id"] for p in ordered if p["team"] == 0]},
        {"id": 1, "name": "Team 2", "players": [p["id"] for p in ordered if p["team"] == 1]},
    ]

    return {
        "map": map_name,
        "radar": {"pos_x": radar.pos_x, "pos_y": radar.pos_y,
                  "scale": radar.scale, "size": radar.width},
        "tick_rate": tick_rate,
        "sample_hz": sample_hz,
        "sections": radar.section_names,
        "max_sec": round((max_samples - 1) / sample_hz, 1),
        "demos": demos,
        "rounds": rounds,
        "kills": round_kills,
        "util": round_util,
        "teams": teams,
        "stats": player_stats(demo_dirs),
        "players": ordered,
    }




def check_demo_dirs(demo_dirs):
    """Fail with something actionable instead of a traceback from read_text().

    Paths are relative to a directory four levels below the repo root, so an
    off-by-one in the ../ count is the likeliest mistake by a wide margin.
    """
    missing = [d for d in demo_dirs if not (d / "manifest.json").exists()]
    if not missing:
        return

    available = sorted(p.name for p in OUT_ROOT.glob("*") if (p / "manifest.json").exists()) \
        if OUT_ROOT.exists() else []
    lines = ["no manifest.json found in:"]
    lines += [f"    {d}  (resolved: {d.resolve()})" for d in missing]
    if available:
        lines += ["", "demos available in out/:"] + [f"    {n}" for n in available]
        lines += ["", f"try:  python export_movement.py --demo {available[0]}"]
    raise SystemExit("\n".join(lines))


def resolve_demo_dirs(names, paths, default="05-11-2026_mirage_44-32-10"):
    """Turn --demo names and --demo-dir paths into checked directories."""
    demo_dirs = [OUT_ROOT / name for name in names] if names else (paths or [])
    if not demo_dirs:
        demo_dirs = [OUT_ROOT / default]
    check_demo_dirs(demo_dirs)
    return demo_dirs
