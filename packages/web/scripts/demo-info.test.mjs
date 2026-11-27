import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { demoNameOf, infoProblems, measure, playSummary, validDate } from "./demo-info.mjs";

describe("infoProblems", () => {
  it("accepts a fully declared demo", () => {
    expect(infoProblems({
      kind: "pug", visibility: "public", sample: true, date: "2026-09-02",
    })).toEqual([]);
    expect(infoProblems({ visibility: "private", owner: "76561198230376396" })).toEqual([]);
  });

  it("rejects a sample that signed-out visitors could not open", () => {
    expect(infoProblems({ sample: true })).toHaveLength(1);
    expect(infoProblems({ sample: true, visibility: "private" })).toHaveLength(1);
  });

  it("rejects typos rather than publishing more widely than meant", () => {
    expect(infoProblems({ visibility: "Public" })).toHaveLength(1);
    expect(infoProblems({ owner: "123" })).toHaveLength(1);
    expect(infoProblems({ date: "2026-02-30" })).toHaveLength(1);
    expect(infoProblems({ kind: "faceit" })).toHaveLength(1);
  });
});

describe("validDate", () => {
  it("knows real dates from shapes", () => {
    expect(validDate("2026-05-11")).toBe(true);
    expect(validDate("2026-13-01")).toBe(false);
    expect(validDate("2026-5-11")).toBe(false);
    expect(validDate(undefined)).toBe(false);
  });
});

describe("playSummary", () => {
  const round = (n, sa, sb) => ({ n, sa, sb });
  const hit = (by, v, hpt) => ({ by, v, hpt, hp: hpt + 50 });
  const payload = {
    players: [{ id: "a", team: 0 }, { id: "b", team: 0 }, { id: "c", team: 1 }],
    rounds: { "0:2": round(2, 2, 0), "0:1": round(1, 1, 0) },
    stats: { a: { k: 3, d: 1, a: 0 }, c: { k: 1, d: 2, a: 1 } },
    damage: {
      "0:1": [hit("a", "c", 100), hit("a", "b", 40), hit(null, "a", 10)],
      "0:2": [hit("a", "c", 50), hit("c", "c", 20)],
    },
  };
  const s = playSummary(payload);

  it("takes the score after the last round, not the last key", () => {
    expect(s.score).toEqual([2, 0]);
  });

  // hpt, enemies only: 150 over 2 rounds. The 40 to teammate b, the world
  // damage and c's self-damage are nobody's ADR.
  it("averages enemy damage actually taken, per round", () => {
    expect(s.lines.a).toEqual([3, 1, 0, 0, 75]);
    expect(s.lines.c[4]).toBe(0);
  });

  it("gives every player a line, zeros for one with no stats", () => {
    expect(s.lines.b).toEqual([0, 0, 0, 0, 0]);
  });

  it("leaves merged payloads without a score", () => {
    expect(playSummary({ ...payload, demos: [{}, {}] })).toEqual({});
  });
});

describe("measure", () => {
  // The contract with the Go catalogue, which generates the published index:
  // packages/catalog's TestSummaryMatchesTheJavaScript checks its port
  // against the same two files. Regenerate summary.json only on purpose.
  it("matches the Go catalogue's fixture", () => {
    const dir = new URL("../../catalog/testdata/", import.meta.url);
    const bytes = readFileSync(new URL("movement.json", dir));
    const want = JSON.parse(readFileSync(new URL("summary.json", dir), "utf8"));
    expect(measure(JSON.parse(bytes), bytes.length)).toEqual(want);
  });
});

describe("demoNameOf", () => {
  it("strips .dem and .dem.zst", () => {
    expect(demoNameOf("nukepug.dem")).toBe("nukepug");
    expect(demoNameOf("1-abc-1-1.dem.zst")).toBe("1-abc-1-1");
    expect(demoNameOf("notes.txt")).toBeNull();
  });
});
