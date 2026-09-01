"""Build the movement payload from round-collector output.

Shared by the standalone HTML generator and the JSON exporter that feeds the
web app, so the two can never disagree about what a payload contains.

Positions are resampled from each round's first live tick. Straight lines are
drawn between samples, so the rate is a geometry setting as much as a size one:
at 1 Hz a sprinting player moves ~250 units between samples and the chord cuts
visibly through corners. 4 Hz keeps 97% of the true path length with a
worst-case 65-unit gap, where a player is 32 units wide.

Rounds run past their own win condition. CS2 gives survivors a flat seven
seconds before the next freeze time, and they are not idle seconds: people run
rifles into corners to save them, pick up what the dead dropped, and get hunted
down doing it. `dur` remains the length of contested play - the round table and
every "was this decided yet" question depends on that - and `post` carries the
tail, so a viewer can play through it while an analysis can still ignore it.
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

# Loadout flag bits packed into the third slot of an equipment change entry.
# A bitmask rather than three booleans because this repeats once per change
# per player per round, and three JSON fields of "0" cost more than one digit.
EQ_HELMET, EQ_KIT, EQ_BOMB = 1, 2, 4

# Minimum spacing between consecutive samples of a loose bomb, in seconds.
# bomb.csv records a thrown C4's arc at tick resolution - about 50 rows for a
# one-second throw - which is more than a viewer running at sample_hz can
# show. 0.1s keeps the arc smooth and drops roughly 80% of the rows.
BOMB_SAMPLE_GAP = 0.1

# demoinfocs RoundEndReason values seen in this data.
REASONS = {1: "bomb detonated", 7: "bomb defused", 8: "CT elimination",
           9: "T elimination", 11: "T surrender", 12: "CT surrender"}


def round_events(demo_dir: Path, rounds_meta: dict, tick_rate: float,
                 level_of=None):
    """Every event table for each round, timed from that round's live start.

    All of them carry absolute ticks; the viewer works in seconds within a
    round, so convert here rather than making every consumer redo it. Events
    before freeze end land at a negative time and are dropped: they belong to
    the buy phase, which the movement tracks do not cover. The bomb is the one
    exception - see bomb_events.

    level_of maps a world z to a radar section index, so events land on the
    right floor of a multi-level map. Without it a tracer fired in Nuke's
    lower bombsite would be drawn over upper - plausible-looking and wrong.
    Defaults to "everything is on floor 0", which is correct for every
    single-level map.
    """
    if level_of is None:
        def level_of(_z):
            return 0
    # Every phase, not just live. A player who stays alive to save a rifle and
    # is hunted down in the seven seconds after the round is decided produces a
    # postround kill, and dropping it means the viewer shows a save that
    # succeeded when it did not. Freeze-phase kills still fall away below, on
    # the negative-time check - those are disconnects and suicides.
    kills = demo_data.load_kills(demo_dir, phase=None)  # deduped
    util = pd.read_csv(demo_dir / "utility.csv")
    util = util[util["phase"] == "live"]
    # Live and postround alike: a save that gets hunted down is postround
    # gunfire, and drawing the death without the shots that caused it would be
    # a strange half-picture.
    shots = demo_data.hit_positions(
        demo_data.load_shots(demo_dir, bullets_only=True, phase=["live", "postround"]),
        demo_data.load_damage(demo_dir, phase=None),
    )
    # Every phase. The C4 detonation lands on the exact tick RoundEnd fires and
    # would be lost to a live-only read; the rest of the postround damage is
    # now something the viewer can play through rather than something to hide.
    damage = demo_data.load_damage(demo_dir, phase=None)
    bomb = demo_data.load_bomb(demo_dir)
    kits = demo_data.load_kits(demo_dir)
    traj = demo_data.load_trajectories(demo_dir)

    by_round_kills, by_round_util = {}, {}
    by_round_shots, by_round_damage, by_round_bomb, by_round_kits = {}, {}, {}, {}
    by_round_traj = {}
    for number, meta in rounds_meta.items():
        base = meta["_freeze_end"]
        # Everything on screen runs to the end of the postround now, not to the
        # win condition. `dur` still means contested play; `span` is what the
        # viewer's clock can actually reach.
        span = meta["dur"] + meta["post"]

        k = kills[kills["round"] == number]
        rows = []
        for r in k.itertuples():
            t = (r.tick - base) / tick_rate
            if t < 0 or t > span:
                continue
            row = {
                "t": round(t, 2),
                "k": str(r.killer_steamid) if r.killer_steamid else None,
                "v": str(r.victim_steamid),
                "a": str(r.assister_steamid) if r.assister_steamid else None,
                "w": r.weapon,
                "hs": int(r.headshot),
                "wb": int(r.penetrated > 0),
            }
            if r.phase == "postround":
                # Seconds after the win condition, which is the whole point of
                # the flag: 2.5s reads very differently from 6.9s when the
                # question is whether somebody got out with their gun. Measured
                # off the raw end tick rather than `dur`, which is rounded to a
                # tenth and would leak that rounding into this.
                row["post"] = round((r.tick - meta["_end"]) / tick_rate, 1)
            rows.append(row)
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

        by_round_shots[number] = shot_events(
            shots[shots["round"] == number], base, tick_rate, span, level_of)
        by_round_damage[number] = damage_events(
            damage[damage["round"] == number], base, tick_rate, span, level_of)
        by_round_bomb[number] = bomb_events(
            bomb[bomb["round"] == number], base, tick_rate, span, level_of)
        by_round_kits[number] = kit_events(
            kits[kits["round"] == number], base, tick_rate, span, level_of)
        by_round_traj[number] = trajectory_events(
            traj[traj["round"] == number], base, tick_rate, span, level_of)

    return (by_round_kills, by_round_util, by_round_shots,
            by_round_damage, by_round_bomb, by_round_kits, by_round_traj)


def measure_postround(rounds_meta: dict, ticks: pd.DataFrame, tick_rate: float) -> None:
    """Fill in each round's `post`: seconds of play after the win condition.

    Normally that is RoundEndOfficial minus RoundEnd, which is a flat 7.00s in
    every complete round across the reference demos - CS2's
    mp_round_restart_delay.

    On a truncated round official_end_tick is 0, meaning the demo stopped
    before RoundEndOfficial fired. That is "unknown", not "zero", so the tail
    is measured from the footage that does exist: the last tick actually
    sampled in that round. A viewer can only play what was recorded anyway,
    and the alternative - assuming the usual seven seconds - would invent a
    window the demo never captured.
    """
    last_tick = ticks.groupby("round")["tick"].max().to_dict()

    for number, meta in rounds_meta.items():
        end = meta["_end"]
        official = meta["_official_end"]
        if official is None:
            official = last_tick.get(number, end)
        meta["post"] = round(max(0.0, (official - end) / tick_rate), 1)


def trajectory_events(traj: pd.DataFrame, base: int, tick_rate: float,
                      span: float, level_of) -> list:
    """One entry per grenade thrown, as a polyline with its own clock.

    Point coordinates go in parallel x/y arrays of whole units, the same shape
    and the same reasoning as a player track: two arrays of ints cost less than
    an array of pairs and read the same.

    `ts` is centiseconds from `t` for each point. It could have been left out
    and the time interpolated evenly along the path, but points are sampled by
    DISTANCE, and a grenade decelerates - so even spacing would run the arc
    fast at the start and slow at the end. A byte or two a point buys the real
    timing.
    """
    if traj.empty:
        return []

    rows = []
    for (_, _pid), path in traj.groupby(["round", "projectile_id"], sort=True):
        path = path.sort_values("seq")
        ticks = path["tick"].to_numpy()
        t0 = (ticks[0] - base) / tick_rate
        t1 = (ticks[-1] - base) / tick_rate
        # A grenade thrown before the round went live, or after the viewer's
        # clock runs out, has nowhere to be drawn.
        if t1 < 0 or t0 > span:
            continue

        first = path.iloc[0]
        rows.append({
            "t": round(max(0.0, t0), 2),
            "t1": round(min(t1, span), 2),
            "kind": first.kind,
            "by": str(first.thrower_steamid) if first.thrower_steamid else None,
            "team": int(first.thrower_team),
            "x": [int(round(v)) for v in path["x"]],
            "y": [int(round(v)) for v in path["y"]],
            "ts": [int(round((tk - ticks[0]) / tick_rate * 100)) for tk in ticks],
            # One char per point: a throw from Nuke's upper level to lower
            # crosses floors mid-flight, and drawing the whole arc on one radar
            # would run it through the ceiling.
            "lvl": "".join(str(level_of(z)) for z in path["z"]),
        })

    # Sorted by throw time, which every other event list in this payload is
    # already in by construction (they come from tick-ordered tables). This one
    # does not: it is grouped by projectile_id, and a consumer that walks the
    # list and stops at the first future event - which is the obvious way to
    # read it - silently loses everything after the first out-of-order entry.
    rows.sort(key=lambda r: r["t"])
    return rows


def kit_events(kits: pd.DataFrame, base: int, tick_rate: float, span: float,
               level_of) -> list:
    """Defuse kits appearing on and leaving the ground.

    Negative times are pinned to 0 rather than dropped, the same as the bomb:
    nothing is on the ground before a round starts, but a kit dropped in the
    postround of the PREVIOUS round is not this round's business either, and
    the collector already scopes ids per round. What this actually catches is
    the ordering slack around freeze end.

    A drop with no matching take is the normal case - most kits are never
    picked up - and the viewer keeps drawing it for the rest of the round.
    """
    rows = []
    for r in kits.itertuples():
        t = (r.tick - base) / tick_rate
        if t > span:
            continue
        rows.append({
            "t": round(max(0.0, t), 2),
            "ev": r.event,
            # Pairs a take with its drop. Unique within the round only.
            "id": int(r.kit_id),
            "by": str(r.steamid),
            "x": int(round(r.x)),
            "y": int(round(r.y)),
            "lv": level_of(r.z),
        })
    return rows


def shot_events(shots: pd.DataFrame, base: int, tick_rate: float, span: float,
                level_of) -> list:
    """One entry per bullet fired, with an endpoint where one is knowable.

    Shots that hit somebody carry the victim's position and the tracer stops
    there. Shots that hit a wall carry only a yaw, because that is genuinely
    all the demo recorded - drawing them to a made-up endpoint would be
    inventing geometry the parser never saw.
    """
    rows = []
    for r in shots.itertuples():
        t = (r.tick - base) / tick_rate
        if t < 0 or t > span:
            continue
        event = {
            "t": round(t, 2),
            "by": str(r.steamid),
            "x": int(round(r.x)),
            "y": int(round(r.y)),
            # Whole degrees, matching the yaw stored on every track sample.
            "a": int(round(r.yaw)),
            "lv": level_of(r.z),
        }
        if pd.notna(r.hit_x):
            event["hx"] = int(round(r.hit_x))
            event["hy"] = int(round(r.hit_y))
        rows.append(event)
    return rows


def damage_events(damage: pd.DataFrame, base: int, tick_rate: float, span: float,
                  level_of) -> list:
    """One entry per hit taken, positioned on the victim.

    Simultaneous hits from the same attacker are merged. A shotgun blast is
    nine pellets and damage.csv records all nine - four of them landing on one
    player at tick 11054 in the reference demo - but a viewer stacking nine
    markers on one spot shows a smear, and nobody experienced nine hits. They
    are summed into the one hit the victim actually felt, with `n` recording
    how many components went into it. The per-pellet rows, hitgroup and all,
    are still in damage.csv for anything that wants them.

    `hp` is the raw damage, which can exceed what the victim had left. That
    over-damage is deliberate - a 5 HP player hit for 502 by the bomb took a
    502-damage hit - and a viewer showing "502" over a corpse is showing what
    happened.

    `hpt` is what the victim actually LOST, which is what any average has to be
    built from. It is derived here rather than read from the parser: demoinfocs
    exposes a HealthDamageTaken, but for a shotgun it reports the whole health
    drop on more than one of the pellets that caused it, so summing it double
    counts - one XM1014 player in the reference Anubis demo came out at 110.9
    ADR against a raw figure of 102.3, which is not even possible.

    Tracking `health_remaining` per victim gives the exact answer: the health
    lost to a hit is the difference between the reading before it and after.
    Raw damage puts that demo's mean ADR at 103.6, a figure no player has ever
    posted; this puts it at 80.9.
    """
    merged = {}
    order = []
    # Everyone starts a round at full health. Sorted by tick because a running
    # health reading is meaningless out of order.
    remaining = {}

    for r in damage.sort_values("tick").itertuples():
        t = (r.tick - base) / tick_rate
        if t < 0 or t > span:
            continue
        # How much health this hit actually removed. max(0, ...) because a
        # health shot can raise it, and a negative "damage" helps nobody.
        before = remaining.get(r.victim_steamid, 100)
        actual = max(0, before - int(r.health_remaining))
        remaining[r.victim_steamid] = int(r.health_remaining)

        # Keyed on the raw tick, not the rounded time: two hits 10 ms apart are
        # the same tick at 64 Hz and would merge either way, but keying on a
        # rounded float invites the reverse mistake.
        key = (r.tick, r.victim_steamid, r.attacker_steamid, r.kind)
        hit = merged.get(key)
        if hit is None:
            merged[key] = {
                "t": round(t, 2),
                "v": str(r.victim_steamid),
                "by": str(r.attacker_steamid) if r.attacker_steamid else None,
                "k": r.kind,
                "hp": int(r.health_damage),
                "hpt": actual,
                "ar": int(r.armor_damage),
                # The hitgroup of the biggest component. A blast that clips a
                # leg and a head is a headshot as far as a reader is concerned.
                "hg": int(r.hitgroup),
                "n": 1,
                "x": int(round(r.x)),
                "y": int(round(r.y)),
                "lv": level_of(r.z),
            }
            order.append(key)
            continue

        if r.health_damage > hit["hp"]:
            hit["hg"] = int(r.hitgroup)
        hit["hp"] += int(r.health_damage)
        hit["hpt"] += actual
        hit["ar"] += int(r.armor_damage)
        hit["n"] += 1

    return [merged[k] for k in order]


def bomb_events(bomb: pd.DataFrame, base: int, tick_rate: float, span: float,
                level_of) -> list:
    """The C4's state over one round, decimated to what a viewer can draw.

    Unlike every other table here, negative times are NOT dropped. The bomb is
    picked up during freezetime, so discarding pre-live rows would leave the
    viewer with no carrier until the first handoff - the whole first minute of
    a round would show nobody holding it. The last such row is kept and pinned
    to t = 0, which is exactly "who walked out of spawn with it".

    The far end runs to the close of the postround, so a bomb that detonates
    after the round is decided, or one still lying where its carrier died, is
    on screen for as long as the viewer can scrub to it.
    """
    rows, pending_start = [], None

    for r in bomb.itertuples():
        t = (r.tick - base) / tick_rate
        event = {
            "t": round(max(0.0, min(t, span)), 2),
            "st": r.state,
            "by": str(r.carrier_steamid) if r.carrier_steamid else None,
            "x": int(round(r.x)),
            "y": int(round(r.y)),
            "lv": level_of(r.z),
        }
        if r.site:
            event["site"] = r.site

        if t < 0:
            pending_start = event      # keep only the latest pre-live state
            continue
        if t > span:
            continue

        # Thin out the arc of a thrown bomb; never a state change, which is
        # the only thing that tells the viewer to change what it draws.
        if (rows and event["st"] == rows[-1]["st"] and event["by"] == rows[-1]["by"]
                and event["t"] - rows[-1]["t"] < BOMB_SAMPLE_GAP):
            continue
        rows.append(event)

    if pending_start is not None:
        rows.insert(0, pending_start)
    return rows


def equipment_changes(picked: pd.DataFrame) -> list:
    """Loadout per sample, stored as changes rather than one entry per sample.

    Equipment is nearly constant within a round - a buy, a few grenades thrown,
    the occasional pickup - so storing it per sample would repeat the same
    values a hundred times over. Each entry is

        [sample_index, health, armor, money, flags, primary, secondary, nades]

    and holds until the next one. flags is a bitmask (see EQ_HELMET and
    friends); the two weapon names and the grenade string are exactly the
    columns the collector wrote.

    Health and money ride along here rather than in arrays of their own for
    the same reason: both change a handful of times a round, so a change list
    costs a fraction of a per-sample array and says exactly the same thing.
    """
    out, prev = [], None
    for i, r in enumerate(picked.itertuples()):
        flags = ((EQ_HELMET if r.has_helmet else 0)
                 | (EQ_KIT if r.has_kit else 0)
                 | (EQ_BOMB if r.has_bomb else 0))
        entry = [int(r.health), int(r.armor), int(r.money), flags,
                 "" if pd.isna(r.primary) else r.primary,
                 "" if pd.isna(r.secondary) else r.secondary,
                 "" if pd.isna(r.nades) else r.nades]
        if entry != prev:
            out.append([i] + entry)
            prev = entry
    return out


def player_stats(demo_dirs) -> dict:
    """Cumulative kills / deaths / assists per steamid across every demo.

    Live and postround, matching the game's own scoreboard: a player hunted
    down while saving is a death in CS2 and the kill counts for whoever got
    them. Freeze-phase kills are excluded because those are disconnects and
    suicides, which the scoreboard does not count either - and they have no
    killer to credit, only a victim who would otherwise pick up a death for
    leaving.
    """
    stats = {}

    def bump(steamid, field):
        if not steamid:
            return
        stats.setdefault(str(steamid), {"k": 0, "d": 0, "a": 0})[field] += 1

    for demo_dir in demo_dirs:
        for row in demo_data.load_kills(demo_dir, phase=["live", "postround"]).itertuples():
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
            "post": 0.0,   # filled in below, once the tick data is in hand
            "_freeze_end": int(row.freeze_end_tick),
            "_end": int(row.end_tick),
            # 0 is the sentinel for "the demo stopped before RoundEndOfficial
            # fired", which is routine on a final round. It means the window is
            # unknown, NOT that it is empty: clamping it shut here dropped a
            # real postround death in nukepug's round 20. Nothing else can be
            # misfiled into a truncated round - there is no round after it - so
            # the honest bound is no bound.
            "_official_end": int(row.official_end_tick) or None,
        }

    # alive_only stays off: the dead rows are what tell us a player died this
    # round, and they are dropped per-round a few lines below.
    #
    # Postround is included so the tracks cover the save window. Without it a
    # survivor's samples stop at the win condition and they freeze mid-stride
    # for the whole tail - the one thing worth watching in it would be the one
    # thing not drawn.
    live = demo_data.load_ticks(
        demo_dir, alive_only=False, phase=["live", "postround"], manifest=manifest,
        columns=["round", "tick", "steamid", "team", "x", "y", "z", "yaw",
                 "is_alive", "is_airborne", "is_scoped",
                 "health", "armor", "has_helmet", "has_kit", "has_bomb",
                 "primary", "secondary", "nades", "money"],
    )
    measure_postround(rounds_meta, live, tick_rate)

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
                # Sparse: one entry per change, not per sample. See
                # equipment_changes for the layout.
                "eq": equipment_changes(picked),
            })
        if out:
            tracks[steamid] = (names.get(steamid, str(steamid)), out)

    events = round_events(
        demo_dir, rounds_meta, tick_rate,
        level_of=lambda z: sections.index(radar.section_for(z)),
    )
    return manifest, rounds_meta, tracks, roster_a, events


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
    round_shots, round_damage, round_bomb, round_kits = {}, {}, {}, {}
    round_traj = {}
    map_name, tick_rate, max_samples = None, None, 0

    for di, demo_dir in enumerate(demo_dirs):
        manifest, rounds_meta, tracks, roster_a, events = load_demo(
            demo_dir, sample_hz, radar)
        kills, util, shots, damage, bomb, kits, traj = events

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
            round_shots[key] = shots.get(n, [])
            round_damage[key] = damage.get(n, [])
            round_bomb[key] = bomb.get(n, [])
            round_kits[key] = kits.get(n, [])
            round_traj[key] = traj.get(n, [])

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
        "shots": round_shots,
        "damage": round_damage,
        "bomb": round_bomb,
        "kits": round_kits,
        "traj": round_traj,
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
