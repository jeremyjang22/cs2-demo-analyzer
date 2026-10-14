import { describe, expect, it } from "vitest";
import {
  assertAnalytics, clutchesByEnemies, clutchRows, conversionGrid, NO_FILTER, openingRows,
  openingSummary, percent, playerRows, record, roundPath, roundsFor, weaponPlayerRows, weaponRows,
  type Analytics,
} from "./analytics";

/**
 * Three rounds. Team 0 is {a, b} and starts on T; the sides swap for round 3.
 *
 *   1  team 0 on T, wins.  b opens on x; 2v1.
 *   2  team 0 on T, loses. x opens on a; b is left 1v2 after the plant.
 *   3  team 0 on CT, wins. y opens on a; b wins the 1v2.
 */
const game: Analytics = {
  version: 2,
  demo: "demo",
  map: "de_test",
  rounds_uncertain: 0,
  teams: ["Team 1", "Team 2"],
  players: [
    { id: "a", name: "Ann", team: 0, rounds: 3 },
    { id: "b", name: "Bo", team: 0, rounds: 3 },
    { id: "x", name: "Xi", team: 1, rounds: 3 },
    { id: "y", name: "Yu", team: 1, rounds: 3 },
  ],
  rounds: [
    { n: 1, t: 0, winner: 0 },
    { n: 2, t: 0, winner: 1 },
    { n: 3, t: 1, winner: 0 },
  ],
  kills: [
    { round: 1, at: 10, team: 1, victim: "x", killer: "b", assister: "", weapon: "AK-47", headshot: true, planted: false },
    { round: 1, at: 20, team: 1, victim: "y", killer: "b", assister: "a", weapon: "AK-47", headshot: true, planted: false },
    { round: 2, at: 12, team: 0, victim: "a", killer: "x", assister: "", weapon: "AK-47", headshot: false, planted: true },
    { round: 2, at: 30, team: 0, victim: "b", killer: "x", assister: "y", weapon: "AK-47", headshot: true, planted: true },
    { round: 3, at: 8, team: 0, victim: "a", killer: "y", assister: "", weapon: "M4A4", headshot: false, planted: false },
    { round: 3, at: 20, team: 1, victim: "x", killer: "b", assister: "", weapon: "M4A4", headshot: false, planted: false },
    // Nobody's kill: y fell. Still y's death.
    { round: 3, at: 30, team: 1, victim: "y", killer: "", assister: "", weapon: "World", headshot: false, planted: false },
  ],
  openings: [
    { round: 1, at: 10, team: 0, killer: "b", victim: "x", weapon: "AK-47", headshot: true },
    { round: 2, at: 12, team: 1, killer: "x", victim: "a", weapon: "AK-47", headshot: false },
    { round: 3, at: 8, team: 1, killer: "y", victim: "a", weapon: "M4A4", headshot: false },
  ],
  clutches: [
    { round: 1, at: 10, team: 1, player: "y", enemies: ["a", "b"], planted: false, kills: 0, survived: false },
    { round: 2, at: 12, team: 0, player: "b", enemies: ["x", "y"], planted: true, kills: 0, survived: false },
    { round: 3, at: 8, team: 0, player: "b", enemies: ["x", "y"], planted: false, kills: 2, survived: true },
  ],
  situations: [
    { round: 1, at: 0, alive: [["a", "b"], ["x", "y"]], planted: false },
    { round: 1, at: 10, alive: [["a", "b"], ["y"]], planted: false },
    { round: 2, at: 0, alive: [["a", "b"], ["x", "y"]], planted: false },
    { round: 2, at: 12, alive: [["b"], ["x", "y"]], planted: true },
    { round: 3, at: 0, alive: [["a", "b"], ["x", "y"]], planted: false },
    { round: 3, at: 8, alive: [["b"], ["x", "y"]], planted: false },
    { round: 3, at: 20, alive: [["b"], ["y"]], planted: false },
  ],
};

describe("record", () => {
  it("counts a team's rounds on each side", () => {
    expect(record(game, 0, "all")).toEqual({ n: 3, wins: 2 });
    expect(record(game, 0, "T")).toEqual({ n: 2, wins: 1 });
    expect(record(game, 0, "CT")).toEqual({ n: 1, wins: 1 });
  });

  // The two teams' records are the same rounds seen from each end.
  it("mirrors between the teams", () => {
    expect(record(game, 1, "all")).toEqual({ n: 3, wins: 1 });
    expect(record(game, 1, "CT")).toEqual({ n: 2, wins: 1 });
  });
});

describe("roundsFor", () => {
  const ns = (...args: Parameters<typeof roundsFor>) => roundsFor(...args).map((r) => r.n);

  it("narrows by side and by result, from the chosen team's end", () => {
    expect(ns(game, 0, "all", "all")).toEqual([1, 2, 3]);
    expect(ns(game, 0, "T", "all")).toEqual([1, 2]);
    expect(ns(game, 0, "all", "won")).toEqual([1, 3]);
    expect(ns(game, 0, "T", "lost")).toEqual([2]);
  });

  // The other team was on the other side and had the other result.
  it("flips with the team", () => {
    expect(ns(game, 1, "CT", "won")).toEqual([2]);
    expect(ns(game, 1, "all", "lost")).toEqual([1, 3]);
  });
});

describe("openings", () => {
  it("splits rounds by who got the first kill", () => {
    const s = openingSummary(openingRows(game, 0, NO_FILTER));
    expect(s.got).toEqual({ n: 1, wins: 1 });
    // Conceded twice, and still won round 3 off the clutch.
    expect(s.conceded).toEqual({ n: 2, wins: 1 });
  });

  // A player's openings are the ones they were in — whichever end of it.
  it("keeps a player's kills and their deaths", () => {
    const rows = openingRows(game, 0, { ...NO_FILTER, player: "a" });
    expect(rows.map((r) => [r.round, r.got])).toEqual([[2, false], [3, false]]);
  });

  it("follows the side swap", () => {
    expect(openingRows(game, 0, { ...NO_FILTER, side: "CT" }).map((r) => r.round)).toEqual([3]);
  });
});

describe("clutches", () => {
  it("is won by the round, and only lists the chosen team's", () => {
    const rows = clutchRows(game, 0, NO_FILTER);
    expect(rows.map((r) => [r.round, r.won])).toEqual([[2, false], [3, true]]);
    expect(clutchesByEnemies(rows)[1]).toEqual({ n: 2, wins: 1 });
  });

  it("narrows by the plant", () => {
    expect(clutchRows(game, 0, { ...NO_FILTER, plant: "post" }).map((r) => r.round)).toEqual([2]);
    expect(clutchRows(game, 0, { ...NO_FILTER, plant: "pre" }).map((r) => r.round)).toEqual([3]);
  });

  // Five slots whatever happened, so the 1v3 column is always the third one.
  it("always returns 1v1 through 1v5", () => {
    expect(clutchesByEnemies([])).toHaveLength(5);
  });
});

describe("conversionGrid", () => {
  it("tallies each count from the chosen team's side", () => {
    const grid = conversionGrid(game, 0, NO_FILTER);
    expect(grid.get("2v2")).toMatchObject({ n: 3, wins: 2 });
    expect(grid.get("2v1")).toMatchObject({ n: 1, wins: 1 });
    expect(grid.get("1v2")).toMatchObject({ n: 2, wins: 1 });
  });

  // The same moments from the other end: their 2v1 is our 1v2, lost when we won.
  it("is the mirror image for the other team", () => {
    const grid = conversionGrid(game, 1, NO_FILTER);
    expect(grid.get("2v1")).toMatchObject({ n: 2, wins: 1 });
    expect(grid.get("1v2")).toMatchObject({ n: 1, wins: 0 });
  });

  // "a" died first in rounds 2 and 3, so was never part of a 1v2.
  it("keeps only the counts a player was alive in", () => {
    const grid = conversionGrid(game, 0, { ...NO_FILTER, player: "a" });
    expect(grid.get("2v2")).toMatchObject({ n: 3 });
    expect(grid.has("1v2")).toBe(false);
  });

  it("lists the rounds behind a cell, with who was up", () => {
    const rows = conversionGrid(game, 0, NO_FILTER).get("1v2")?.rows ?? [];
    expect(rows.map((r) => [r.round, r.side, r.planted, r.won])).toEqual([
      [2, "T", true, false],
      [3, "CT", false, true],
    ]);
    expect(rows[0].own).toEqual(["b"]);
    expect(rows[0].enemy).toEqual(["x", "y"]);
  });
});

describe("playerRows", () => {
  it("puts the analytics columns beside the scoreboard, best first", () => {
    const [bo, ann] = playerRows(game, 0, NO_FILTER);
    expect(bo).toMatchObject({
      name: "Bo", kills: 3, deaths: 1, assists: 0, headshots: 2,
      openingKills: 1, clutches: { n: 2, wins: 1 },
    });
    expect(bo.topWeapon).toEqual({ weapon: "AK-47", kills: 2, headshots: 2 });
    expect(ann).toMatchObject({ name: "Ann", kills: 0, deaths: 2, assists: 1, openingDeaths: 2 });
    expect(ann.topWeapon).toBeNull();
  });

  // The reason the payload carries kills rather than totals: a scoreboard
  // that stays put when the side changes is showing the wrong game.
  it("re-sums K / D / A for one side", () => {
    const kda = (side: "T" | "CT", name: string) => {
      const p = playerRows(game, 0, { ...NO_FILTER, side }).find((r) => r.name === name);
      return p && [p.kills, p.deaths, p.assists];
    };
    expect(kda("T", "Bo")).toEqual([2, 1, 0]);
    expect(kda("CT", "Bo")).toEqual([1, 0, 0]);
    expect(kda("T", "Ann")).toEqual([0, 1, 1]);
    // Team 1 was on CT while team 0 was on T.
    const xi = playerRows(game, 1, { ...NO_FILTER, side: "CT" }).find((r) => r.name === "Xi");
    expect(xi).toMatchObject({ kills: 2, deaths: 1 });
  });

  it("narrows by the plant", () => {
    const [xi] = playerRows(game, 1, { ...NO_FILTER, plant: "post" });
    expect(xi).toMatchObject({ name: "Xi", kills: 2, deaths: 0 });
  });

  // A death nobody is credited with is still a death.
  it("counts a fall as a death and as nobody's kill", () => {
    const yu = playerRows(game, 1, NO_FILTER).find((r) => r.name === "Yu");
    expect(yu).toMatchObject({ kills: 1, deaths: 2, assists: 1 });
  });

  // Selecting a player must not blank everyone else's row in the table that
  // is used to select them.
  it("ignores the player filter", () => {
    const rows = playerRows(game, 0, { ...NO_FILTER, player: "a" });
    expect(rows.find((r) => r.name === "Bo")?.kills).toBe(3);
  });
});

describe("weaponRows", () => {
  it("counts a team's kills by weapon, most used first", () => {
    expect(weaponRows(game, 0, NO_FILTER)).toEqual([
      { weapon: "AK-47", kills: 2, headshots: 2 },
      { weapon: "M4A4", kills: 1, headshots: 0 },
    ]);
  });

  it("follows the side and the player", () => {
    expect(weaponRows(game, 0, { ...NO_FILTER, side: "CT" })).toEqual([
      { weapon: "M4A4", kills: 1, headshots: 0 },
    ]);
    expect(weaponRows(game, 0, { ...NO_FILTER, player: "a" })).toEqual([]);
  });

  // The fall has a weapon name but no killer, so it is no team's kill.
  it("leaves out deaths nobody is credited with", () => {
    expect(weaponRows(game, 0, NO_FILTER).some((w) => w.weapon === "World")).toBe(false);
  });
});

describe("weaponPlayerRows", () => {
  it("splits one weapon's kills between the players who got them", () => {
    expect(weaponPlayerRows(game, 1, NO_FILTER, "AK-47")).toEqual([
      { player: "x", kills: 2, headshots: 1 },
    ]);
    expect(weaponPlayerRows(game, 0, NO_FILTER, "AK-47")).toEqual([
      { player: "b", kills: 2, headshots: 2 },
    ]);
  });

  // The rows under a weapon have to add up to the bar they hang off.
  it("adds up to the team's total for that weapon", () => {
    for (const w of weaponRows(game, 0, NO_FILTER)) {
      const sum = weaponPlayerRows(game, 0, NO_FILTER, w.weapon).reduce((n, r) => n + r.kills, 0);
      expect(sum).toBe(w.kills);
    }
  });

  it("follows the side filter and ignores the player one", () => {
    expect(weaponPlayerRows(game, 0, { ...NO_FILTER, side: "CT" }, "AK-47")).toEqual([]);
    expect(weaponPlayerRows(game, 0, { ...NO_FILTER, player: "a" }, "AK-47")).toHaveLength(1);
  });
});

describe("roundPath", () => {
  it("walks a round from its first count to its last, naming who died", () => {
    const path = roundPath(game, 0, 3);
    expect(path).toMatchObject({ round: 3, side: "CT", won: true });
    expect(path?.steps.map((s) => [s.own, s.enemy])).toEqual([[2, 2], [1, 2], [1, 1], [1, 0]]);
    expect(path?.steps[0].deaths).toEqual([]);
    expect(path?.steps[1].deaths).toEqual([
      { victim: "a", killer: "y", weapon: "M4A4", headshot: false, own: true },
    ]);
    expect(path?.steps[2].deaths[0]).toMatchObject({ victim: "x", killer: "b", own: false });
  });

  // No recorded count has an empty side, so the step that ends a round by
  // elimination has to be rebuilt from the deaths that are left over.
  it("ends on the elimination, including a death nobody is credited with", () => {
    const last = roundPath(game, 0, 3)?.steps.at(-1);
    expect(last).toMatchObject({ own: 1, enemy: 0, at: 30 });
    expect(last?.deaths).toEqual([
      { victim: "y", killer: "", weapon: "World", headshot: false, own: false },
    ]);
  });

  it("is the same walk from the other team, turned around", () => {
    const path = roundPath(game, 1, 3);
    expect(path).toMatchObject({ side: "T", won: false });
    expect(path?.steps.map((s) => [s.own, s.enemy])).toEqual([[2, 2], [2, 1], [1, 1], [0, 1]]);
    expect(path?.steps[1].deaths[0].own).toBe(false);
  });

  // Round 2 ends 0v2 with the last T killed after the plant.
  it("carries the plant onto the steps after it", () => {
    expect(roundPath(game, 0, 2)?.steps.map((s) => s.planted)).toEqual([false, true, true]);
  });

  // A death after the round was won changed nothing about how it went.
  it("leaves postround deaths off the path", () => {
    const withSave: Analytics = {
      ...game,
      kills: [...game.kills, {
        round: 1, at: 40, team: 0, victim: "a", killer: "y", assister: "",
        weapon: "AK-47", headshot: false, planted: false, post: true,
      }],
    };
    expect(roundPath(withSave, 0, 1)?.steps.map((s) => [s.own, s.enemy])).toEqual([[2, 2], [2, 1], [2, 0]]);
  });

  it("has no path for a round it has no counts for", () => {
    expect(roundPath(game, 0, 99)).toBeNull();
    expect(roundPath({ ...game, situations: [] }, 0, 1)).toBeNull();
  });
});

describe("percent", () => {
  // No attempts is not a 0% success rate.
  it("shows a dash rather than a rate for nothing", () => {
    expect(percent({ n: 0, wins: 0 })).toBe("—");
    expect(percent({ n: 3, wins: 2 })).toBe("67%");
  });
});

describe("assertAnalytics", () => {
  it("accepts a well-formed payload", () => {
    expect(() => assertAnalytics(game)).not.toThrow();
  });

  it("names what is wrong with a bad one", () => {
    expect(() => assertAnalytics(null)).toThrow(/not an object/);
    expect(() => assertAnalytics({ ...game, version: 1 })).toThrow(/version 1/);
    expect(() => assertAnalytics({ version: 2, teams: [] })).toThrow(/players/);
  });
});
