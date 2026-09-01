import { describe, expect, it } from "vitest";
import { byScoreboard, statsUpTo } from "./stats";
import { buildTimeline } from "./timeline";
import type { Payload } from "./types";

const kill = (t: number, k: string | null, v: string, a: string | null = null) =>
  ({ t, k, v, a, w: "AK-47", hs: 0, wb: 0 });

const hit = (t: number, by: string | null, v: string, hp: number, hpt = hp) =>
  ({ t, v, by, k: "bullet" as const, hp, hpt, ar: 0, hg: 2, n: 1, x: 0, y: 0, lv: 0 });

const round = (n: number, t0: number, dur: number) =>
  ({ n, dm: 0, t0, dur, w: 2, why: "T elimination", sa: 0, sb: 0, aw: 1, ok: 1 });

/** Two 100-second rounds, starting at 0 and 200. */
const payload = {
  players: [{ id: "a" }, { id: "b" }, { id: "c" }],
  rounds: { "0:1": round(1, 0, 100), "0:2": round(2, 200, 100) },
  kills: {
    "0:1": [kill(10, "a", "c"), kill(50, "a", "b", "c")],
    "0:2": [kill(20, "b", "a")],
  },
  damage: {
    "0:1": [hit(5, "a", "c", 60), hit(9, "a", "c", 90, 40), hit(50, "a", "b", 100)],
    "0:2": [hit(15, "b", "a", 100), hit(80, "c", "b", 30)],
  },
  stats: {},
} as unknown as Payload;

const tl = buildTimeline(payload);

describe("statsUpTo", () => {
  it("counts nothing before anything has happened", () => {
    const s = statsUpTo(payload, tl.chapters, 0);
    expect(s.get("a")).toMatchObject({ kills: 0, deaths: 0, damage: 0 });
  });

  it("accumulates as the round plays", () => {
    expect(statsUpTo(payload, tl.chapters, 10)!.get("a")!.kills).toBe(1);
    expect(statsUpTo(payload, tl.chapters, 49)!.get("a")!.kills).toBe(1);
    expect(statsUpTo(payload, tl.chapters, 50)!.get("a")!.kills).toBe(2);
  });

  it("credits deaths and assists to the right people", () => {
    const s = statsUpTo(payload, tl.chapters, 60);
    expect(s.get("c")).toMatchObject({ deaths: 1, assists: 1, kills: 0 });
    expect(s.get("b")!.deaths).toBe(1);
  });

  // The whole point of the exercise: raw damage includes the over-damage of
  // every killing blow, so an average built on it runs about a third high.
  it("sums damage actually taken, not raw", () => {
    const s = statsUpTo(payload, tl.chapters, 100);
    expect(s.get("a")!.damage).toBe(60 + 40 + 100);
  });

  it("carries totals across rounds", () => {
    const s = statsUpTo(payload, tl.chapters, 250);
    expect(s.get("a")).toMatchObject({ kills: 2, deaths: 1 });
    expect(s.get("b")!.kills).toBe(1);
  });

  // A round counts once it has started, including the one in progress.
  // Otherwise the average lurches at every round boundary instead of settling.
  it("divides by rounds started, including the current one", () => {
    const mid = statsUpTo(payload, tl.chapters, 50);
    expect(mid.get("a")!.adr).toBeCloseTo(200 / 1);

    const later = statsUpTo(payload, tl.chapters, 250);
    expect(later.get("a")!.adr).toBeCloseTo(200 / 2);
  });

  it("ignores a round that has not started", () => {
    // 150s is in the gap between the two rounds; only round 1 has begun.
    const s = statsUpTo(payload, tl.chapters, 150);
    expect(s.get("b")!.kills).toBe(0);
  });

  // World damage - a fall, or the bomb - has nobody to credit.
  it("credits nothing for unattributed damage", () => {
    const world = {
      ...payload,
      damage: { "0:1": [hit(5, null, "c", 50)], "0:2": [] },
    } as unknown as Payload;
    const s = statsUpTo(world, buildTimeline(world).chapters, 50);
    for (const v of s.values()) expect(v.damage).toBe(0);
  });

  it("lists every player, including one who has done nothing", () => {
    const s = statsUpTo(payload, tl.chapters, 1);
    expect([...s.keys()].sort()).toEqual(["a", "b", "c"]);
  });

  // A payload written before damage was exported still has to produce a board.
  it("survives a payload with no damage at all", () => {
    const bare = { ...payload, damage: undefined } as unknown as Payload;
    const s = statsUpTo(bare, tl.chapters, 250);
    expect(s.get("a")!.kills).toBe(2);
    expect(s.get("a")!.adr).toBe(0);
  });
});

describe("byScoreboard", () => {
  const stats = statsUpTo(payload, tl.chapters, 250);
  const nameOf = (id: string) => id;

  it("puts the most kills first", () => {
    expect(byScoreboard(["c", "b", "a"], stats, nameOf)).toEqual(["a", "b", "c"]);
  });

  // Two players on the same kills are not equally responsible for them.
  it("breaks a kill tie on ADR", () => {
    const tied = new Map([
      ["x", { id: "x", kills: 3, deaths: 0, assists: 0, damage: 100, adr: 50 }],
      ["y", { id: "y", kills: 3, deaths: 0, assists: 0, damage: 200, adr: 100 }],
    ]);
    expect(byScoreboard(["x", "y"], tied, nameOf)).toEqual(["y", "x"]);
  });

  // A live board that shuffles equal rows frame to frame is unreadable, so
  // the order has to be total - name is the last resort that guarantees it.
  it("is stable for players who are identical on every stat", () => {
    const same = new Map([
      ["bob", { id: "bob", kills: 1, deaths: 1, assists: 0, damage: 10, adr: 5 }],
      ["amy", { id: "amy", kills: 1, deaths: 1, assists: 0, damage: 10, adr: 5 }],
    ]);
    const once = byScoreboard(["bob", "amy"], same, nameOf);
    const twice = byScoreboard(["amy", "bob"], same, nameOf);
    expect(once).toEqual(["amy", "bob"]);
    expect(twice).toEqual(once);
  });

  it("does not mutate the roster it was given", () => {
    const ids = ["c", "b", "a"];
    byScoreboard(ids, stats, nameOf);
    expect(ids).toEqual(["c", "b", "a"]);
  });
});
