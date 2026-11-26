import { describe, expect, it } from "vitest";
import { viewerOf } from "./session";

const KEY = "0123456789abcdef0123456789abcdef";
// Produced by the Go side's Encode, and asserted there in
// TestEncodeMatchesWorkerVector. If this breaks, the Worker and the API
// disagree about the cookie and every private demo is locked to everyone.
const VECTOR = "76561198230376396.4102444800.ZmtrVcXaMOlnOiKp3AXxjOyXOc5dYnTacJd9GogvYRI";
const BEFORE_EXPIRY = new Date("2050-01-01T00:00:00Z");

describe("viewerOf", () => {
  it("reads the steamid from a cookie the API signed", async () => {
    expect(await viewerOf(`sid=${VECTOR}`, KEY, BEFORE_EXPIRY)).toBe("76561198230376396");
  });

  it("finds the cookie among others", async () => {
    expect(await viewerOf(`theme=dark; sid=${VECTOR}; x=1`, KEY, BEFORE_EXPIRY))
      .toBe("76561198230376396");
  });

  it("rejects a cookie whose steamid was edited", async () => {
    const forged = VECTOR.replace("76561198230376396", "76561198230376397");
    expect(await viewerOf(`sid=${forged}`, KEY, BEFORE_EXPIRY)).toBeNull();
  });

  it("rejects a cookie signed with another key", async () => {
    expect(await viewerOf(`sid=${VECTOR}`, "f".repeat(32), BEFORE_EXPIRY)).toBeNull();
  });

  it("rejects an expired cookie", async () => {
    expect(await viewerOf(`sid=${VECTOR}`, KEY, new Date("2100-01-01T00:00:01Z"))).toBeNull();
  });

  it("treats a Worker without the key as nobody being signed in", async () => {
    expect(await viewerOf(`sid=${VECTOR}`, undefined, BEFORE_EXPIRY)).toBeNull();
    expect(await viewerOf(`sid=${VECTOR}`, "", BEFORE_EXPIRY)).toBeNull();
  });

  it("rejects missing and malformed cookies", async () => {
    for (const header of [null, "", "other=1", "sid=", "sid=a.b", "sid=a.b.c.d",
      "sid=x.4102444800.sig", "sid=1.2.!!!"]) {
      expect(await viewerOf(header, KEY, BEFORE_EXPIRY)).toBeNull();
    }
  });
});
