/**
 * Two rounds on one clock.
 *
 * A pane shows one round of one payload. The panes share a clock measured from
 * an anchor - the round going live, the bomb plant, or the first kill - so
 * "+0:10" means ten seconds after the same event in both rounds. Comparing two
 * retakes from the plant, or two openings from the first duel, is the point:
 * lining them up from round start would put the moment that matters at
 * different places on the slider.
 *
 * Pure: the page turns these numbers into renderer seeks.
 */

import { chapterLength, type Chapter } from "./timeline";
import type { Payload } from "./types";

export type Anchor = "start" | "plant" | "first";

export const ANCHOR_LABEL: Record<Anchor, string> = {
  start: "Round start",
  plant: "Bomb plant",
  first: "First kill",
};

/** One round of one published payload: `demo/0:5`. */
export interface RoundRef {
  demo: string;
  key: string;
}

const KEY = /^\d+:\d+$/;

export function parseRoundRef(raw: string | null): RoundRef | null {
  if (!raw) return null;
  // Split on the LAST slash: the key never contains one, and splitting on the
  // first would break if a demo name ever did.
  const at = raw.lastIndexOf("/");
  if (at <= 0) return null;
  const demo = raw.slice(0, at);
  const key = raw.slice(at + 1);
  return KEY.test(key) ? { demo, key } : null;
}

export function formatRoundRef(ref: RoundRef): string {
  return `${ref.demo}/${ref.key}`;
}

/**
 * Seconds into a round where the anchor happened, or null when it did not
 * (no plant, no kill). The caller falls back to round start and says so.
 */
export function anchorSec(payload: Payload, key: string, anchor: Anchor): number | null {
  if (anchor === "start") return 0;
  if (anchor === "plant") {
    return (payload.bomb?.[key] ?? []).find((e) => e.st === "planted")?.t ?? null;
  }
  return (payload.kills[key] ?? [])[0]?.t ?? null;
}

/** What the shared clock needs to know about one pane. */
export interface PaneClock {
  /** Seconds into the round of the anchor; 0 when aligned at round start. */
  anchor: number;
  /** The round's full playable length, postround included. */
  length: number;
}

export function paneClock(payload: Payload, chapter: Chapter, anchor: Anchor): PaneClock & { missing: boolean } {
  const at = anchorSec(payload, chapter.key, anchor);
  return { anchor: at ?? 0, length: chapterLength(chapter.meta), missing: at === null };
}

/**
 * The shared clock's range, in seconds relative to the anchor: from the start
 * of whichever round had the longest lead-in, to the end of whichever ran
 * longest after it.
 */
export function clockRange(panes: PaneClock[]): { min: number; max: number } {
  if (panes.length === 0) return { min: 0, max: 0 };
  return {
    min: -Math.max(...panes.map((p) => p.anchor)),
    max: Math.max(...panes.map((p) => p.length - p.anchor)),
  };
}

/**
 * Seconds into this pane's round at shared-clock time `rel`, held to the
 * round: before it began the pane shows its first frame, after it ended its
 * last one, while the other pane plays on.
 */
export function paneSec(p: PaneClock, rel: number): number {
  return Math.max(0, Math.min(p.anchor + rel, p.length));
}

/** "Game 2 · Round 5" for a merged payload, "Round 5" for a single demo. */
export function roundTitle(payload: Payload, chapter: Chapter): string {
  const r = `Round ${chapter.meta.n}`;
  return payload.demos.length > 1 ? `Game ${chapter.meta.dm + 1} · ${r}` : r;
}

/** The round after `key` in the same payload, or the one before at the end. */
export function neighbour(chapters: Chapter[], key: string): Chapter | null {
  const i = chapters.findIndex((c) => c.key === key);
  if (i < 0) return chapters[0] ?? null;
  return chapters[i + 1] ?? chapters[i - 1] ?? null;
}

/** "+0:12" / "−0:04": a time relative to the anchor. */
export function relClock(rel: number): string {
  const sign = rel < 0 ? "−" : "+";
  const s = Math.floor(Math.abs(rel));
  return `${sign}${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}
