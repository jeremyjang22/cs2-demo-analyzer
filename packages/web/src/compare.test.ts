import { describe, expect, it } from "vitest";
import {
  anchorSec, clockRange, formatRoundRef, neighbour, paneClock, paneSec, parseRoundRef, relClock, roundTitle,
} from "./compare";
import { buildTimeline } from "./timeline";
import type { Payload, RoundMeta } from "./types";

function meta(n: number, dm: number, dur: number, post = 0): RoundMeta {
  return { n, dm, t0: n * 100, dur, post, w: 2, why: "", sa: 0, sb: 0, aw: 1, ok: 1 };
}

const payload = {
  demos: [{ i: 0, label: "a", rounds: 2 }, { i: 1, label: "b", rounds: 1 }],
  rounds: { "0:1": meta(1, 0, 60, 5), "0:2": meta(2, 0, 80), "1:1": meta(1, 1, 50) },
  kills: { "0:1": [{ t: 12, k: "a", v: "b", a: null, w: "ak47", hs: 0, wb: 0 }], "0:2": [] },
  bomb: {
    "0:1": [
      { t: 3, st: "carried", by: "a", x: 0, y: 0, lv: 0 },
      { t: 40, st: "planted", by: "a", x: 0, y: 0, lv: 0, site: "A" },
    ],
  },
} as unknown as Payload;
const tl = buildTimeline(payload);
const ch = (key: string) => tl.chapters.find((c) => c.key === key)!;

describe("round refs", () => {
  it("round-trips demo and key", () => {
    const ref = { demo: "m0nesy-mirage", key: "2:14" };
    expect(parseRoundRef(formatRoundRef(ref))).toEqual(ref);
  });

  it("rejects what is not a round", () => {
    for (const bad of [null, "", "x", "/0:1", "demo/", "demo/abc", "demo/1"]) {
      expect(parseRoundRef(bad)).toBeNull();
    }
  });
});

describe("anchors", () => {
  it("finds the plant and the first kill", () => {
    expect(anchorSec(payload, "0:1", "start")).toBe(0);
    expect(anchorSec(payload, "0:1", "plant")).toBe(40);
    expect(anchorSec(payload, "0:1", "first")).toBe(12);
  });

  // A round with no plant is aligned at its start, and the page says so,
  // rather than silently lining up something that did not happen.
  it("reports a missing anchor", () => {
    expect(anchorSec(payload, "0:2", "plant")).toBeNull();
    expect(paneClock(payload, ch("0:2"), "plant")).toEqual({ anchor: 0, length: 80, missing: true });
  });

  it("counts the postround in a pane's length", () => {
    expect(paneClock(payload, ch("0:1"), "start").length).toBe(65);
  });
});

describe("the shared clock", () => {
  const a = { anchor: 40, length: 65 }; // planted at 0:40
  const b = { anchor: 10, length: 50 }; // planted at 0:10

  it("spans the longest lead-in and the longest tail", () => {
    expect(clockRange([a, b])).toEqual({ min: -40, max: 40 });
    expect(clockRange([])).toEqual({ min: 0, max: 0 });
  });

  it("puts both panes on their anchor at zero", () => {
    expect(paneSec(a, 0)).toBe(40);
    expect(paneSec(b, 0)).toBe(10);
  });

  // The shorter round holds its first or last frame while the other plays.
  it("holds a pane inside its own round", () => {
    expect(paneSec(b, -40)).toBe(0);
    expect(paneSec(b, 40)).toBe(50);
    expect(paneSec(a, 40)).toBe(65);
  });
});

describe("labels", () => {
  it("names the game only when there is more than one", () => {
    expect(roundTitle(payload, ch("1:1"))).toBe("Game 2 · Round 1");
    const single = { ...payload, demos: [payload.demos[0]] } as Payload;
    expect(roundTitle(single, ch("0:1"))).toBe("Round 1");
  });

  it("picks the next round, or the previous one at the end", () => {
    expect(neighbour(tl.chapters, "0:1")!.key).toBe("0:2");
    expect(neighbour(tl.chapters, "1:1")!.key).toBe("0:2");
  });

  it("formats relative time with a sign", () => {
    expect(relClock(12.7)).toBe("+0:12");
    expect(relClock(-64)).toBe("−1:04");
  });
});
