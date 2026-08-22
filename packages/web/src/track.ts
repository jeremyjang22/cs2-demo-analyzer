/**
 * Pure sampling helpers over a RoundTrack. No canvas, no DOM — this is the part
 * worth testing, and the part that has already produced one hard bug.
 */

import type { RoundTrack } from "./types";

/** Fractional sample index for a time in seconds. Never negative. */
export function idxAt(sec: number, hz: number): number {
  return Math.max(0, sec * hz);
}

/** Length of a track in seconds. */
export function lengthSec(track: RoundTrack, hz: number): number {
  return (track.x.length - 1) / hz;
}

/** Was this player dead at `sec`? */
export function deadAt(track: RoundTrack, sec: number, hz: number): boolean {
  return track.d >= 0 && idxAt(sec, hz) >= track.d;
}

/** Value of a per-sample flag string ("air", "sc", "lvl") at `sec`. */
export function flagAt(flags: string, sec: number, hz: number): string {
  const i = Math.min(Math.floor(idxAt(sec, hz)), flags.length - 1);
  return flags[Math.max(0, i)] ?? "0";
}

/**
 * Interpolated world position at `sec`.
 *
 * The index is clamped at zero deliberately. A negative index reads
 * `undefined` out of the array, which turns every downstream coordinate into
 * NaN — and canvas gradients throw on non-finite input rather than skipping the
 * draw, which killed the whole render loop. Playback can legitimately hand this
 * a slightly negative time when a frame timestamp arrives out of order.
 */
export function posAt(track: RoundTrack, sec: number, hz: number): [number, number] {
  const s = idxAt(sec, hz);
  const i = Math.floor(s);
  const f = s - i;
  const last = track.x.length - 1;
  if (i >= last) return [track.x[last], track.y[last]];
  return [
    track.x[i] + (track.x[i + 1] - track.x[i]) * f,
    track.y[i] + (track.y[i + 1] - track.y[i]) * f,
  ];
}

/**
 * Interpolated view angle at `sec`, in degrees.
 *
 * Yaw is [-180, 180] and wraps, so a plain lerp between +179 and -179 sweeps
 * 358 degrees the wrong way. Take the shortest signed arc instead — the same
 * rule as yaw_delta() on the Python side.
 */
export function yawAt(track: RoundTrack, sec: number, hz: number): number {
  const s = idxAt(sec, hz);
  const i = Math.floor(s);
  const f = s - i;
  const last = track.yaw.length - 1;
  if (i >= last) return track.yaw[last];
  const a = track.yaw[i];
  const delta = ((track.yaw[i + 1] - a + 540) % 360) - 180;
  return a + delta * f;
}

export interface Run {
  /** Inclusive start sample index. */
  from: number;
  /** Inclusive end sample index. */
  to: number;
  /** Floor index, from the `lvl` string. */
  level: number;
  airborne: boolean;
}

/**
 * Split a sample range into runs where both floor and airborne state hold
 * steady, so each run can be drawn on the right canvas in the right colour.
 *
 * Runs overlap by one sample on purpose: without it the segment spanning a
 * boundary is drawn by neither run, and on a multi-level map that segment is
 * exactly the ramp or the vents.
 */
export function runs(track: RoundTrack, from: number, to: number): Run[] {
  const end = Math.min(to, track.x.length - 1);
  const start = Math.max(0, from);
  if (end <= start) return [];

  const out: Run[] = [];
  let i = start;
  while (i < end) {
    const level = Number(track.lvl[i] ?? "0");
    const airborne = track.air[i] === "1";
    let j = i;
    while (
      j < end &&
      Number(track.lvl[j + 1] ?? "0") === level &&
      (track.air[j + 1] === "1") === airborne
    ) {
      j++;
    }
    if (j === i) j++;
    out.push({ from: i, to: j, level, airborne });
    i = j;
  }
  return out;
}
