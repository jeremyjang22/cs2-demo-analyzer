/**
 * The match as one continuous timeline, with rounds as chapters.
 *
 * Every round's samples are stored relative to its own live start, but the
 * viewer scrubs a single match clock. This module is the conversion between
 * the two, plus the seek helpers the playback bar needs. Pure — no canvas, no
 * DOM.
 */

import type { Payload, RoundMeta } from "./types";

export interface Chapter {
  key: string;
  meta: RoundMeta;
  /** Absolute match seconds where this round's live play starts. */
  start: number;
  /** Absolute match seconds where it ends. */
  end: number;
}

export interface Timeline {
  chapters: Chapter[];
  /** Absolute seconds from the first round's start to the last round's end. */
  duration: number;
}

export function buildTimeline(payload: Payload): Timeline {
  const chapters = Object.entries(payload.rounds)
    .map(([key, meta]) => ({ key, meta, start: meta.t0, end: meta.t0 + meta.dur }))
    .sort((a, b) => a.start - b.start);

  return {
    chapters,
    duration: chapters.length ? chapters[chapters.length - 1].end : 0,
  };
}

/**
 * The chapter containing `t`.
 *
 * Rounds do not tile the timeline: freeze time and the post-round period sit
 * between one round's end and the next one's start. A time landing in that gap
 * belongs to the round about to begin, so scrubbing through it shows players
 * in their buy positions rather than a blank map.
 */
export function chapterAt(timeline: Timeline, t: number): Chapter | null {
  const { chapters } = timeline;
  if (!chapters.length) return null;
  if (t <= chapters[0].start) return chapters[0];

  for (let i = 0; i < chapters.length; i++) {
    if (t <= chapters[i].end) return chapters[i];
    // In the gap before the next round starts.
    if (i + 1 < chapters.length && t < chapters[i + 1].start) return chapters[i + 1];
  }
  return chapters[chapters.length - 1];
}

/** Seconds into a chapter's own round, clamped to its length. */
export function secWithin(chapter: Chapter, t: number): number {
  return Math.max(0, Math.min(t - chapter.start, chapter.meta.dur));
}

export function clampTime(timeline: Timeline, t: number): number {
  return Math.max(0, Math.min(t, timeline.duration));
}

/**
 * Start of the chapter `delta` rounds away.
 *
 * Stepping back mid-round restarts the current round first, matching how a
 * video player's "previous chapter" behaves — otherwise a viewer who is ten
 * seconds in and wants the start of what they are watching gets the previous
 * round instead.
 */
export function stepRound(timeline: Timeline, t: number, delta: number): number {
  const current = chapterAt(timeline, t);
  if (!current) return t;
  const index = timeline.chapters.indexOf(current);

  const RESTART_GRACE = 1.5;
  if (delta < 0 && t - current.start > RESTART_GRACE) return current.start;

  const target = Math.max(0, Math.min(index + delta, timeline.chapters.length - 1));
  return timeline.chapters[target].start;
}

/** Jump to a round by its chapter key. */
export function seekToRound(timeline: Timeline, key: string): number {
  return timeline.chapters.find((c) => c.key === key)?.start ?? 0;
}

/** Score going into a chapter — what a live scoreboard should read. */
export function scoreBefore(timeline: Timeline, chapter: Chapter): [number, number] {
  const index = timeline.chapters.indexOf(chapter);
  if (index <= 0) return [0, 0];
  const previous = timeline.chapters[index - 1].meta;
  return [previous.sa, previous.sb];
}
