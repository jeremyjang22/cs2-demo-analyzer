import { describe, expect, it } from "vitest";
import {
  CT, T, defaultPlayer, flownTo, longest, playersIn, snapshot, startsFor, summarize, trailTo,
  type Source,
} from "./starters";
import type { KillEvent, Payload, RoundMeta, RoundTrack, TrajectoryEvent } from "./types";

const HZ = 4;
const ME = "76561198000000001";
const FOE = "76561198000000002";

function track(over: Partial<RoundTrack> = {}): RoundTrack {
  return {
    r: 1, dm: 0, k: "0:1", s: T, d: -1, sv: 1,
    x: [0, 100, 200, 300, 400],
    y: [0, 0, 0, 0, 0],
    yaw: [0, 0, 0, 0, 0],
    air: "00000", sc: "00000", lvl: "00000",
    ...over,
  };
}

function meta(over: Partial<RoundMeta> = {}): RoundMeta {
  return { n: 1, dm: 0, t0: 0, dur: 1, w: T, why: "", sa: 0, sb: 0, aw: 1, ok: 1, ...over };
}

function kill(over: Partial<KillEvent>): KillEvent {
  return { t: 0, k: null, v: FOE, a: null, w: "ak47", hs: 0, wb: 0, ...over };
}

function throwOf(over: Partial<TrajectoryEvent> = {}): TrajectoryEvent {
  return {
    t: 0.5, t1: 1, kind: "smoke", by: ME, team: T,
    x: [0, 50, 100], y: [0, 0, 0], ts: [0, 20, 40], lvl: "000", ...over,
  };
}

function payload(): Payload {
  return {
    map: "de_mirage",
    radar: { pos_x: 0, pos_y: 0, scale: 1, size: 1024 },
    tick_rate: 64, sample_hz: HZ, max_sec: 1, sections: ["default"],
    demos: [{ i: 0, label: "a", rounds: 3 }],
    rounds: {
      "0:1": meta({ n: 1, w: T }),
      "0:2": meta({ n: 2, w: CT }),
      "0:3": meta({ n: 3, w: T, ok: 0 }),
      "0:13": meta({ n: 13, w: CT }),
    },
    kills: {
      "0:1": [kill({ t: 0.5, k: ME, v: FOE, hs: 1 })],
      "0:2": [kill({ t: 0.25, k: FOE, v: ME })],
    },
    util: {},
    traj: { "0:1": [throwOf(), throwOf({ by: FOE })] },
    teams: [],
    stats: {},
    players: [
      {
        id: ME, name: "me", team: 0, col: 0, side0: T,
        rounds: [
          track({ k: "0:2", r: 2, d: 1 }),
          track({ k: "0:1", r: 1 }),
          track({ k: "0:3", r: 3 }),
          track({ k: "0:13", r: 13, s: CT }),
        ],
      },
      {
        id: FOE, name: "foe", team: 1, col: 0, side0: CT,
        rounds: [track({ k: "0:1", s: CT, x: [900, 900, 900, 900, 900] })],
      },
    ],
  };
}

/** One payload as a one-source set, the common case in these tests. */
const one = (): Source[] => [{ name: "g1", payload: payload() }];

describe("startsFor", () => {
  it("keeps one side, in round order, and drops truncated rounds", () => {
    const got = startsFor(one(), [ME], T);
    // Round 3 is the demo's cut-off last round; its track stops mid-route.
    expect(got.map((s) => s.round)).toEqual([1, 2]);
    expect(startsFor(one(), [ME], CT).map((s) => s.round)).toEqual([13]);
  });

  it("reads the result against the player's own side", () => {
    const [r1, r2] = startsFor(one(), [ME], T);
    expect(r1.won).toBe(true);
    expect(r2.won).toBe(false);
  });

  // A kill is drawn where the VICTIM was, not where the shooter stood - the
  // shooter's spot is already on the trail.
  it("places a kill at the victim's position", () => {
    const [r1] = startsFor(one(), [ME], T);
    expect(r1.kills).toEqual([{ t: 0.5, x: 900, y: 0, weapon: "ak47", hs: true }]);
  });

  it("tells opening duels won from lost", () => {
    const [r1, r2] = startsFor(one(), [ME], T);
    expect(r1.opening).toBe("won");
    expect(r2.opening).toBe("lost");
    expect(r2.diedAt).toBe(0.25);
  });

  it("keeps only the player's own grenades", () => {
    const [r1] = startsFor(one(), [ME], T);
    expect(r1.throws).toHaveLength(1);
  });

  // Two published games are one set: game numbers run on across them and the
  // ids stay unique, though both games have a round "0:1".
  it("numbers games across sources and keeps ids unique", () => {
    const got = startsFor([{ name: "g1", payload: payload() }, { name: "g2", payload: payload() }], [ME], T);
    expect(got.map((s) => `${s.game}/${s.round}`)).toEqual(["0/1", "0/2", "1/1", "1/2"]);
    expect(new Set(got.map((s) => s.id)).size).toBe(4);
    expect(got[2].id).toBe("g2/0:1");
  });

  // A pro on an alternate account in one game is still the same pro.
  it("follows whichever of the player's accounts is present", () => {
    expect(startsFor(one(), ["alt-account", ME], T)).toHaveLength(2);
  });

  it("returns nothing for an unknown player", () => {
    expect(startsFor(one(), ["nobody"], T)).toEqual([]);
  });
});

describe("trailTo", () => {
  it("ends at the interpolated position so the line meets the dot", () => {
    expect(trailTo(track(), 0.375, HZ)).toEqual([[0, 0], [100, 0], [150, 0]]);
  });

  // Dead players keep emitting rows at their corpse's position; a trail that
  // followed them would be a line that stops moving, not a route.
  it("stops at death", () => {
    expect(trailTo(track({ d: 2 }), 1, HZ)).toEqual([[0, 0], [100, 0], [200, 0]]);
  });

  it("never runs past the end of the track", () => {
    expect(trailTo(track(), 99, HZ)).toHaveLength(5);
  });
});

describe("flownTo", () => {
  it("uses the per-point centiseconds, not even spacing", () => {
    const e = throwOf();
    expect(flownTo(e, 0.4)).toEqual([]);
    expect(flownTo(e, 0.7)).toEqual([[0, 0], [50, 0]]);
    expect(flownTo(e, 5)).toHaveLength(3);
  });
});

describe("snapshot and summarize", () => {
  it("counts at a moment across every round", () => {
    const starts = startsFor(one(), [ME], T);
    expect(snapshot(starts, 0, HZ)).toEqual({ rounds: 2, alive: 2, kills: 0, deaths: 0, thrown: 0 });
    expect(snapshot(starts, 0.5, HZ)).toEqual({ rounds: 2, alive: 1, kills: 1, deaths: 1, thrown: 1 });
  });

  it("summarizes the whole set", () => {
    expect(summarize(startsFor(one(), [ME], T))).toEqual({
      rounds: 2, won: 1, openingsWon: 1, openingsLost: 1, kills: 1, deaths: 1,
    });
  });

  it("measures the longest round for the slider", () => {
    expect(longest(startsFor(one(), [ME], T), HZ)).toBe(1);
  });
});

describe("defaults", () => {
  it("opens on whoever played the most rounds across the set", () => {
    expect(defaultPlayer(one())).toBe(ME);
  });

  it("joins players across sources by steamid", () => {
    const set = [{ name: "a", payload: payload() }, { name: "b", payload: payload() }];
    expect(playersIn(set).map((p) => [p.name, p.rounds])).toEqual([["me", 8], ["foe", 2]]);
  });
});
