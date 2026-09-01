/**
 * Shape of movement.json, written by export_movement.py.
 *
 * Keys are terse because they repeat once per round per player and the file is
 * fetched over the wire. The Python side is the source of truth; if these drift
 * the app breaks at runtime, which is why `assertPayload` exists.
 */

export interface RadarMeta {
  /** World coordinate of the radar image's top-left pixel. */
  pos_x: number;
  pos_y: number;
  /** World units per pixel. */
  scale: number;
  /** Radar image edge length in pixels (square). */
  size: number;
}

/** One player's samples for one round, at `sample_hz`. */
export interface RoundTrack {
  /** Round number within its demo. */
  r: number;
  /** Demo index. */
  dm: number;
  /** `${dm}:${r}` — key into Payload.rounds. */
  k: string;
  /** Side this round: 2 = T, 3 = CT. */
  s: number;
  /** Sample index at which they died, or -1 if they survived. */
  d: number;
  /** 1 if they were alive at round end. */
  sv: number;
  x: number[];
  y: number[];
  /** View angle in whole degrees, [-180, 180], wraps. */
  yaw: number[];
  /** One char per sample: "1" = airborne. */
  air: string;
  /** One char per sample: "1" = scoped. */
  sc: string;
  /** One char per sample: index into Payload.sections. */
  lvl: string;
  /**
   * Loadout, stored sparsely: one entry per change, not per sample. Absent on
   * payloads written before the exporter learned to emit it.
   */
  eq?: EquipEntry[];
}

/**
 * One loadout change, as a positional tuple:
 *
 *     [sampleIndex, health, armor, money, flags, primary, secondary, nades]
 *
 * It holds until the next entry. A tuple rather than an object because this
 * repeats a handful of times per player per round and the field names would
 * outweigh the values — the same reasoning behind the one-char flag strings
 * above. `equipAt` in track.ts is the only thing that should index into it.
 */
export type EquipEntry = [number, number, number, number, number, string, string, string];

/** Bit positions in an EquipEntry's flags slot. */
export const EQ_HELMET = 1;
export const EQ_KIT = 2;
export const EQ_BOMB = 4;

/** A loadout resolved to something readable. */
export interface Loadout {
  hp: number;
  armor: number;
  /** Cash in hand, live — it moves on a buy, a kill reward, and the payout. */
  money: number;
  helmet: boolean;
  kit: boolean;
  bomb: boolean;
  /** Weapon name, or "" when the slot is empty — which is a real state. */
  primary: string;
  secondary: string;
  /**
   * One character per grenade carried, sorted: h HE, f flash, s smoke,
   * m molotov, i incendiary, d decoy. "ff" is two flashes.
   */
  nades: string;
}

export interface Player {
  /** SteamID as a string — exceeds Number.MAX_SAFE_INTEGER. */
  id: string;
  name: string;
  /** Roster index, stable across the halftime swap. */
  team: number;
  /** Minimap colour slot, 0-4. */
  col: number;
  /** Side in their first round, used for grouping. */
  side0: number;
  rounds: RoundTrack[];
}

export interface RoundMeta {
  n: number;
  dm: number;
  /** Absolute seconds from match start to this round's live start. */
  t0: number;
  /** Live-play duration in seconds — up to the win condition, not past it. */
  dur: number;
  /**
   * Seconds of postround after the win condition: CS2's seven-second window
   * before the next freeze time, where survivors save rifles, pick up what the
   * dead dropped, and occasionally get hunted down doing it.
   *
   * Kept separate from `dur` rather than folded into it, because the two
   * answer different questions. Anything asking "how long was this round
   * contested" wants `dur`; the viewer's clock wants both. Absent on payloads
   * written before the tail was exported, which is why every reader defaults
   * it to 0.
   */
  post?: number;
  /** Winning side: 2 = T, 3 = CT. */
  w: number;
  /** Human-readable end reason. */
  why: string;
  /** Running score after this round. */
  sa: number;
  sb: number;
  /** 1 if roster A won. */
  aw: number;
  /** 0 if the round was truncated. */
  ok: number;
}

export interface DemoRef {
  i: number;
  label: string;
  rounds: number;
}

/** One death, timed from its round's live start. */
export interface KillEvent {
  t: number;
  /** Killer steamid; null for world damage. */
  k: string | null;
  v: string;
  a: string | null;
  w: string;
  /** 1 when a headshot. */
  hs: number;
  /** 1 when the bullet penetrated something. */
  wb: number;
  /**
   * Seconds after the win condition, when this death happened during the
   * postround. Absent for a normal kill.
   *
   * CS2 gives survivors seven seconds after a round is decided, and people use
   * them — running for a corner to save a rifle, or hunting down whoever is
   * trying to. A death here does not change the round's result and does not
   * count against `sv` on the track, but it does cost that player their gun
   * for the next round, which is usually the reason anyone was still alive.
   *
   * `t` is clamped to the round's own length, since the viewer's clock stops
   * at the win condition. This field is the real offset.
   */
  post?: number;
}

/** One grenade effect, timed from its round's live start. */
export interface UtilEvent {
  t: number;
  /** When it expired. Equals `t` for HE and flash. */
  t1: number;
  /**
   * Molotov and incendiary are separate because they burn for visibly
   * different lengths — medians of 7.02s and 5.50s. CS2 does not network
   * which grenade produced an inferno, so the collector derives this from
   * the projectile that was thrown.
   */
  kind: "smoke" | "molotov" | "incendiary" | "he" | "flash" | "decoy";
  /** Thrower steamid; null when unattributed. */
  by: string | null;
  /** Thrower side: 2 = T, 3 = CT. */
  team: number;
  x: number;
  y: number;
  /**
   * Measured spread in Hammer units, for infernos. Molotovs reach 150 and
   * incendiaries 110, and either covers less when it lands against geometry —
   * so this is per-event rather than a constant per kind. Null for effects the
   * demo does not measure.
   */
  r: number | null;
}

/**
 * One bullet leaving a barrel, timed from its round's live start.
 *
 * `hx`/`hy` are where it stopped, and are present only when the shot damaged
 * somebody — the demo does not trace bullets, so a shot that hit a wall has a
 * direction and no endpoint. Drawing those two cases identically would be
 * inventing geometry; see how the renderer fades one and terminates the other.
 */
export interface ShotEvent {
  t: number;
  /** Shooter steamid. */
  by: string;
  x: number;
  y: number;
  /** View angle in whole degrees, same convention as RoundTrack.yaw. */
  a: number;
  /** Index into Payload.sections — which floor the shooter was on. */
  lv: number;
  hx?: number;
  hy?: number;
}

/** One hit taken, timed from its round's live start and placed on the victim. */
export interface DamageEvent {
  t: number;
  /** Victim steamid. */
  v: string;
  /** Attacker steamid; null for world damage — a fall or the bomb. */
  by: string | null;
  /** What caused it. A closed set, but treat it as open — see DAMAGE_STYLE. */
  k: "bullet" | "he" | "fire" | "impact" | "bomb" | "fall" | "knife" | "zeus" | "other";
  /** Raw health damage, which can exceed what the victim had left. */
  hp: number;
  /**
   * Health the victim actually lost — raw damage minus the over-damage a
   * killing blow produces. This is the one to average; `hp` is the one to
   * show on a marker, because it is what the weapon did.
   */
  hpt: number;
  ar: number;
  /** Hit group: 1 head, 2 chest, 3 stomach, 4/5 arms, 6/7 legs, 8 neck, 0 generic. */
  hg: number;
  /**
   * How many simultaneous hits were merged into this one. A shotgun blast is
   * nine pellets and the viewer shows the one hit the victim felt.
   */
  n: number;
  x: number;
  y: number;
  lv: number;
}

/**
 * One C4 state change, timed from its round's live start.
 *
 * These are changes, not samples: an entry holds until the next one. A
 * `carried` entry's x/y is where the pickup happened and goes stale
 * immediately — follow the carrier's track instead, which is what the renderer
 * does. A loose bomb is sampled while it moves, so its entries stay accurate.
 */
export interface BombEvent {
  t: number;
  st: "carried" | "dropped" | "planted" | "defused" | "exploded";
  /** Carrier steamid; on a planted entry the planter, on defused the defuser. */
  by: string | null;
  x: number;
  y: number;
  lv: number;
  /** "A" or "B", from the plant onward. */
  site?: string;
}

/**
 * A defuse kit hitting the ground, or being picked back up.
 *
 * DERIVED, not observed. CS2 never networks a dropped kit as an entity — it
 * exists only as a flag on a player pawn — so these are reconstructed by the
 * collector from the moment that flag changes. `id` pairs a "taken" with the
 * "dropped" it refers to, and is unique within a round only.
 *
 * A drop with no matching take is the normal case: most kits are never
 * retrieved and stay on the ground for the rest of the round.
 */
export interface KitEvent {
  t: number;
  ev: "dropped" | "taken";
  id: number;
  /** Who dropped it, or who took it. */
  by: string;
  x: number;
  y: number;
  lv: number;
}

/**
 * One grenade's flight path, timed from its round's live start.
 *
 * Points are parallel x/y arrays of whole units, the same shape as a player
 * track. `ts` is centiseconds from `t` per point — points are sampled by
 * DISTANCE and a grenade decelerates, so interpolating time evenly along the
 * path would run the arc fast at the start and slow at the end.
 *
 * This is where the grenade FLEW, which is not where its effect ended up:
 * `UtilEvent` has the latter. A smoke that clips a doorframe and drops short
 * has a revealing arc and an unremarkable landing spot.
 */
export interface TrajectoryEvent {
  t: number;
  /** When it stopped moving — landed, or detonated in the air. */
  t1: number;
  kind: UtilEvent["kind"];
  /** Thrower steamid; null when unattributed. */
  by: string | null;
  team: number;
  x: number[];
  y: number[];
  /** Centiseconds from `t`, one per point. */
  ts: number[];
  /** One char per point: index into Payload.sections. A throw can cross floors. */
  lvl: string;
}

export interface Team {
  id: number;
  name: string;
  players: string[];
}

export interface PlayerStats {
  k: number;
  d: number;
  a: number;
}

export interface Payload {
  map: string;
  radar: RadarMeta;
  tick_rate: number;
  /** Samples per second in every track array. */
  sample_hz: number;
  /** Longest round, in seconds. */
  max_sec: number;
  /** Floor names, "default" first. Index matches RoundTrack.lvl chars. */
  sections: string[];
  demos: DemoRef[];
  rounds: Record<string, RoundMeta>;
  /** Kills per round key. */
  kills: Record<string, KillEvent[]>;
  /** Grenade effects per round key. */
  util: Record<string, UtilEvent[]>;
  /**
   * Bullets fired, hits taken, and the bomb, per round key. Optional: a
   * movement.json written before these existed is still perfectly drawable,
   * just without them, so the renderer defaults each to empty rather than
   * refusing the payload. See assertPayload for what genuinely is required.
   */
  shots?: Record<string, ShotEvent[]>;
  damage?: Record<string, DamageEvent[]>;
  bomb?: Record<string, BombEvent[]>;
  kits?: Record<string, KitEvent[]>;
  traj?: Record<string, TrajectoryEvent[]>;
  teams: Team[];
  /**
   * Final K/D/A per steamid, for the whole match.
   *
   * No longer read by the viewer: the scoreboard accumulates its own numbers
   * up to the current moment (see stats.ts), because a fixed final total
   * cannot reorder and spoils the result from the opening round.
   *
   * Kept anyway. It is a documented part of the payload, it costs a few
   * hundred bytes, and it is the obvious thing for a notebook or a future
   * match-summary page to read rather than recomputing. Remove it only when
   * something actually wants the space.
   */
  stats: Record<string, PlayerStats>;
  players: Player[];
}

const REQUIRED = ["map", "radar", "sample_hz", "max_sec", "sections",
  "rounds", "players", "kills", "util", "teams", "stats"] as const;

/**
 * Fail loudly on a payload this app cannot draw.
 *
 * The alternative is a blank canvas and an undefined-property error somewhere in
 * the render loop, which says nothing about the actual problem — a stale JSON
 * file written by an older exporter.
 */
export function assertPayload(data: unknown): asserts data is Payload {
  if (typeof data !== "object" || data === null) {
    throw new Error("movement.json is not an object");
  }
  const p = data as Record<string, unknown>;
  const missing = REQUIRED.filter((key) => !(key in p));
  if (missing.length) {
    throw new Error(
      `movement.json is missing ${missing.join(", ")} — regenerate it with export_movement.py`,
    );
  }
  if (!Array.isArray(p.players) || p.players.length === 0) {
    throw new Error("movement.json contains no players");
  }
}
