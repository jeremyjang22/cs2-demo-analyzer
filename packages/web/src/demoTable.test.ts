import { describe, expect, it } from "vitest";
import {
  applyFilters, lineOf, NO_FILTERS, recordOf, resultOf, sampleDemos, sortDemos,
  yourDemos, type Subject,
} from "./demoTable";
import type { DemoSummary } from "./payload";

const ME = "76561198230376396";
const ALT_A = "76561198000000001", ALT_B = "76561198000000002";
const me: Subject = { name: "You", ids: [ME] };
// A pro on two accounts: a game on either is theirs.
const pro: Subject = { name: "Pro", ids: [ALT_A, ALT_B] };

const demo = (over: Partial<DemoSummary>): DemoSummary => ({
  name: "x", map: "de_mirage", rounds: 20, players: 10, kb: 1000, seconds: 1800, ...over,
});

// Roster 1, score [roster A, roster B] = [7, 13]: a 13-7 win from roster 1.
const nuke = demo({
  name: "nukepug", map: "de_nuke", date: "2026-08-14", score: [7, 13],
  lines: { [ME]: [10, 14, 1, 1, 63.5] },
});
const anubis = demo({
  name: "anubispug", map: "de_anubis", date: "2026-08-13", score: [13, 6],
  lines: { [ME]: [23, 10, 4, 1, 105] },
});
const proGame = demo({
  name: "pro1", score: [13, 9], sample: true, kind: "pug",
  lines: { [ALT_B]: [32, 17, 1, 0, 141.2] },
});
const owned = demo({ name: "scrim", owner: ME, score: [13, 11], lines: {} });

describe("lineOf / resultOf", () => {
  it("reads the result from the subject's side of the score", () => {
    expect(resultOf(nuke, me)).toEqual({ wl: "W", us: 13, them: 7 });
    expect(resultOf(anubis, me)).toEqual({ wl: "L", us: 6, them: 13 });
  });

  it("finds a pro on any of their accounts", () => {
    expect(lineOf(proGame, pro)).toEqual([32, 17, 1, 0, 141.2]);
    expect(resultOf(proGame, pro)?.wl).toBe("W");
  });

  it("gives a score with no side for someone who was not there", () => {
    expect(lineOf(nuke, pro)).toBeNull();
    expect(resultOf(nuke, pro)).toEqual({ wl: "", us: 7, them: 13 });
  });

  it("has no result for a demo indexed before scores were", () => {
    expect(resultOf(demo({}), me)).toBeNull();
  });
});

describe("yourDemos / sampleDemos", () => {
  const index = [nuke, anubis, proGame, owned];

  it("lists games you played in and demos you own", () => {
    expect(yourDemos(index, ME).map((d) => d.name)).toEqual(["nukepug", "anubispug", "scrim"]);
  });

  it("lists only declared samples for visitors", () => {
    expect(sampleDemos(index).map((d) => d.name)).toEqual(["pro1"]);
  });
});

describe("applyFilters", () => {
  const all = [nuke, anubis, owned];

  it("narrows by map and by result from the subject's side", () => {
    expect(applyFilters(all, { ...NO_FILTERS, map: "de_nuke" }, me)).toEqual([nuke]);
    expect(applyFilters(all, { ...NO_FILTERS, result: "L" }, me)).toEqual([anubis]);
  });

  // "Played in August" cannot be true of a game nobody dated.
  it("drops undated demos once a date range is set", () => {
    expect(applyFilters(all, { ...NO_FILTERS, from: "2026-08-14" }, me)).toEqual([nuke]);
    expect(applyFilters(all, { ...NO_FILTERS, to: "2026-08-13" }, me)).toEqual([anubis]);
  });
});

describe("sortDemos", () => {
  const label = (d: DemoSummary) => d.name;

  it("defaults to newest first, undated last", () => {
    expect(sortDemos([owned, anubis, nuke], null, me, label).map((d) => d.name))
      .toEqual(["nukepug", "anubispug", "scrim"]);
  });

  it("sorts by the subject's numbers", () => {
    expect(sortDemos([nuke, anubis], { key: "adr", dir: -1 }, me, label)[0]).toBe(anubis);
    expect(sortDemos([nuke, anubis], { key: "result", dir: -1 }, me, label)[0]).toBe(nuke);
  });

  it("puts games the subject has no line in below every number", () => {
    expect(sortDemos([owned, nuke], { key: "kda", dir: -1 }, me, label)[0]).toBe(nuke);
  });
});

describe("recordOf", () => {
  // 63.5 over 20 rounds and 105 over 20: 84.25 across all 40, an average of
  // rounds rather than of games.
  it("weights ADR by rounds played", () => {
    const r = recordOf([nuke, { ...anubis, rounds: 20 }], me);
    expect(r).toEqual({ wins: 1, losses: 1, adr: 84.25 });
  });
});
