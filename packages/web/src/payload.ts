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
        // The Worker answers a private demo exactly like a missing one, so
        // this cannot say which - only that signing in might be the fix.
        if (r.status === 404) {
          throw new Error(`${demo} is not available: it is not published, or needs you signed in to an account that can see it`);
        }
        if (!r.ok) throw new Error(`movement.json for ${demo} could not be loaded (HTTP ${r.status})`);
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

/** [kills, deaths, assists, roster, adr] - see DemoSummary.lines. */
export type PlayerLine = [number, number, number, number, number];

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
  /**
   * Who may see it, declared at export (--visibility). The deployed Worker
   * filters the index by this, so a visitor is only ever sent entries they
   * can open; a non-public one here means the viewer is its owner. Local dev
   * serves the raw file and shows everything.
   */
  visibility?: "public" | "link" | "private";
  /** Steamid of the account it belongs to on the site. */
  owner?: string;
  /** Declared as what signed-out visitors are shown. Always public. */
  sample?: boolean;
  /** Final score, [roster A, roster B]. Absent on merged payloads. */
  score?: [number, number];
  /**
   * Every player's final line: steamid → [kills, deaths, assists, roster, adr].
   * Kills leave out suicides and teamkills; ADR is enemy damage actually
   * taken, per round. Absent on merged payloads and older indexes.
   */
  lines?: Record<string, PlayerLine>;
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

/** The list fetched afresh: one of your uploads has just been published. */
export function reloadIndex(): Promise<DemoSummary[]> {
  index = null;
  return loadIndex();
}
