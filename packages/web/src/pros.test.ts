import { describe, expect, it } from "vitest";
import { demoLabel, demosFor, facets, mapLabel } from "./pros";
import type { DemoSummary } from "./payload";

function demo(over: Partial<DemoSummary>): DemoSummary {
  return { name: "x", map: "de_mirage", rounds: 20, players: 10, kb: 1, seconds: 1, games: 1, ...over };
}

const demos = [
  demo({ name: "a", kind: "pug", pros: ["m0nesy"] }),
  demo({ name: "b", kind: "match", pros: ["m0nesy", "zywoo"], map: "de_inferno", rounds: 30 }),
  demo({ name: "c", kind: "pug", pros: ["zywoo"] }),
  demo({ name: "merged", kind: "pug", pros: ["m0nesy"], games: 5 }),
  demo({ name: "old", pros: ["m0nesy"] }),
];

describe("demosFor", () => {
  it("finds a pro's demos and leaves merged payloads out", () => {
    expect(demosFor(demos, "m0nesy").map((d) => d.name)).toEqual(["a", "b", "old"]);
  });

  it("narrows by kind and map", () => {
    expect(demosFor(demos, "m0nesy", { kind: "pug" }).map((d) => d.name)).toEqual(["a"]);
    expect(demosFor(demos, "m0nesy", { kind: "match", map: "de_inferno" }).map((d) => d.name)).toEqual(["b"]);
    expect(demosFor(demos, "m0nesy", { kind: "all", map: "de_mirage" }).map((d) => d.name)).toEqual(["a", "old"]);
  });

  // A pro match is on every catalogued player's page, from one demo.
  it("puts one pro match under every pro in it", () => {
    expect(demosFor(demos, "zywoo").map((d) => d.name)).toEqual(["b", "c"]);
  });
});

describe("facets", () => {
  it("counts kinds, rounds and maps, most played first", () => {
    expect(facets(demosFor(demos, "m0nesy"))).toEqual({
      total: 3,
      rounds: 70,
      kinds: { pug: 1, match: 1, unknown: 1 },
      maps: [["de_mirage", 2], ["de_inferno", 1]],
    });
  });
});

describe("labels", () => {
  it("prefers a title, then an event, then a short FACEIT id", () => {
    expect(demoLabel(demo({ title: "Falcons vs Vitality" }))).toBe("Falcons vs Vitality");
    expect(demoLabel(demo({ kind: "match", event: "IEM Example" }))).toBe("IEM Example");
    expect(demoLabel(demo({ name: "1-604132bd-e063-4e28-acfa-c2b4dc1d670c-1-1" }))).toBe("FACEIT 604132bd");
    expect(demoLabel(demo({ name: "nukepug" }))).toBe("nukepug");
  });

  it("drops the map prefix", () => {
    expect(mapLabel("de_mirage")).toBe("Mirage");
  });
});
