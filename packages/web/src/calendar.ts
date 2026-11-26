/**
 * Calendar arithmetic for the date-range picker, on "YYYY-MM-DD" strings.
 *
 * Dates here are calendar days, not instants: a demo's date is the day it was
 * played where it was played, as declared. Doing the arithmetic in UTC keeps
 * a viewer's own timezone from shifting a day across midnight.
 */

export const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
export const WEEKDAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];

/** A real calendar date in YYYY-MM-DD form - not just the right shape. */
export function validDate(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1, d));
  return t.getUTCFullYear() === y && t.getUTCMonth() === m - 1 && t.getUTCDate() === d;
}

/** "2026-09-02" → "2 Sep 2026". */
export function formatDate(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  return `${d} ${MONTHS[m - 1]} ${y}`;
}

/** "YYYY-MM" moved by n months. */
export function shiftMonth(ym: string, n: number): string {
  const [y, m] = ym.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1 + n, 1));
  return `${t.getUTCFullYear()}-${String(t.getUTCMonth() + 1).padStart(2, "0")}`;
}

export interface Month {
  /** "Sep 2026" */
  label: string;
  /** Blank cells before the 1st, in a week that starts on Monday. */
  lead: number;
  /** Every day of the month, as YYYY-MM-DD. */
  days: string[];
}

export function monthGrid(ym: string): Month {
  const [y, m] = ym.split("-").map(Number);
  const lead = (new Date(Date.UTC(y, m - 1, 1)).getUTCDay() + 6) % 7;
  const count = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const days = Array.from({ length: count }, (_, i) =>
    `${ym}-${String(i + 1).padStart(2, "0")}`);
  return { label: `${MONTHS[m - 1]} ${y}`, lead, days };
}

/**
 * The range after clicking `day` in the start or end calendar.
 *
 * A start after the current end clears the end rather than producing a range
 * that runs backwards; an end before the current start swaps the two, since
 * that is what someone picking "from the 30th back to the 2nd" means.
 */
export function pick(
  range: { from: string; to: string },
  which: "from" | "to",
  day: string,
): { from: string; to: string } {
  if (which === "from") {
    return { from: day, to: range.to && day > range.to ? "" : range.to };
  }
  if (range.from && day < range.from) return { from: day, to: range.from };
  return { from: range.from, to: day };
}
