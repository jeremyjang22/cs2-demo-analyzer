import { describe, expect, it } from "vitest";
import { formatDate, monthGrid, pick, shiftMonth, validDate } from "./calendar";

describe("validDate", () => {
  it("accepts real days and rejects impossible ones", () => {
    expect(validDate("2026-09-02")).toBe(true);
    expect(validDate("2028-02-29")).toBe(true);
    expect(validDate("2026-02-29")).toBe(false);
    expect(validDate("2026-9-2")).toBe(false);
    expect(validDate("")).toBe(false);
  });
});

describe("monthGrid", () => {
  it("starts weeks on Monday", () => {
    // 1 Sep 2026 is a Tuesday.
    expect(monthGrid("2026-09").lead).toBe(1);
    // 1 Nov 2026 is a Sunday.
    expect(monthGrid("2026-11").lead).toBe(6);
  });

  it("lists every day of the month", () => {
    const feb = monthGrid("2028-02");
    expect(feb.days).toHaveLength(29);
    expect(feb.days[0]).toBe("2028-02-01");
    expect(feb.label).toBe("Feb 2028");
  });
});

describe("shiftMonth", () => {
  it("crosses year boundaries", () => {
    expect(shiftMonth("2026-12", 1)).toBe("2027-01");
    expect(shiftMonth("2026-01", -1)).toBe("2025-12");
  });
});

describe("formatDate", () => {
  it("reads like a date, not an ISO string", () => {
    expect(formatDate("2026-09-02")).toBe("2 Sep 2026");
  });
});

describe("pick", () => {
  const range = { from: "2026-09-10", to: "2026-09-20" };

  it("sets either end", () => {
    expect(pick(range, "from", "2026-09-12")).toEqual({ from: "2026-09-12", to: "2026-09-20" });
    expect(pick(range, "to", "2026-09-25")).toEqual({ from: "2026-09-10", to: "2026-09-25" });
  });

  it("clears the end when the start moves past it", () => {
    expect(pick(range, "from", "2026-09-25")).toEqual({ from: "2026-09-25", to: "" });
  });

  it("swaps when the end is picked before the start", () => {
    expect(pick(range, "to", "2026-09-01")).toEqual({ from: "2026-09-01", to: "2026-09-10" });
  });
});
