/**
 * The pro catalog, and what the published demos hold for each pro.
 *
 * pros.json is committed and edited by hand: a pro is a name and every Steam
 * account they play on, because pros pug on alternate accounts and a single
 * steamid would split their games. Which demos a pro appears in is NOT in the
 * catalog - index-demos.mjs works it out from each roster - so adding a demo
 * never means editing this file, and adding a pro here picks up every demo
 * they were already in.
 */

import catalog from "./pros.json";
import type { DemoSummary } from "./payload";

export interface Pro {
  slug: string;
  name: string;
  team?: string;
  steamids: string[];
}

export const PROS: Pro[] = catalog.pros;

export function proBySlug(slug: string | undefined): Pro | undefined {
  return PROS.find((p) => p.slug === slug);
}

export type KindFilter = "all" | "pug" | "match";

export const KIND_LABEL: Record<"pug" | "match" | "unknown", string> = {
  pug: "Pug",
  match: "Pro match",
  unknown: "Unlabelled",
};

/**
 * One pro's demos, optionally narrowed to a kind and a map.
 *
 * Merged payloads are left out: their games are published on their own too,
 * and counting both would double every round.
 */
export function demosFor(
  demos: DemoSummary[],
  slug: string,
  filter: { kind?: KindFilter; map?: string } = {},
): DemoSummary[] {
  return demos.filter((d) =>
    (d.games ?? 1) === 1 &&
    (d.pros ?? []).includes(slug) &&
    (!filter.kind || filter.kind === "all" || d.kind === filter.kind) &&
    (!filter.map || d.map === filter.map),
  );
}

/** What a pro's demos cover, for the filter chips and the pro list. */
export interface Facets {
  total: number;
  rounds: number;
  kinds: Record<"pug" | "match" | "unknown", number>;
  /** Map name to demo count, most played first. */
  maps: [string, number][];
}

export function facets(demos: DemoSummary[]): Facets {
  const kinds = { pug: 0, match: 0, unknown: 0 };
  const maps = new Map<string, number>();
  let rounds = 0;
  for (const d of demos) {
    kinds[d.kind ?? "unknown"]++;
    maps.set(d.map, (maps.get(d.map) ?? 0) + 1);
    rounds += d.rounds;
  }
  return {
    total: demos.length,
    rounds,
    kinds,
    maps: [...maps.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
  };
}

/** de_mirage → Mirage. */
export function mapLabel(map: string): string {
  const bare = map.replace(/^(de|cs)_/, "");
  return bare.charAt(0).toUpperCase() + bare.slice(1);
}

/**
 * What to call a demo in a list: its declared title, else the event for a pro
 * match, else a short form of the file name - FACEIT names are a UUID, and
 * the first block is enough to tell them apart.
 */
export function demoLabel(d: DemoSummary): string {
  if (d.title) return d.title;
  if (d.kind === "match" && d.event) return d.event;
  const faceit = /^\d-([0-9a-f]{8})-[0-9a-f-]{27}-\d-\d$/.exec(d.name);
  return faceit ? `FACEIT ${faceit[1]}` : d.name;
}
