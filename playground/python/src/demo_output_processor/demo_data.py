"""Read round-collector output with the schema's rules already applied.

`docs/round-collector-schema.md` lists five traps in the tick data. Every one of
them is a silent wrong answer rather than a crash, and every consumer has had to
re-derive them from prose. This module is that document made executable:

    ticks = load_ticks(demo_dir)                 # alive, live play, ready to use

1. Dead players keep emitting frozen rows -> `alive_only` (default on).
2. `buttons` lags position by one tick     -> `align_buttons()`.
3. `vel_valid = 0` means "no answer", not  -> `vel_valid_only` (opt in; it also
   "stood still"                              drops each round's first sample,
                                               whose position is perfectly good).
4. `yaw` wraps at +/-180                   -> `yaw_delta()`.
5. `phase` has three values, you want one  -> `phase` (default "live").

The event tables have their own loaders with the same shape: load_kills,
load_shots, load_damage and load_bomb. load_bomb defaults to every phase
rather than "live", because the bomb is picked up in freezetime and can
explode after the round is already decided.

Prefers ticks.parquet when the collector writes one, falling back to the
gzipped CSV, so callers do not change when the storage format does.
"""

import json
import warnings
from pathlib import Path

import numpy as np
import pandas as pd

# Bump only for breaking changes. The check is on the major component so a
# 1.1 that adds columns keeps working.
SCHEMA_MAJOR = "1"

PHASES = ("freeze", "live", "postround")

# Weapons whose fire event is not a bullet leaving a barrel. shots.csv records
# them on purpose — it is the only table that says when a grenade was thrown —
# but a tracer drawn along the view angle would be wrong for every one of them.
# Names are demoinfocs' display strings, which is what the collector writes.
NON_BULLET_WEAPONS = frozenset({
    "Knife", "Zeus x27", "C4",
    "Smoke Grenade", "Flashbang", "HE Grenade",
    "Molotov", "Incendiary Grenade", "Decoy Grenade",
})

# Columns load_ticks needs in hand to apply its own filters, whatever the
# caller asked for.
_FILTER_COLUMNS = {"is_alive": "alive_only", "phase": "phase", "vel_valid": "vel_valid_only"}


class SchemaVersionError(RuntimeError):
    """The output was written by a collector this module does not understand."""


class IncompleteDemoWarning(UserWarning):
    """The demo stopped before its last round finished."""


_WARNED = set()


def read_manifest(demo_dir) -> dict:
    """Load manifest.json, refusing output from an incompatible collector.

    Failing loudly here beats a KeyError three functions deeper, or worse, a
    plausible-looking number computed from a column that changed meaning.
    """
    demo_dir = Path(demo_dir)
    path = demo_dir / "manifest.json"
    if not path.exists():
        raise FileNotFoundError(f"{path} not found — is {demo_dir} a collector output folder?")

    manifest = json.loads(path.read_text())
    version = str(manifest.get("schema_version", "0"))
    if version.split(".")[0] != SCHEMA_MAJOR:
        raise SchemaVersionError(
            f"{path} is schema {version}; this module understands {SCHEMA_MAJOR}.x. "
            "Re-parse the demo or update demo_data.py."
        )

    # load_ticks reads the manifest on every call, so warn once per folder
    # rather than once per read.
    if not manifest.get("complete", True) and demo_dir.resolve() not in _WARNED:
        _WARNED.add(demo_dir.resolve())
        warnings.warn(
            f"{demo_dir.name}: demo ended before its last round completed "
            "(no RoundEndOfficial). Per-round data is intact; the final round is "
            "missing its tail. Filter rounds.csv on complete == 1 to exclude it.",
            IncompleteDemoWarning,
            stacklevel=2,
        )
    return manifest


def ticks_path(demo_dir) -> Path:
    """Where the tick table lives, preferring Parquet when it exists."""
    demo_dir = Path(demo_dir)
    for name in ("ticks.parquet", "ticks.csv.gz"):
        candidate = demo_dir / name
        if candidate.exists():
            return candidate
    raise FileNotFoundError(f"no ticks.parquet or ticks.csv.gz in {demo_dir}")


def load_ticks(demo_dir, *, alive_only=True, phase="live", vel_valid_only=False,
               columns=None, manifest=None) -> pd.DataFrame:
    """Load the tick table with the schema's filters applied.

    alive_only     drop rows after death. Dead players keep emitting rows frozen
                   at their last living values, so any aggregate over buttons,
                   shots_fired or position is wrong without this.
    phase          "live" (default), "freeze", "postround", a list of those, or
                   None for everything. Freeze-time position is constant, and
                   post-round movement carries no analytical weight.
    vel_valid_only drop rows with no usable predecessor. Off by default,
                   because with the other two defaults on it is already a no-op:
                   on the reference demo every vel_valid = 0 row is either a
                   round's first sample (in freeze) or a post-death row (the
                   collector calls vel.forget() on death), so none survive
                   alive + live. It matters once you widen `phase` or turn
                   `alive_only` off, and it is still the honest switch to reach
                   for before reasoning about speed.
    columns        subset to read. Filter columns are added automatically and
                   then dropped again, so asking for ["x", "y"] returns exactly
                   those two.
    """
    demo_dir = Path(demo_dir)
    if manifest is None:
        manifest = read_manifest(demo_dir)

    phases = _normalise_phase(phase)

    wanted = list(columns) if columns is not None else None
    if wanted is not None:
        needed = {col for col, flag in _FILTER_COLUMNS.items()
                  if _filter_active(flag, alive_only, phases, vel_valid_only)}
        read = list(dict.fromkeys(wanted + sorted(needed - set(wanted))))
    else:
        read = None

    path = ticks_path(demo_dir)
    if path.suffix == ".parquet":
        ticks = pd.read_parquet(path, columns=read)
    else:
        # low_memory=False reads the file in one pass instead of inferring a
        # dtype per chunk. The mostly-empty string columns (primary, secondary,
        # nades, active_weapon, place) come out as float NaN in the chunks
        # where nobody had one and as object elsewhere, which is both a silent
        # correctness trap and, combined with usecols, a live crash in pandas'
        # own mixed-dtype warning path.
        ticks = pd.read_csv(path, usecols=read, low_memory=False)

    if alive_only:
        ticks = ticks[ticks["is_alive"] == 1]
    if phases is not None:
        ticks = ticks[ticks["phase"].isin(phases)]
    if vel_valid_only:
        ticks = ticks[ticks["vel_valid"] == 1]

    if wanted is not None:
        ticks = ticks[wanted]
    return ticks.reset_index(drop=True)


def load_rounds(demo_dir, *, complete_only=False) -> pd.DataFrame:
    """One row per round. `complete_only` drops a truncated final round."""
    rounds = pd.read_csv(Path(demo_dir) / "rounds.csv")
    return rounds[rounds["complete"] == 1] if complete_only else rounds


def load_round_players(demo_dir) -> pd.DataFrame:
    """One row per (round, player) — team, buy, and survival."""
    return pd.read_csv(Path(demo_dir) / "round_players.csv")


def load_kills(demo_dir, *, phase="live", dedupe=True) -> pd.DataFrame:
    """One row per death.

    phase   "live" by default. Non-live kills are real but rarely wanted: the
            freeze-phase ones are World deaths (disconnects and suicides) and
            the postround ones are bomb detonations after the round is already
            decided. Pass None to keep everything.
    dedupe  drop a repeat death for the same (round, victim), keeping the
            earliest. demoinfocs occasionally fires a second Kill for an
            already-dead player at the exact tick the round ends — once in 613
            live kills across the three reference demos. Off gives you the raw
            events.
    """
    kills = pd.read_csv(Path(demo_dir) / "kills.csv")

    phases = _normalise_phase(phase)
    if phases is not None:
        kills = kills[kills["phase"].isin(phases)]
    if dedupe:
        kills = kills.sort_values("tick").drop_duplicates(
            subset=["round", "victim_steamid"], keep="first")
    return kills.reset_index(drop=True)


def load_shots(demo_dir, *, phase="live", bullets_only=False) -> pd.DataFrame:
    """One row per weapon fire: who, from where, facing which way.

    bullets_only  drop knife swings and grenade throws. They are real fires and
                  the collector records them deliberately, but a tracer drawn
                  for a thrown smoke is a lie — the projectile does not travel
                  along the view angle. Off by default so the raw table is
                  still what you get.

    There is no endpoint column; see hit_positions() for why, and for the join
    that recovers one where the shot actually landed on somebody.
    """
    shots = pd.read_csv(Path(demo_dir) / "shots.csv")

    phases = _normalise_phase(phase)
    if phases is not None:
        shots = shots[shots["phase"].isin(phases)]
    if bullets_only:
        shots = shots[~shots["weapon"].isin(NON_BULLET_WEAPONS)]
    return shots.reset_index(drop=True)


def load_damage(demo_dir, *, phase="live", kinds=None) -> pd.DataFrame:
    """One row per damage event, with a derived `kind` column.

    kinds  keep only these kinds, e.g. ["bullet"] or ["he", "fire"]. None keeps
           everything. See docs/round-collector-schema.md for the closed set.
    """
    damage = pd.read_csv(Path(demo_dir) / "damage.csv")

    phases = _normalise_phase(phase)
    if phases is not None:
        damage = damage[damage["phase"].isin(phases)]
    if kinds is not None:
        damage = damage[damage["kind"].isin(list(kinds))]
    return damage.reset_index(drop=True)


def load_bomb(demo_dir, *, phase=None) -> pd.DataFrame:
    """One row per C4 state change.

    phase defaults to None here, unlike everywhere else in this module. The
    bomb is picked up during freezetime and can explode after the round is
    decided, so the usual "live" filter would cut both ends off the story.
    """
    bomb = pd.read_csv(Path(demo_dir) / "bomb.csv")
    bomb["site"] = bomb["site"].fillna("")

    phases = _normalise_phase(phase)
    if phases is not None:
        bomb = bomb[bomb["phase"].isin(phases)]
    return bomb.reset_index(drop=True)


def load_kits(demo_dir, *, phase=None) -> pd.DataFrame:
    """Defuse kits hitting the ground and being picked back up.

    Like load_bomb, this defaults to every phase: a kit dropped by the last CT
    to die is often dropped during postround, and it is still the kit lying
    there when the round ends.

    This table is DERIVED, not observed - CS2 does not network a dropped kit as
    an entity, so these rows are reconstructed from per-player state. Read
    collector.KitEvent's doc comment before treating them like kills.csv.
    """
    kits = pd.read_csv(Path(demo_dir) / "kits.csv")

    phases = _normalise_phase(phase)
    if phases is not None:
        kits = kits[kits["phase"].isin(phases)]
    return kits.reset_index(drop=True)


def load_trajectories(demo_dir, *, phase=None) -> pd.DataFrame:
    """Grenade flight paths, one row per sampled point.

    Group by ["round", "projectile_id"] and sort by "seq" to get one throw as a
    polyline. Defaults to every phase: grenades thrown as a round ends keep
    flying into the postround.
    """
    traj = pd.read_csv(Path(demo_dir) / "trajectories.csv")

    phases = _normalise_phase(phase)
    if phases is not None:
        traj = traj[traj["phase"].isin(phases)]
    return traj.reset_index(drop=True)


def hit_positions(shots: pd.DataFrame, damage: pd.DataFrame) -> pd.DataFrame:
    """Attach the victim's position to every shot that hit somebody.

    A demo never traces a bullet. What it does record is that a weapon fired at
    tick t and that someone took damage at tick t from the same attacker, which
    is enough to say where that bullet stopped. Shots that hit a wall have no
    match and keep NaN endpoints — that is the honest answer, not a failure.

    A shotgun blast damaging two players at once produces two damage rows for
    one shot; the nearest one wins, since that is where the visible tracer
    terminated. Ties are broken by taking the first, so the result is stable.
    """
    hits = damage[damage["attacker_steamid"] != 0][
        ["round", "tick", "attacker_steamid", "x", "y", "z"]
    ].rename(columns={"attacker_steamid": "steamid", "x": "hit_x", "y": "hit_y", "z": "hit_z"})

    # merge() hands back a fresh RangeIndex, so the shot a row came from has to
    # be carried explicitly - without it there is nothing to deduplicate on and
    # a shotgun blast quietly turns into several shots.
    merged = shots.assign(_shot=range(len(shots))).merge(
        hits, on=["round", "tick", "steamid"], how="left")

    dist = (merged["hit_x"] - merged["x"]) ** 2 + (merged["hit_y"] - merged["y"]) ** 2
    merged = (merged.assign(_d=dist)
              .sort_values(["_shot", "_d"], na_position="last")
              .drop_duplicates(subset="_shot", keep="first")
              .sort_values("_shot")
              .drop(columns=["_d", "_shot"]))
    return merged.reset_index(drop=True)


def load_players(demo_dir) -> pd.DataFrame:
    """steamid -> last-seen name. Names change mid-match; steamid does not."""
    return pd.read_csv(Path(demo_dir) / "players.csv")


def player_names(demo_dir) -> dict:
    """steamid -> name, for labelling output."""
    players = load_players(demo_dir)
    return dict(zip(players["steamid"], players["name"]))


def align_buttons(ticks: pd.DataFrame, group=("round", "steamid")) -> pd.Series:
    """Return `buttons` shifted into alignment with the position it produced.

    The column comes from `m_nButtonDownMaskPrev`, so the value stored at tick t
    is the *previous* command's mask. The input that produced the position at
    tick t therefore shows up one row later. Shifting back by one lines them up.

    The last sample of each player-round has no successor and becomes NA — there
    is genuinely no answer for it, so it is left missing rather than guessed.
    """
    return ticks.groupby(list(group), sort=False)["buttons"].shift(-1)


def yaw_delta(yaw) -> np.ndarray:
    """Signed change in view angle between consecutive samples, in degrees.

    `yaw` runs [-180, +180] and wraps, so a plain diff turns a 10-degree flick
    across the boundary into a 350-degree one. Wrapping into [-180, 180) fixes
    it. (demoinfocs documents 0-360 for this field and is wrong; the data is
    signed.) Group by player-round before calling, or the first value of each
    group will difference against the previous player.
    """
    return (np.diff(np.asarray(yaw, dtype=float), prepend=np.nan) + 180) % 360 - 180


def _normalise_phase(phase):
    if phase is None:
        return None
    phases = [phase] if isinstance(phase, str) else list(phase)
    unknown = set(phases) - set(PHASES)
    if unknown:
        raise ValueError(f"unknown phase(s) {sorted(unknown)}; expected any of {list(PHASES)}")
    return phases


def _filter_active(flag, alive_only, phases, vel_valid_only):
    return {"alive_only": alive_only,
            "phase": phases is not None,
            "vel_valid_only": vel_valid_only}[flag]
