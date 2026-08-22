import { describe, expect, it } from "vitest";
import { deadAt, flagAt, idxAt, lengthSec, posAt, runs, yawAt } from "./track";
import type { RoundTrack } from "./types";

const HZ = 4;

function track(over: Partial<RoundTrack> = {}): RoundTrack {
  return {
    r: 1, dm: 0, k: "0:1", s: 2, d: -1, sv: 1,
    x: [0, 100, 200, 300, 400],
    y: [0, 0, 100, 100, 200],
    yaw: [0, 90, 180, -90, 0],
    air: "00000",
    sc: "00000",
    lvl: "00000",
    ...over,
  };
}

describe("idxAt", () => {
  it("converts seconds to sample index", () => {
    expect(idxAt(1, HZ)).toBe(4);
    expect(idxAt(0.25, HZ)).toBe(1);
  });

  // Playback can hand this a slightly negative time when a frame timestamp
  // arrives out of order. A negative index reads undefined out of the arrays,
  // NaNs every coordinate, and makes canvas gradients throw — which killed the
  // render loop on the first frame of playback.
  it("never returns a negative index", () => {
    expect(idxAt(-0.0144, HZ)).toBe(0);
    expect(idxAt(-1000, HZ)).toBe(0);
  });
});

describe("posAt", () => {
  it("returns the exact sample on a boundary", () => {
    expect(posAt(track(), 0.25, HZ)).toEqual([100, 0]);
  });

  it("interpolates between samples", () => {
    expect(posAt(track(), 0.125, HZ)).toEqual([50, 0]);
  });

  it("clamps past the end rather than reading undefined", () => {
    expect(posAt(track(), 999, HZ)).toEqual([400, 200]);
  });

  it("returns finite coordinates for a negative time", () => {
    const [x, y] = posAt(track(), -0.0144, HZ);
    expect(Number.isFinite(x)).toBe(true);
    expect(Number.isFinite(y)).toBe(true);
    expect([x, y]).toEqual([0, 0]);
  });
});

describe("yawAt", () => {
  it("interpolates the short way across the +/-180 wrap", () => {
    // 170 -> -170 is a 20 degree turn through 180, not a 340 degree one back
    // through zero. Halfway must land on 180 (or its equivalent, -180).
    const t = track({ yaw: [170, -170], x: [0, 1], y: [0, 1], air: "00", sc: "00", lvl: "00" });
    const mid = yawAt(t, 0.125, HZ);
    expect(Math.abs(((mid - 180 + 540) % 360) - 180)).toBeLessThan(1e-9);
  });

  it("interpolates normally when no wrap is involved", () => {
    expect(yawAt(track(), 0.125, HZ)).toBeCloseTo(45);
  });

  it("is finite for a negative time", () => {
    expect(Number.isFinite(yawAt(track(), -0.5, HZ))).toBe(true);
  });
});

describe("deadAt", () => {
  it("is false for a survivor", () => {
    expect(deadAt(track(), 999, HZ)).toBe(false);
  });

  it("flips at the death sample", () => {
    const t = track({ d: 2 });
    expect(deadAt(t, 0.25, HZ)).toBe(false); // index 1
    expect(deadAt(t, 0.5, HZ)).toBe(true);   // index 2
  });
});

describe("flagAt", () => {
  it("reads the flag for the current sample", () => {
    expect(flagAt("00110", 0.5, HZ)).toBe("1");
    expect(flagAt("00110", 0, HZ)).toBe("0");
  });

  it("clamps at both ends", () => {
    expect(flagAt("00110", -5, HZ)).toBe("0");
    expect(flagAt("00110", 999, HZ)).toBe("0");
  });
});

describe("lengthSec", () => {
  it("is sample count minus one over the rate", () => {
    expect(lengthSec(track(), HZ)).toBe(1);
  });
});

describe("runs", () => {
  it("returns one run when nothing changes", () => {
    expect(runs(track(), 0, 4)).toEqual([{ from: 0, to: 4, level: 0, airborne: false }]);
  });

  // A boundary produces its own short run for the crossing segment, attributed
  // to the floor/state it started on. So assert the properties that matter —
  // each run agrees with the flags at its start, and both states appear —
  // rather than an exact run count.
  it("splits on an airborne transition", () => {
    const t = track({ air: "00110" });
    const r = runs(t, 0, 4);
    for (const run of r) expect(run.airborne).toBe(t.air[run.from] === "1");
    expect(new Set(r.map((x) => x.airborne))).toEqual(new Set([false, true]));
  });

  it("splits on a floor change", () => {
    const t = track({ lvl: "00011" });
    const r = runs(t, 0, 4);
    for (const run of r) expect(run.level).toBe(Number(t.lvl[run.from]));
    expect(new Set(r.map((x) => x.level))).toEqual(new Set([0, 1]));
  });

  it("tiles the range with no gaps", () => {
    const r = runs(track({ air: "00110", lvl: "00011" }), 0, 4);
    for (let i = 1; i < r.length; i++) expect(r[i].from).toBe(r[i - 1].to);
  });

  // Without the overlap the boundary segment belongs to no run and is never
  // drawn — on Nuke that segment is the ramp or the vents.
  it("overlaps runs by one sample so no segment is dropped", () => {
    const r = runs(track({ lvl: "00011" }), 0, 4);
    expect(r[0].to).toBe(r[1].from);
  });

  it("covers the whole requested range", () => {
    const r = runs(track({ air: "01010" }), 0, 4);
    expect(r[0].from).toBe(0);
    expect(r[r.length - 1].to).toBe(4);
  });

  it("returns nothing for an empty range", () => {
    expect(runs(track(), 2, 2)).toEqual([]);
    expect(runs(track(), 3, 1)).toEqual([]);
  });

  it("always advances, even when flags alternate every sample", () => {
    const r = runs(track({ air: "01010" }), 0, 4);
    expect(r.length).toBeGreaterThan(0);
    for (const run of r) expect(run.to).toBeGreaterThan(run.from);
  });
});
