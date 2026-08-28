import { describe, expect, it } from "vitest";
import {
  arcsAt, bombAt, damageTaken, fadingAt, fuseProgress, kitsOnGround, pointReached,
  tracerEnd, TRAJECTORY_FADE_SEC,
} from "./effects";
import type { BombEvent, DamageEvent, KitEvent, ShotEvent, TrajectoryEvent } from "./types";

const shot = (t: number, over: Partial<ShotEvent> = {}): ShotEvent => ({
  t, by: "1", x: 0, y: 0, a: 0, lv: 0, ...over,
});

const bomb = (t: number, st: BombEvent["st"], over: Partial<BombEvent> = {}): BombEvent => ({
  t, st, by: null, x: 0, y: 0, lv: 0, ...over,
});

const hit = (t: number, victim: string, hp: number): DamageEvent => ({
  t, v: victim, by: "9", k: "bullet", hp, ar: 0, hg: 2, n: 1, x: 0, y: 0, lv: 0,
});

describe("fadingAt", () => {
  it("returns only events inside the window", () => {
    const shots = [shot(1), shot(2), shot(2.9), shot(4)];
    const visible = fadingAt(shots, 3, 1);
    expect(visible.map((v) => v.event.t)).toEqual([2, 2.9]);
  });

  it("ages an event from 0 at its moment to 1 at the end of its life", () => {
    const shots = [shot(10)];
    expect(fadingAt(shots, 10, 2)[0].age).toBe(0);
    expect(fadingAt(shots, 11, 2)[0].age).toBe(0.5);
    expect(fadingAt(shots, 12, 2)[0].age).toBe(1);
    expect(fadingAt(shots, 12.01, 2)).toHaveLength(0);
  });

  // Scrubbing backwards is the case a frame-to-frame accumulator gets wrong:
  // events that already played would linger with nothing to clear them.
  it("shows nothing before the first event, however it was reached", () => {
    const shots = [shot(30), shot(31)];
    expect(fadingAt(shots, 5, 1)).toHaveLength(0);
  });

  it("survives an empty round", () => {
    expect(fadingAt([], 12, 1)).toEqual([]);
  });

  // A zero life would otherwise divide by zero and hand the renderer NaN,
  // which canvas gradients throw on rather than skipping - the same class of
  // bug that posAt's clamp exists to prevent.
  it("does not produce NaN for a zero-length life", () => {
    const ages = fadingAt([shot(4)], 4, 0).map((v) => v.age);
    expect(ages).toEqual([1]);
  });
});

describe("bombAt", () => {
  const events = [
    bomb(0, "carried", { by: "a" }),
    bomb(12, "dropped"),
    bomb(20, "carried", { by: "b" }),
    bomb(35, "planted", { by: "b", site: "A" }),
    bomb(70, "defused", { by: "c" }),
  ];

  it("holds a state until the next one", () => {
    expect(bombAt(events, 5)?.st).toBe("carried");
    expect(bombAt(events, 19.9)?.st).toBe("dropped");
    expect(bombAt(events, 34)?.by).toBe("b");
    expect(bombAt(events, 69)?.site).toBe("A");
    expect(bombAt(events, 999)?.st).toBe("defused");
  });

  it("takes the state exactly at its own timestamp", () => {
    expect(bombAt(events, 35)?.st).toBe("planted");
  });

  // A payload written before bomb events existed has none, and "no bomb data"
  // must not be drawn as "a bomb at the world origin".
  it("returns null when there is nothing to report", () => {
    expect(bombAt([], 10)).toBeNull();
    expect(bombAt([bomb(5, "carried")], 1)).toBeNull();
  });
});

describe("fuseProgress", () => {
  const planted = bomb(30, "planted");

  it("runs 0 to 1 across the fuse", () => {
    expect(fuseProgress(planted, 30)).toBe(0);
    expect(fuseProgress(planted, 50)).toBe(0.5);
    expect(fuseProgress(planted, 70)).toBe(1);
  });

  it("clamps at both ends", () => {
    expect(fuseProgress(planted, 10)).toBe(0);
    expect(fuseProgress(planted, 500)).toBe(1);
  });
});

describe("tracerEnd", () => {
  it("stops on the victim when the shot hit somebody", () => {
    const end = tracerEnd(shot(1, { x: 100, y: 100, hx: 400, hy: 250 }), 900);
    expect(end).toEqual({ x: 400, y: 250, hit: true });
  });

  // World yaw runs counter-clockwise from +X. The canvas y-flip belongs to the
  // projector; doing it here too would mirror every miss.
  it("projects along world yaw when nothing recorded an endpoint", () => {
    const east = tracerEnd(shot(1, { x: 0, y: 0, a: 0 }), 100);
    expect(east.x).toBeCloseTo(100);
    expect(east.y).toBeCloseTo(0);
    expect(east.hit).toBe(false);

    const north = tracerEnd(shot(1, { x: 0, y: 0, a: 90 }), 100);
    expect(north.x).toBeCloseTo(0);
    expect(north.y).toBeCloseTo(100);
  });

  it("handles the yaw wrap boundary like any other angle", () => {
    const west = tracerEnd(shot(1, { x: 0, y: 0, a: -180 }), 100);
    expect(west.x).toBeCloseTo(-100);
    expect(west.y).toBeCloseTo(0);
  });

  // hx without hy would be a malformed payload; treating it as a hit would put
  // the endpoint at an undefined coordinate and NaN the draw.
  it("treats a half-present endpoint as a miss", () => {
    const end = tracerEnd({ ...shot(1, { x: 0, y: 0, a: 0 }), hx: 50 }, 100);
    expect(end.hit).toBe(false);
  });
});

describe("damageTaken", () => {
  it("sums each victim's damage over the window", () => {
    const window = fadingAt([hit(1, "a", 30), hit(1.2, "b", 15), hit(1.5, "a", 27)], 2, 2);
    const totals = damageTaken(window);
    expect(totals.get("a")).toBe(57);
    expect(totals.get("b")).toBe(15);
    expect(totals.has("c")).toBe(false);
  });
});

describe("kitsOnGround", () => {
  const kit = (t: number, ev: KitEvent["ev"], id: number): KitEvent =>
    ({ t, ev, id, by: "1", x: id * 100, y: 0, lv: 0 });

  it("puts a dropped kit on the floor and takes it off when claimed", () => {
    const events = [kit(10, "dropped", 1), kit(25, "taken", 1)];
    expect(kitsOnGround(events, 5)).toHaveLength(0);
    expect(kitsOnGround(events, 15).map((k) => k.id)).toEqual([1]);
    expect(kitsOnGround(events, 30)).toHaveLength(0);
  });

  // Most kits are never picked up. A drop with no take is the normal case and
  // must stay on the floor for the rest of the round.
  it("keeps an unclaimed kit for good", () => {
    expect(kitsOnGround([kit(10, "dropped", 1)], 500).map((k) => k.id)).toEqual([1]);
  });

  it("tracks several kits independently", () => {
    const events = [
      kit(10, "dropped", 1),
      kit(12, "dropped", 2),
      kit(20, "taken", 2),
      kit(30, "dropped", 3),
    ];
    expect(kitsOnGround(events, 15).map((k) => k.id).sort()).toEqual([1, 2]);
    expect(kitsOnGround(events, 35).map((k) => k.id).sort()).toEqual([1, 3]);
  });

  // Scrubbing backwards has to put a claimed kit back where it was. An
  // accumulator carried across frames would leave it gone.
  it("is a replay, not an accumulator", () => {
    const events = [kit(10, "dropped", 1), kit(25, "taken", 1)];
    kitsOnGround(events, 40);
    expect(kitsOnGround(events, 15).map((k) => k.id)).toEqual([1]);
  });

  // Kit ids only mean anything within one round, so a take with no drop
  // behind it is not evidence that some other kit left the floor.
  it("ignores a take with no matching drop", () => {
    const events = [kit(10, "dropped", 1), kit(20, "taken", 9)];
    expect(kitsOnGround(events, 30).map((k) => k.id)).toEqual([1]);
  });

  it("survives a round with no kits at all", () => {
    expect(kitsOnGround([], 60)).toEqual([]);
  });
});

describe("grenade arcs", () => {
  // Sampled by distance, so the times are uneven on purpose: fast at the
  // start, slowing as it lands. ts is centiseconds from t.
  const smoke: TrajectoryEvent = {
    t: 10, t1: 11, kind: "smoke", by: "1", team: 2,
    x: [0, 100, 180, 220], y: [0, 0, 0, 0], ts: [0, 20, 60, 100], lvl: "0000",
  };

  describe("pointReached", () => {
    it("walks the real timings rather than scaling by elapsed", () => {
      expect(pointReached(smoke, 10)).toBe(0);
      expect(pointReached(smoke, 10.2)).toBe(1);
      expect(pointReached(smoke, 10.5)).toBe(1);   // still before the 60cs point
      expect(pointReached(smoke, 10.6)).toBe(2);
      expect(pointReached(smoke, 11)).toBe(3);
    });

    // Half the flight time is NOT half the path: even interpolation would put
    // this at point 1 or 2 depending on rounding, and both are wrong.
    it("differs from an even split, which is the reason it exists", () => {
      const even = Math.round(((10.5 - smoke.t) / (smoke.t1 - smoke.t)) * (smoke.x.length - 1));
      expect(pointReached(smoke, 10.5)).not.toBe(even);
    });

    it("never runs off the end of the path", () => {
      expect(pointReached(smoke, 9999)).toBe(smoke.x.length - 1);
    });
  });

  describe("arcsAt", () => {
    it("shows nothing before the throw", () => {
      expect(arcsAt([smoke], 9.9)).toHaveLength(0);
    });

    it("grows the line as the grenade flies", () => {
      expect(arcsAt([smoke], 10.2)[0].upto).toBe(1);
      expect(arcsAt([smoke], 10.6)[0].upto).toBe(2);
      expect(arcsAt([smoke], 11)[0].upto).toBe(3);
    });

    it("is not fading while it is still in the air", () => {
      expect(arcsAt([smoke], 10.5)[0].fade).toBe(0);
    });

    // A grenade's flight is over in about a second. An arc that vanished on
    // landing would be a flicker you could watch a whole round and never see.
    it("lingers after it lands, then goes", () => {
      expect(arcsAt([smoke], 11 + TRAJECTORY_FADE_SEC / 2)[0].fade).toBeCloseTo(0.5);
      expect(arcsAt([smoke], 11 + TRAJECTORY_FADE_SEC)[0].fade).toBeCloseTo(1);
      expect(arcsAt([smoke], 11 + TRAJECTORY_FADE_SEC + 0.01)).toHaveLength(0);
    });

    it("survives a round with no grenades", () => {
      expect(arcsAt([], 30)).toEqual([]);
    });

    // The regression this exists for: trajectories were once ordered by
    // LANDING time, so an instant smoke thrown at 0.3s sat behind every flash
    // in the round. A scan that stopped at the first future event found the
    // flash, stopped, and hid the smoke for the whole flight — then showed it
    // late and left it up for twenty seconds.
    it("finds an early arc that is listed after a later one", () => {
      const late: TrajectoryEvent = {
        t: 7, t1: 8, kind: "flash", by: "2", team: 3,
        x: [0, 10], y: [0, 0], ts: [0, 100], lvl: "00",
      };
      const early: TrajectoryEvent = {
        t: 0.3, t1: 7.5, kind: "smoke", by: "1", team: 2,
        x: [0, 50], y: [0, 0], ts: [0, 720], lvl: "00",
      };

      const found = arcsAt([late, early], 1);
      expect(found.map((a) => a.event.kind)).toEqual(["smoke"]);
    });
  });
});
