/**
 * The starters view: one player's rounds on one side, laid over each other
 * from the moment each round went live.
 *
 * A single replay shows what happened once. Stacking every T round a player
 * played shows what they USUALLY do - the route they default to, where they
 * are at 0:15, what they throw on the way - which is the thing worth copying
 * from a pug, where nobody is calling a strat for them.
 *
 * Everything here is pure: the component draws what these functions return.
 */

import { deadAt, idxAt, posAt } from "./track";
import type { KillEvent, Payload, RoundTrack, TrajectoryEvent, UtilEvent } from "./types";

export const T = 2;
export const CT = 3;
export type Side = typeof T | typeof CT;

/**
 * One payload in the set being studied, and where its games sit in it.
 *
 * The set is whatever a page asked for - a pro's Mirage pugs, say - and is
 * put together here rather than exported as one merged file, so any filter
 * over the published demos works without an export per combination.
 */
export interface Source {
  name: string;
  payload: Payload;
}

/** One round of the followed player, with what they did in it. */
export interface Start {
  /** Unique across the whole set: `${demoName}/${key}`. */
  id: string;
  /** The payload it came from, and its round key there. */
  demoName: string;
  key: string;
  /** Game number across the set, from 0, for labels. */
  game: number;
  round: number;
  track: RoundTrack;
  won: boolean;
  /** Seconds into the round they died, or null if they lived. */
  diedAt: number | null;
  /** Their kills, with where the victim was when it happened. */
  kills: { t: number; x: number; y: number; weapon: string; hs: boolean }[];
  /** The kill that ended them, if any. */
  death: KillEvent | null;
  /**
   * Whether they were in the round's first kill, and on which end of it.
   * Null when somebody else opened the round.
   */
  opening: "won" | "lost" | null;
  /** Grenades they threw: the flight, and the effect it left where present. */
  throws: TrajectoryEvent[];
  effects: UtilEvent[];
}

/**
 * Every round the player played on `side`, across every source, in order.
 *
 * `ids` is every account the player is known by: a pro pugs on alternates, and
 * a set of their demos can hold more than one. In any one game they play on
 * one account, so the first that is present is the one followed.
 *
 * A round the demo cut short (`ok` 0, the last round of a demo that ended
 * early) is left out: its track stops wherever the recording did, and drawn
 * among complete rounds it reads as somebody freezing mid-route.
 */
export function startsFor(sources: Source[], ids: readonly string[], side: Side): Start[] {
  const out: Start[] = [];
  let gameBase = 0;
  for (const { name, payload } of sources) {
    out.push(...startsIn(name, payload, ids, side, gameBase));
    gameBase += Math.max(1, payload.demos.length);
  }
  return out;
}

function startsIn(name: string, payload: Payload, ids: readonly string[], side: Side, gameBase: number): Start[] {
  const player = ids.map((id) => payload.players.find((p) => p.id === id)).find(Boolean);
  if (!player) return [];
  const playerId = player.id;

  const tracksByKey = new Map<string, Map<string, RoundTrack>>();
  for (const p of payload.players) {
    for (const r of p.rounds) {
      let m = tracksByKey.get(r.k);
      if (!m) tracksByKey.set(r.k, (m = new Map()));
      m.set(p.id, r);
    }
  }

  const hz = payload.sample_hz;
  const out: Start[] = [];
  for (const track of player.rounds) {
    if (track.s !== side) continue;
    const meta = payload.rounds[track.k];
    if (!meta || meta.ok === 0) continue;

    const kills = payload.kills[track.k] ?? [];
    const others = tracksByKey.get(track.k);
    const first = kills[0];
    out.push({
      id: `${name}/${track.k}`,
      demoName: name,
      key: track.k,
      game: gameBase + track.dm,
      round: track.r,
      track,
      won: meta.w === track.s,
      diedAt: track.d >= 0 ? track.d / hz : null,
      kills: kills
        .filter((k) => k.k === playerId && k.v !== playerId)
        .map((k) => {
          const victim = others?.get(k.v);
          const [x, y] = victim ? posAt(victim, k.t, hz) : posAt(track, k.t, hz);
          return { t: k.t, x, y, weapon: k.w, hs: k.hs === 1 };
        }),
      death: kills.find((k) => k.v === playerId) ?? null,
      opening: first ? (first.k === playerId ? "won" : first.v === playerId ? "lost" : null) : null,
      throws: (payload.traj?.[track.k] ?? []).filter((e) => e.by === playerId),
      effects: (payload.util[track.k] ?? []).filter((e) => e.by === playerId),
    });
  }
  out.sort((a, b) => a.game - b.game || a.round - b.round);
  return out;
}

/** Everyone in the set, joined by steamid, with rounds played across it. */
export interface SetPlayer {
  id: string;
  name: string;
  rounds: number;
}

export function playersIn(sources: Source[]): SetPlayer[] {
  const by = new Map<string, SetPlayer>();
  for (const { payload } of sources) {
    for (const p of payload.players) {
      const e = by.get(p.id);
      // Later sources win the name: people rename, and the newest is the one
      // they are known by.
      if (e) { e.rounds += p.rounds.length; e.name = p.name; }
      else by.set(p.id, { id: p.id, name: p.name, rounds: p.rounds.length });
    }
  }
  return [...by.values()].sort((a, b) => b.rounds - a.rounds || a.name.localeCompare(b.name));
}

/**
 * Trail of a round up to `sec`: every whole sample, then the interpolated
 * position at `sec` itself so the line meets the moving dot. Stops at death,
 * because a dead player's track holds their corpse's position.
 */
export function trailTo(track: RoundTrack, sec: number, hz: number): [number, number][] {
  const end = track.d >= 0 ? Math.min(sec, track.d / hz) : sec;
  const last = Math.min(Math.floor(idxAt(end, hz)), track.x.length - 1);
  const pts: [number, number][] = [];
  for (let i = 0; i <= last; i++) pts.push([track.x[i], track.y[i]]);
  if (idxAt(end, hz) > last && last < track.x.length - 1) pts.push(posAt(track, end, hz));
  return pts;
}

/** Where a grenade is along its flight at `sec`, as the points flown so far. */
export function flownTo(e: TrajectoryEvent, sec: number): [number, number][] {
  if (sec < e.t) return [];
  // ts is whole centiseconds from e.t. The epsilon is for float subtraction:
  // (0.7 - 0.5) * 100 is 20.000000000000004, which would otherwise skip the
  // point stamped exactly 20.
  const elapsed = (sec - e.t) * 100 + 1e-6;
  const pts: [number, number][] = [];
  for (let i = 0; i < e.x.length; i++) {
    if (e.ts[i] > elapsed) break;
    pts.push([e.x[i], e.y[i]]);
  }
  return pts;
}

/** The numbers above the map, at one moment across every visible round. */
export interface Snapshot {
  rounds: number;
  alive: number;
  kills: number;
  deaths: number;
  thrown: number;
}

export function snapshot(starts: Start[], sec: number, hz: number): Snapshot {
  const s: Snapshot = { rounds: starts.length, alive: 0, kills: 0, deaths: 0, thrown: 0 };
  for (const st of starts) {
    if (!deadAt(st.track, sec, hz)) s.alive++;
    s.kills += st.kills.filter((k) => k.t <= sec).length;
    if (st.death && st.death.t <= sec) s.deaths++;
    s.thrown += st.throws.filter((e) => e.t <= sec).length;
  }
  return s;
}

/** Whole-set numbers that do not depend on the clock. */
export interface Summary {
  rounds: number;
  won: number;
  openingsWon: number;
  openingsLost: number;
  kills: number;
  deaths: number;
}

export function summarize(starts: Start[]): Summary {
  const s: Summary = { rounds: starts.length, won: 0, openingsWon: 0, openingsLost: 0, kills: 0, deaths: 0 };
  for (const st of starts) {
    if (st.won) s.won++;
    if (st.opening === "won") s.openingsWon++;
    if (st.opening === "lost") s.openingsLost++;
    s.kills += st.kills.length;
    if (st.death) s.deaths++;
  }
  return s;
}

/** The longest visible round, so the slider covers all of them. */
export function longest(starts: Start[], hz: number): number {
  let max = 0;
  for (const st of starts) max = Math.max(max, (st.track.x.length - 1) / hz);
  return max;
}

/**
 * The player the page opens on when the URL names none: whoever played the
 * most rounds across the set - in a set of one player's pugs, that player (or
 * a duo partner who queued with them every game; both are worth watching).
 */
export function defaultPlayer(sources: Source[]): string {
  return playersIn(sources)[0]?.id ?? "";
}
