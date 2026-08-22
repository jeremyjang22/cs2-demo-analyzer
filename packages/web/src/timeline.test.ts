import { describe, expect, it } from "vitest";
import {
  buildTimeline, chapterAt, clampTime, scoreBefore, secWithin, seekToRound, stepRound,
} from "./timeline";
import type { Payload, RoundMeta } from "./types";

function round(n: number, t0: number, dur: number, sa: number, sb: number): RoundMeta {
  return { n, dm: 0, t0, dur, w: 2, why: "T elimination", sa, sb, aw: 1, ok: 1 };
}

// Rounds deliberately leave gaps: freeze time and post-round sit between them.
const payload = {
  rounds: {
    "0:1": round(1, 0, 40, 1, 0),
    "0:2": round(2, 100, 30, 1, 1),
    "0:3": round(3, 200, 50, 2, 1),
  },
} as unknown as Payload;

const tl = buildTimeline(payload);

describe("buildTimeline", () => {
  it("orders chapters and measures the match", () => {
    expect(tl.chapters.map((c) => c.meta.n)).toEqual([1, 2, 3]);
    expect(tl.duration).toBe(250);
  });
});

describe("chapterAt", () => {
  it("finds the round containing a time", () => {
    expect(chapterAt(tl, 20)!.meta.n).toBe(1);
    expect(chapterAt(tl, 210)!.meta.n).toBe(3);
  });

  it("clamps before the first round", () => {
    expect(chapterAt(tl, -50)!.meta.n).toBe(1);
  });

  // Scrubbing through freeze time should show the round about to start, not a
  // blank map or the round that just finished.
  it("attributes the gap to the round about to begin", () => {
    expect(chapterAt(tl, 70)!.meta.n).toBe(2);
    expect(chapterAt(tl, 99)!.meta.n).toBe(2);
  });

  it("clamps past the end", () => {
    expect(chapterAt(tl, 9999)!.meta.n).toBe(3);
  });
});

describe("secWithin", () => {
  it("rebases onto the round's own clock", () => {
    expect(secWithin(tl.chapters[1], 115)).toBe(15);
  });

  it("clamps to the round's length", () => {
    expect(secWithin(tl.chapters[1], 9999)).toBe(30);
    expect(secWithin(tl.chapters[1], 0)).toBe(0);
  });
});

describe("stepRound", () => {
  it("goes forward a round", () => {
    expect(stepRound(tl, 10, 1)).toBe(100);
  });

  // Same behaviour as a video player: back mid-chapter restarts it.
  it("restarts the current round when stepping back mid-round", () => {
    expect(stepRound(tl, 130, -1)).toBe(100);
  });

  it("goes to the previous round when stepping back near a round's start", () => {
    expect(stepRound(tl, 100.5, -1)).toBe(0);
  });

  it("clamps at both ends", () => {
    expect(stepRound(tl, 0, -1)).toBe(0);
    expect(stepRound(tl, 200, 5)).toBe(200);
  });
});

describe("seekToRound", () => {
  it("jumps to a chapter by key", () => {
    expect(seekToRound(tl, "0:3")).toBe(200);
  });

  it("falls back to the start for an unknown key", () => {
    expect(seekToRound(tl, "nope")).toBe(0);
  });
});

describe("scoreBefore", () => {
  it("is nil-nil entering the first round", () => {
    expect(scoreBefore(tl, tl.chapters[0])).toEqual([0, 0]);
  });

  // Entering round 3, the score is what round 2 left behind — not round 3's.
  it("reads the previous round's running score", () => {
    expect(scoreBefore(tl, tl.chapters[2])).toEqual([1, 1]);
  });
});

describe("clampTime", () => {
  it("holds the match bounds", () => {
    expect(clampTime(tl, -5)).toBe(0);
    expect(clampTime(tl, 9999)).toBe(250);
  });
});
