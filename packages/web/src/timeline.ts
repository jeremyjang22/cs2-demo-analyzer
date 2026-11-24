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
  /**
   * Absolute match seconds where the win condition was met — where the round
   * stopped being contested. Everything between here and `end` is postround.
   */
  decided: number;
  /** Absolute match seconds where it ends, postround included. */
  end: number;
}

/** A round's full playable length: contested play plus the postround tail. */
export function chapterLength(meta: RoundMeta): number {
  return meta.dur + (meta.post ?? 0);
}

export interface Timeline {
  chapters: Chapter[];
  /** Absolute seconds from the first round's start to the last round's end. */
  duration: number;
}

/** Seconds of empty clock between one game and the next on a merged timeline. */
export const GAME_GAP = 10;

/**
 * Lay every round on one clock.
 *
 * `t0` counts from the first round of the round's OWN demo, so in a payload
 * merged from several demos every game starts at zero. Taken as-is, five games
 * land on top of each other: chapterAt finds game one's round wherever the
 * clocks collide, and the other games' rounds become unreachable. So games are
 * laid end to end in demo order, each starting GAME_GAP after the last one
 * ends. A single-demo payload is unchanged.
 */
export function buildTimeline(payload: Payload): Timeline {
  const byDemo = new Map<number, Array<[string, RoundMeta]>>();
  for (const entry of Object.entries(payload.rounds)) {
    const dm = entry[1].dm ?? 0;
    const list = byDemo.get(dm);
    if (list) list.push(entry); else byDemo.set(dm, [entry]);
  }

  const chapters: Chapter[] = [];
  let offset = 0;
  for (const dm of [...byDemo.keys()].sort((a, b) => a - b)) {
    const game = byDemo.get(dm)!
      .map(([key, meta]) => ({
        key,
        meta,
        start: offset + meta.t0,
        decided: offset + meta.t0 + meta.dur,
        end: offset + meta.t0 + chapterLength(meta),
      }))
      .sort((a, b) => a.start - b.start);
    chapters.push(...game);
    if (game.length) offset = game[game.length - 1].end + GAME_GAP;
  }

  return {
    chapters,
    duration: chapters.length ? chapters[chapters.length - 1].end : 0,
  };
}

/**
 * Absolute clock time for a moment inside one round, held inside that round.
 *
 * Seeking a renderer to `chapter.start + sec` shows that round only while sec
 * is within its length; past the end, the clock would cross into the gap
 * before the NEXT round and chapterAt would move on to it. Anything that pins
 * a renderer to one round (the compare view) seeks through this instead.
 */
export function timeInRound(chapter: Chapter, sec: number): number {
  return chapter.start + Math.max(0, Math.min(sec, chapterLength(chapter.meta)));
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

/**
 * Seconds into a chapter's own round, clamped to its length.
 *
 * The clamp runs to the end of the postround, not to the win condition. Every
 * track and every event in the payload is timed on this clock, so stopping it
 * at `dur` would freeze the last seven seconds of each round into a single
 * held frame — which is exactly what it used to do.
 */
export function secWithin(chapter: Chapter, t: number): number {
  return Math.max(0, Math.min(t - chapter.start, chapterLength(chapter.meta)));
}

/**
 * Is `t` past this round's win condition?
 *
 * The distinction is worth surfacing rather than leaving implicit: the map
 * looks identical either side of it, but nothing that happens after changes
 * who won, and a viewer who cannot tell will misread a save as a fight.
 */
export function isPostround(chapter: Chapter, t: number): boolean {
  return t > chapter.decided;
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
