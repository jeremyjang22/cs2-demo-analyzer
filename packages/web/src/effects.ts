/**
 * Short-lived events on the map: tracers, damage markers, and the bomb.
 *
 * Pure — no canvas, no DOM. Everything here answers one question: given a time
 * within a round, what should be on screen? The renderer decides how it looks;
 * this decides what is there, which is the part with edge cases worth testing.
 */

import type { BombEvent, DamageEvent, KitEvent, ShotEvent, TrajectoryEvent } from "./types";

/** An event that is fading out, with how far through its life it is. */
export interface Fading<T> {
  event: T;
  /** 0 at the instant it happened, 1 when it is gone. */
  age: number;
}

/**
 * Events that happened within `life` seconds before `sec`.
 *
 * Scrubbing backwards must not leave ghosts on screen, so this is a window
 * around the current time rather than anything accumulated frame to frame —
 * the renderer holds no state about what it drew last.
 *
 * A linear scan is deliberate. A heavy round holds a few hundred shots, which
 * is nothing at 60 fps, and an index would have to be rebuilt every time the
 * round changes for no measurable gain.
 */
export function fadingAt<T extends { t: number }>(
  events: readonly T[],
  sec: number,
  life: number,
): Fading<T>[] {
  const out: Fading<T>[] = [];
  for (const event of events) {
    if (event.t > sec) break; // events are in time order; nothing later qualifies
    const elapsed = sec - event.t;
    if (elapsed > life) continue;
    out.push({ event, age: life <= 0 ? 1 : elapsed / life });
  }
  return out;
}

/**
 * The bomb's state at `sec`: the last entry at or before it.
 *
 * Null means the round has not shown the bomb yet, which happens on a payload
 * that predates bomb events entirely — not the same as "no bomb", and the
 * renderer draws nothing rather than guessing a position.
 */
export function bombAt(events: readonly BombEvent[], sec: number): BombEvent | null {
  let found: BombEvent | null = null;
  for (const event of events) {
    if (event.t > sec) break;
    found = event;
  }
  return found;
}

/** The CS2 fuse, in seconds. Used to pace the planted bomb's pulse. */
export const FUSE_SEC = 40;

/**
 * How urgent a planted bomb looks, 0 at the plant and 1 at detonation.
 *
 * The real C4 beeps faster as the fuse runs down, and that acceleration is the
 * single most useful thing a spectator reads off it — a bomb at 0.9 is a
 * different situation from one at 0.2, and a steady pulse says neither.
 * Clamped at both ends: a defused bomb stops mattering, and a demo can run a
 * hair past 40 seconds.
 */
export function fuseProgress(planted: BombEvent, sec: number): number {
  return Math.min(1, Math.max(0, (sec - planted.t) / FUSE_SEC));
}

/**
 * Where a tracer ends.
 *
 * A shot that hit somebody knows exactly where it stopped. A shot that hit a
 * wall knows only which way it was pointing, so it gets a fixed-length ray and
 * the renderer fades the far end — the fade IS the statement that the endpoint
 * is unknown. `hit` says which of the two this is, so the caller does not have
 * to re-derive it from the presence of hx.
 */
export function tracerEnd(
  shot: ShotEvent,
  missLength: number,
): { x: number; y: number; hit: boolean } {
  if (shot.hx !== undefined && shot.hy !== undefined) {
    return { x: shot.hx, y: shot.hy, hit: true };
  }
  // World yaw runs counter-clockwise from +X, in world units — the y flip
  // belongs to the projection, not here.
  const rad = (shot.a * Math.PI) / 180;
  return {
    x: shot.x + Math.cos(rad) * missLength,
    y: shot.y + Math.sin(rad) * missLength,
    hit: false,
  };
}

/** Damage totals per player over a window, for a "just took N" readout. */
export function damageTaken(events: Fading<DamageEvent>[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const { event } of events) {
    out.set(event.v, (out.get(event.v) ?? 0) + event.hp);
  }
  return out;
}


/**
 * Defuse kits lying on the ground at `sec`.
 *
 * Replayed from drop/take pairs rather than tracked frame to frame, for the
 * same reason fadingAt is a window: scrubbing backwards has to put a picked-up
 * kit back on the floor, and an accumulator never would.
 *
 * A take with no matching drop is ignored rather than trusted. Kit ids are
 * unique within a round only, so a malformed payload could pair one round's
 * take with another's drop, and dropping the orphan is the safe read.
 */
export function kitsOnGround(events: readonly KitEvent[], sec: number): KitEvent[] {
  const down = new Map<number, KitEvent>();
  for (const event of events) {
    if (event.t > sec) break;
    if (event.ev === "dropped") down.set(event.id, event);
    else down.delete(event.id);
  }
  return [...down.values()];
}

/** How long a landed grenade's arc stays on screen before it is gone. */
export const TRAJECTORY_FADE_SEC = 1.5;

/** A grenade arc, with how much of it has been flown and how faded it is. */
export interface Arc {
  event: TrajectoryEvent;
  /** Index of the last point reached at this moment, inclusive. */
  upto: number;
  /** 0 while in flight, then 0→1 across TRAJECTORY_FADE_SEC after it lands. */
  fade: number;
}

/**
 * Grenade arcs to draw at `sec`: the ones in flight, plus the ones that landed
 * recently enough to still be fading.
 *
 * A landed arc lingers deliberately. A grenade's flight is over in about a
 * second, and at 1x speed an arc that vanished the instant it landed would be
 * a flicker you could watch a whole round without ever catching.
 */
export function arcsAt(events: readonly TrajectoryEvent[], sec: number): Arc[] {
  const out: Arc[] = [];
  for (const event of events) {
    // No early break, unlike the other windows here. Those read lists that are
    // in time order because they came from tick-ordered tables; this one is
    // grouped per projectile, and it was briefly ordered by LANDING time
    // instead - under which a break hid every instant smoke in the demo, since
    // a smoke thrown at 0.3s lands after half the flashes in the round. A
    // round holds a few dozen arcs, so scanning all of them costs nothing and
    // cannot be broken by a reordering upstream.
    if (event.t > sec) continue;
    const since = sec - event.t1;
    if (since > TRAJECTORY_FADE_SEC) continue;

    out.push({
      event,
      upto: pointReached(event, sec),
      fade: since <= 0 ? 0 : since / TRAJECTORY_FADE_SEC,
    });
  }
  return out;
}

/**
 * How far along its own path a grenade has flown at `sec`.
 *
 * Walks `ts` rather than scaling by elapsed/total: the points are spaced by
 * distance and the grenade slows down, so the two disagree by more than a
 * frame near the end of an arc.
 */
export function pointReached(event: TrajectoryEvent, sec: number): number {
  // Rounded to whole centiseconds because that is the resolution `ts` is
  // stored at. Comparing finer is not more accurate, just more fragile:
  // (10.2 - 10) * 100 is 19.999999999999929 in binary floating point, which
  // sits just under the 20 it should match and leaves the grenade's head one
  // point behind at every single boundary.
  const elapsed = Math.round((sec - event.t) * 100);
  let i = 0;
  while (i + 1 < event.ts.length && event.ts[i + 1] <= elapsed) i++;
  return i;
}

/** Where a player died, for the players who have died by `sec`. */
export interface Death {
  id: string;
  x: number;
  y: number;
  level: number;
  /** Seconds since they died — deaths dim as the round moves on. */
  age: number;
}
