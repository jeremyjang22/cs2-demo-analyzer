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
  /** Live-play duration in seconds. */
  dur: number;
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
  teams: Team[];
  /** Cumulative K/D/A per steamid. */
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
