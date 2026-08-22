import { describe, expect, it } from "vitest";
import { makeProjector, radarImage } from "./radar";
import type { RadarMeta } from "./types";

// Real de_mirage values, straight out of its overview .txt.
const MIRAGE: RadarMeta = { pos_x: -3230, pos_y: 1713, scale: 5, size: 1024 };

describe("makeProjector", () => {
  it("maps the top-left world corner to pixel origin", () => {
    const p = makeProjector(MIRAGE, 1024);
    expect(p.x(MIRAGE.pos_x)).toBe(0);
    expect(p.y(MIRAGE.pos_y)).toBe(0);
  });

  it("maps the bottom-right world corner to the far edge", () => {
    const p = makeProjector(MIRAGE, 1024);
    expect(p.x(-3230 + 1024 * 5)).toBe(1024);
    expect(p.y(1713 - 1024 * 5)).toBe(1024);
  });

  // Ground truth from the reference demo: round 1's first CT freeze tick sits
  // at world (-1656, -1976) with place = "CTSpawn". de_mirage.txt declares the
  // CT spawn icon at normalised (0.28, 0.70) — approximate, since it is a
  // loading-screen placement rather than a survey point, so the tolerance is
  // loose. It is still far tighter than any sign error: flipping y lands this
  // at -0.72, and dropping the flip lands it at -0.05.
  it("places a real CT spawn tick in the CT spawn corner", () => {
    const p = makeProjector(MIRAGE, 1024);
    expect(p.x(-1656) / 1024).toBeCloseTo(0.28, 1);
    expect(p.y(-1976) / 1024).toBeCloseTo(0.70, 1);
  });

  it("flips y — north is a smaller pixel value", () => {
    const p = makeProjector(MIRAGE, 1024);
    expect(p.y(1000)).toBeLessThan(p.y(-1000));
  });

  it("scales to the panel size", () => {
    const full = makeProjector(MIRAGE, 1024);
    const half = makeProjector(MIRAGE, 512);
    expect(half.x(-1776)).toBeCloseTo(full.x(-1776) / 2);
    expect(half.y(-1800)).toBeCloseTo(full.y(-1800) / 2);
  });
});

describe("radarImage", () => {
  it("uses the bare name for the default floor", () => {
    expect(radarImage("de_mirage", "default")).toBe("de_mirage.png");
  });

  it("suffixes other floors", () => {
    expect(radarImage("de_nuke", "lower")).toBe("de_nuke_lower.png");
  });
});
