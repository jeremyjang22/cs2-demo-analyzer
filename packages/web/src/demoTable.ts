/**
 * The demo tables: one person's games, with that person's numbers.
 *
 * Every table has a SUBJECT - the signed-in user on the home page, a pro on
 * their page, the featured pro on the signed-out sample. The subject decides
 * which player's line Result, K/D/A and ADR come from, and the page names the
 * subject once in its heading, so no cell has to say whose number it is.
 *
 * A subject is a list of steamids, not one: pros play on alternate accounts
 * (pros.json), and a game on any of them is theirs.
 */

import type { DemoSummary, PlayerLine } from "./payload";
import { PROS, type Pro } from "./pros";

export interface Subject {
  /** "You", or a pro's name. */
  name: string;
  ids: string[];
}

export function proSubject(pro: Pro): Subject {
  return { name: pro.name, ids: pro.steamids.map(String) };
}

/** The subject's line in a demo, or null if they did not play in it. */
export function lineOf(d: DemoSummary, subject: Subject): PlayerLine | null {
  for (const id of subject.ids) {
    const line = d.lines?.[id];
    if (line) return line;
  }
  return null;
}

export interface Result {
  /** From the subject's side. Empty when they were not in the game. */
  wl: "W" | "L" | "D" | "";
  /** The subject's team's rounds first; roster A's when there is no side. */
  us: number;
  them: number;
}

export function resultOf(d: DemoSummary, subject: Subject): Result | null {
  if (!d.score) return null;
  const [a, b] = d.score;
  const line = lineOf(d, subject);
  if (!line) return { wl: "", us: a, them: b };
  const [us, them] = line[3] === 0 ? [a, b] : [b, a];
  return { wl: us > them ? "W" : us < them ? "L" : "D", us, them };
}

// ---- which demos a page is about -------------------------------------------

/**
 * The signed-in user's games: every published demo they played in, plus any
 * they own. Owning without playing is a real case - a scrim someone recorded
 * - and lists with blank numbers rather than vanishing.
 */
export function yourDemos(index: DemoSummary[], steamid: string): DemoSummary[] {
  return index.filter((d) => d.lines?.[steamid] !== undefined || d.owner === steamid);
}

/** What a signed-out visitor is shown. */
export function sampleDemos(index: DemoSummary[]): DemoSummary[] {
  return index.filter((d) => d.sample === true);
}

/**
 * Whose numbers a sample shows: the first catalogued pro in it, since that is
 * who it was chosen for. A sample with no pro shows the score alone.
 */
export function sampleSubject(d: DemoSummary): Subject | null {
  for (const slug of d.pros ?? []) {
    const pro = PROS.find((p) => p.slug === slug);
    if (pro) return proSubject(pro);
  }
  return null;
}

// ---- filters ----------------------------------------------------------------

export interface Filters {
  map: string;
  kind: string;
  result: "" | "W" | "L";
  /** YYYY-MM-DD, inclusive; empty for open-ended. */
  from: string;
  to: string;
}

export const NO_FILTERS: Filters = { map: "", kind: "", result: "", from: "", to: "" };

export function activeFilters(f: Filters): number {
  return [f.map, f.kind, f.result, f.from, f.to].filter(Boolean).length;
}

/**
 * Demos matching every filter. A date range drops undated demos - "played in
 * September" cannot be true of a game whose date nobody declared.
 */
export function applyFilters(demos: DemoSummary[], f: Filters, subject: Subject | null): DemoSummary[] {
  return demos.filter((d) =>
    (!f.map || d.map === f.map) &&
    (!f.kind || (d.kind ?? "unknown") === f.kind) &&
    (!f.result || (subject !== null && resultOf(d, subject)?.wl === f.result)) &&
    (!f.from || (d.date !== undefined && d.date >= f.from)) &&
    (!f.to || (d.date !== undefined && d.date <= f.to)));
}

/** Choices the dropdowns offer: only what the demos on the page contain. */
export function filterChoices(demos: DemoSummary[]): {
  maps: string[];
  kinds: ("pug" | "match" | "unknown")[];
  dated: Set<string>;
} {
  return {
    maps: [...new Set(demos.map((d) => d.map))].sort(),
    kinds: (["pug", "match", "unknown"] as const).filter((k) => demos.some((d) => (d.kind ?? "unknown") === k)),
    dated: new Set(demos.flatMap((d) => (d.date ? [d.date] : []))),
  };
}

// ---- ordering ---------------------------------------------------------------

export type SortKey = "map" | "demo" | "date" | "result" | "kda" | "adr" | "length";
export interface Sort { key: SortKey; dir: 1 | -1 }

/** Numbers sort biggest first on the first click, words A-Z. */
export const FIRST_DIR: Record<SortKey, 1 | -1> = {
  map: 1, demo: 1, date: -1, result: -1, kda: -1, adr: -1, length: -1,
};

function sortValue(d: DemoSummary, key: SortKey, subject: Subject | null, label: (d: DemoSummary) => string): string | number {
  const line = subject ? lineOf(d, subject) : null;
  switch (key) {
    case "map": return d.map;
    case "demo": return label(d).toLowerCase();
    case "date": return d.date ?? "";
    case "result": {
      const r = subject ? resultOf(d, subject) : null;
      // Wins above losses, then by margin within each.
      return r ? (r.wl === "W" ? 1000 : r.wl === "D" ? 500 : 0) + r.us - r.them : -Infinity;
    }
    case "kda": return line ? line[0] - line[1] : -Infinity;
    case "adr": return line ? line[4] : -Infinity;
    case "length": return d.seconds;
  }
}

/**
 * The table's order. With no column chosen: newest first, undated after
 * every dated demo, then by name - so before anything has a date the list
 * still reads in a stable order.
 */
export function sortDemos(
  demos: DemoSummary[],
  sort: Sort | null,
  subject: Subject | null,
  label: (d: DemoSummary) => string,
): DemoSummary[] {
  const out = [...demos];
  if (!sort) {
    return out.sort((a, b) =>
      (b.date ?? "").localeCompare(a.date ?? "") || a.name.localeCompare(b.name));
  }
  return out.sort((a, b) => {
    const x = sortValue(a, sort.key, subject, label), y = sortValue(b, sort.key, subject, label);
    return (x < y ? -1 : x > y ? 1 : 0) * sort.dir || a.name.localeCompare(b.name);
  });
}

// ---- per-map summary (pro pages) ----------------------------------------------

export interface MapRecord {
  wins: number;
  losses: number;
  /** ADR across every round of these games, not an average of averages. */
  adr: number | null;
}

export function recordOf(demos: DemoSummary[], subject: Subject): MapRecord {
  let wins = 0, losses = 0, damage = 0, rounds = 0;
  for (const d of demos) {
    const r = resultOf(d, subject);
    if (r?.wl === "W") wins++;
    if (r?.wl === "L") losses++;
    const line = lineOf(d, subject);
    if (line) { damage += line[4] * d.rounds; rounds += d.rounds; }
  }
  return { wins, losses, adr: rounds ? damage / rounds : null };
}
