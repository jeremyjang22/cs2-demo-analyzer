/**
 * Fetching demo payloads, once each.
 *
 * Every page that shows a demo needs its movement.json, and the compare view
 * can show the same one in both panes - or switch a pane away and back. A
 * payload is megabytes of JSON, so the promise is kept per name and every
 * caller shares it. A failed fetch is dropped from the cache so a retry
 * (a reload, or picking the demo again) actually retries.
 */

import { assertPayload, type Payload } from "./types";

const cache = new Map<string, Promise<Payload>>();

export function loadPayload(demo: string): Promise<Payload> {
  let p = cache.get(demo);
  if (!p) {
    p = fetch(`${import.meta.env.BASE_URL}data/${demo}/movement.json`)
      .then((r) => {
        if (!r.ok) throw new Error(`movement.json for ${demo} not found (HTTP ${r.status})`);
        return r.json();
      })
      .then((data: unknown) => {
        assertPayload(data);
        return data;
      });
    p.catch(() => cache.delete(demo));
    cache.set(demo, p);
  }
  return p;
}

/** One entry from /data/index.json, written by scripts/index-demos.mjs. */
export interface DemoSummary {
  name: string;
  /** A readable name, when whoever exported it gave one (--title). */
  title?: string;
  /**
   * Pug or pro match, DECLARED at export (--kind), never inferred. "unknown"
   * for anything exported without it. Optional so an index written before the
   * field existed still reads.
   */
  kind?: "pug" | "match" | "unknown";
  event?: string;
  /** YYYY-MM-DD, as declared. */
  date?: string;
  /** Games in the payload: more than one is a merged export. */
  games?: number;
  /** Slugs of catalogued pros on the roster, matched by steamid at index time. */
  pros?: string[];
  map: string;
  rounds: number;
  players: number;
  kb: number;
  seconds: number;
}

let index: Promise<DemoSummary[]> | null = null;

/**
 * The published demo list. A missing index is the normal state before anything
 * is published, so it reads as an empty list rather than an error.
 */
export function loadIndex(): Promise<DemoSummary[]> {
  if (!index) {
    index = fetch(`${import.meta.env.BASE_URL}data/index.json`)
      .then((r) => {
        if (r.status === 404) return { demos: [] };
        if (!r.ok) throw new Error(`could not load the demo list (HTTP ${r.status})`);
        return r.json();
      })
      .then((data: { demos?: DemoSummary[] }) => data.demos ?? []);
    index.catch(() => { index = null; });
  }
  return index;
}
