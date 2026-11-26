import { describe, expect, it } from "vitest";
import { canOpen, demoOfKey, isListed } from "./access";

const ME = "76561198230376396";
const SOMEONE = "76561198000000000";

describe("canOpen / isListed", () => {
  const mine = { name: "nukepug", visibility: "private", owner: ME };

  it("shows a private demo to its owner only", () => {
    expect(canOpen(mine, ME)).toBe(true);
    expect(isListed(mine, ME)).toBe(true);
    for (const viewer of [SOMEONE, null]) {
      expect(canOpen(mine, viewer)).toBe(false);
      expect(isListed(mine, viewer)).toBe(false);
    }
  });

  it("opens a link demo for any account but lists it only for its owner", () => {
    const link = { ...mine, visibility: "link" };
    expect(canOpen(link, SOMEONE)).toBe(true);
    expect(isListed(link, SOMEONE)).toBe(false);
    expect(isListed(link, ME)).toBe(true);
  });

  it("shows a public demo to every account", () => {
    const pub = { name: "m0nesy-1", visibility: "public" };
    expect(canOpen(pub, SOMEONE)).toBe(true);
    expect(isListed(pub, SOMEONE)).toBe(true);
  });

  // The pro demos are public to accounts; signed out, only the samples show.
  it("shows a signed-out visitor the samples and nothing else", () => {
    const sample = { name: "s", visibility: "public", sample: true };
    expect(canOpen(sample, null)).toBe(true);
    expect(isListed(sample, null)).toBe(true);
    for (const visibility of ["public", "link"]) {
      const entry = { name: "x", visibility };
      expect(canOpen(entry, null)).toBe(false);
      expect(isListed(entry, null)).toBe(false);
    }
  });

  it("does not let a sample flag open a demo that is not public", () => {
    for (const visibility of ["link", "private", undefined]) {
      const entry = { name: "x", visibility, owner: ME, sample: true };
      expect(canOpen(entry, null)).toBe(false);
      expect(isListed(entry, null)).toBe(false);
    }
  });

  // Fail closed: an index written before visibility existed, or a demo whose
  // info.json forgot to say, must not leak.
  it("treats an undeclared or unknown visibility as private", () => {
    for (const visibility of [undefined, "", "Public", "unlisted"]) {
      const entry = { name: "x", visibility, owner: ME };
      expect(canOpen(entry, SOMEONE)).toBe(false);
      expect(canOpen(entry, ME)).toBe(true);
    }
  });

  it("opens a private demo with no owner for nobody", () => {
    const orphan = { name: "x", visibility: "private" };
    expect(canOpen(orphan, ME)).toBe(false);
    expect(isListed(orphan, ME)).toBe(false);
  });

  it("does not serve a demo the index does not list", () => {
    expect(canOpen(undefined, ME)).toBe(false);
  });
});

describe("demoOfKey", () => {
  it("names the demo a payload belongs to", () => {
    expect(demoOfKey("data/nukepug/movement.json")).toBe("nukepug");
    expect(demoOfKey("data/nukepug/analytics.json")).toBe("nukepug");
  });

  it("is null for anything that is not a file in a demo folder", () => {
    for (const key of ["data/index.json", "data/a/b/c.json", "data//x.json",
      "other/nukepug/movement.json", "data/../movement.json"]) {
      expect(demoOfKey(key)).toBeNull();
    }
  });
});
