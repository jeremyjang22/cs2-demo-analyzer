/**
 * Scoreboard numbers as of a moment, rather than for the whole match.
 *
 * `Payload.stats` holds final K/D/A, which is the wrong thing for a viewer
 * that plays a match back: at round 1 it already shows how the match ended,
 * and it never moves, so a board sorted by it can never reorder. These are
 * accumulated from the kill and damage events up to wherever the clock is.
 *
 * Pure, and recomputed per frame rather than cached. A whole match is a few
 * thousand events, so a full pass costs less than the render it feeds — and a
 * cache keyed on time would have to be invalidated on every scrub anyway.
 */

import type { Chapter } from "./timeline";
import type { Payload } from "./types";

export interface LiveStat {
  id: string;
  kills: number;
  deaths: number;
  assists: number;
  /**
   * Health the player has actually taken off other people. Not raw damage —
   * see the exporter's `hpt`, which excludes the over-damage every killing
   * blow produces.
   */
  damage: number;
  /** Damage per round played so far. The headline number after kills. */
  adr: number;
}

const EMPTY = (id: string): LiveStat => ({
  id, kills: 0, deaths: 0, assists: 0, damage: 0, adr: 0,
});

/**
 * Every player's numbers as of absolute match second `t`.
 *
 * A round counts toward the ADR denominator once it has started, including the
 * one in progress. Counting only finished rounds would make the average jump
 * at every round boundary rather than settle as the round plays; counting a
 * round nobody has played yet would dilute it for no reason.
 */
export function statsUpTo(
  payload: Payload,
  chapters: readonly Chapter[],
  t: number,
): Map<string, LiveStat> {
  const out = new Map<string, LiveStat>();
  const get = (id: string) => {
    let s = out.get(id);
    if (!s) out.set(id, (s = EMPTY(id)));
    return s;
  };
  for (const p of payload.players) get(p.id);

  let roundsPlayed = 0;

  for (const chapter of chapters) {
    if (t < chapter.start) break; // chapters are in time order
    roundsPlayed++;

    // Everything in a finished round counts; in the round on screen, only
    // what has happened by now.
    const cutoff = t >= chapter.end ? Infinity : t - chapter.start;

    for (const kill of payload.kills[chapter.key] ?? []) {
      if (kill.t > cutoff) break;
      if (kill.k) get(kill.k).kills++;
      if (kill.a) get(kill.a).assists++;
      get(kill.v).deaths++;
    }

    for (const hit of payload.damage?.[chapter.key] ?? []) {
      if (hit.t > cutoff) break;
      // World damage — a fall, or the bomb — has nobody to credit.
      if (hit.by) get(hit.by).damage += hit.hpt ?? 0;
    }
  }

  if (roundsPlayed > 0) {
    for (const s of out.values()) s.adr = s.damage / roundsPlayed;
  }
  return out;
}

/**
 * Roster order for a scoreboard: best first.
 *
 * Kills lead because that is what a scoreboard is read for. ADR breaks ties
 * because two players on four kills are not equally responsible for them, and
 * it is the number already on the row. Name last so the order is total — an
 * unstable sort on equal keys would let rows swap places frame to frame while
 * nothing was happening, which is the one thing a live board must not do.
 */
export function byScoreboard(
  ids: readonly string[],
  stats: Map<string, LiveStat>,
  nameOf: (id: string) => string,
): string[] {
  return [...ids].sort((a, b) => {
    const sa = stats.get(a) ?? EMPTY(a);
    const sb = stats.get(b) ?? EMPTY(b);
    return (
      sb.kills - sa.kills ||
      sb.adr - sa.adr ||
      sa.deaths - sb.deaths ||
      nameOf(a).localeCompare(nameOf(b))
    );
  });
}
