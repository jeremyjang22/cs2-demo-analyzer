/**
 * Which view the URL is asking for.
 *
 * Routing is by query parameter, not path, and that is a constraint rather
 * than a preference: `base: "./"` in vite.config.ts makes every asset URL
 * relative, so a page served at /demo/anubispug would ask for
 * /demo/assets/index.js and get nothing. Query params leave the path at the
 * root where the relative base works.
 *
 * It also costs nothing in compatibility — ?demo= and ?t= were already the
 * viewer's contract, so every link anyone has already shared still opens
 * exactly what it opened before. The home page is simply what the app shows
 * when no demo is named.
 */

/**
 * A moment named the way the analytics name it: a round, and seconds into its
 * live play. Separate from `start` because the analytics page links into the
 * replay without having the replay's payload, and only that payload knows
 * where a round sits on the match clock.
 */
export interface RoundMoment {
  round: number;
  sec: number;
  /**
   * Which game of a merged payload, by demo index. Every game has a round 5,
   * so without this a link into a merged payload always opened game one's.
   * Absent means the first match, which is right for a single-demo payload
   * and keeps every link written before this field working.
   */
  game?: number;
}

import { formatRoundRef, parseRoundRef, type Anchor, type RoundRef } from "./compare";
import type { KindFilter } from "./pros";

export type Route =
  | { view: "home" }
  | { view: "viewer"; demo: string; start: number; at?: RoundMoment }
  | { view: "analytics"; demo: string }
  | {
      view: "starters";
      /** The set studied: one demo, or several (a pro's Mirage pugs). */
      demos: string[];
      player?: string;
      /** A catalogued pro, followed on whichever of their accounts is present. */
      pro?: string;
      side?: "T" | "CT";
      t?: number;
    }
  | { view: "pros" }
  | { view: "pro"; pro: string; map?: string; kind?: KindFilter }
  | { view: "compare"; a?: RoundRef; b?: RoundRef; align?: Anchor; t?: number };

const ANCHORS: readonly Anchor[] = ["start", "plant", "first"];
const KINDS: readonly KindFilter[] = ["all", "pug", "match"];

/**
 * Parse a location search string into a route.
 *
 * Takes the string rather than reading `location` so it is testable and so a
 * caller can route from a popstate event's URL.
 */
export function routeFrom(search: string): Route {
  const params = new URLSearchParams(search);

  // Checked before ?demo=: each pane names its own round, possibly from
  // different payloads, so there is no single demo for the page.
  if (params.get("view") === "compare") {
    const route: Route = { view: "compare" };
    const a = parseRoundRef(params.get("a"));
    const b = parseRoundRef(params.get("b"));
    if (a) route.a = a;
    if (b) route.b = b;
    const align = params.get("align") as Anchor | null;
    if (align && ANCHORS.includes(align)) route.align = align;
    // Relative to the anchor, so it may be negative (before the plant).
    const t = Number(params.get("t") ?? NaN);
    if (params.has("t") && Number.isFinite(t)) route.t = t;
    return route;
  }

  if (params.get("view") === "pros") return { view: "pros" };
  if (params.get("view") === "pro") {
    const pro = params.get("pro");
    if (!pro) return { view: "pros" };
    const route: Route = { view: "pro", pro };
    const map = params.get("map");
    if (map) route.map = map;
    const kind = params.get("kind") as KindFilter | null;
    if (kind && KINDS.includes(kind)) route.kind = kind;
    return route;
  }

  // Starters takes a set of demos, so it is read before the single ?demo=.
  if (params.get("view") === "starters") {
    const demos = (params.get("demos") ?? params.get("demo") ?? "")
      .split(",").map((d) => d.trim()).filter(Boolean);
    if (demos.length === 0) return { view: "home" };
    // All optional: the page picks a default player and side, so a bare link
    // still opens something worth looking at.
    const route: Route = { view: "starters", demos };
    const player = params.get("player");
    if (player) route.player = player;
    const pro = params.get("pro");
    if (pro) route.pro = pro;
    const side = params.get("side")?.toUpperCase();
    if (side === "T" || side === "CT") route.side = side;
    // Seconds into the round, the same for every stacked round.
    const t = Number(params.get("t") ?? NaN);
    if (Number.isFinite(t) && t > 0) route.t = t;
    return route;
  }

  const demo = params.get("demo");
  if (!demo) return { view: "home" };

  // An unknown ?view= opens the replay, the same way a malformed ?t= does:
  // a link that names a real demo should always open that demo.
  if (params.get("view") === "analytics") return { view: "analytics", demo };
  const raw = params.get("t");
  const start = raw === null ? NaN : Number(raw);
  const route: Route = {
    view: "viewer",
    demo,
    // A malformed ?t= should open the round at the start, not refuse to open
    // it. Negative is clamped for the same reason posAt clamps its index.
    start: Number.isFinite(start) && start > 0 ? start : 0,
  };

  const round = Number(params.get("round") ?? NaN);
  if (Number.isInteger(round) && round > 0) {
    const sec = Number(params.get("rt") ?? 0);
    route.at = { round, sec: Number.isFinite(sec) && sec > 0 ? sec : 0 };
    const game = Number(params.get("game") ?? NaN);
    if (Number.isInteger(game) && game >= 0) route.at.game = game;
  }
  return route;
}

/** The URL for a demo, optionally at a moment. Used for links and history. */
export function viewerHref(demo: string, start?: number, at?: RoundMoment): string {
  const params = new URLSearchParams({ demo });
  if (start && start > 0) params.set("t", String(Math.round(start)));
  if (at) {
    params.set("round", String(at.round));
    if (at.sec > 0) params.set("rt", at.sec.toFixed(1));
    if (at.game !== undefined && at.game > 0) params.set("game", String(at.game));
  }
  return `?${params}`;
}

/** The URL of a demo's analytics page. */
export function analyticsHref(demo: string): string {
  return `?${new URLSearchParams({ demo, view: "analytics" })}`;
}

export interface StartersOptions {
  player?: string;
  pro?: string;
  side?: "T" | "CT";
  t?: number;
}

/**
 * The URL of the starters page over one demo or a set. A single demo keeps
 * the ?demo= form every earlier link used.
 */
export function startersHref(demos: string | string[], opt: StartersOptions = {}): string {
  const list = typeof demos === "string" ? [demos] : demos;
  const params = new URLSearchParams({ view: "starters" });
  if (list.length === 1) params.set("demo", list[0]);
  else params.set("demos", list.join(","));
  if (opt.pro) params.set("pro", opt.pro);
  else if (opt.player) params.set("player", opt.player);
  if (opt.side) params.set("side", opt.side);
  if (opt.t && opt.t > 0) params.set("t", opt.t.toFixed(1));
  return `?${params}`;
}

/** The Pros section: every catalogued pro. */
export const PROS_HREF = "?view=pros";

/** One pro's page, optionally filtered. */
export function proHref(pro: string, map?: string, kind?: KindFilter): string {
  const params = new URLSearchParams({ view: "pro", pro });
  if (map) params.set("map", map);
  if (kind && kind !== "all") params.set("kind", kind);
  return `?${params}`;
}

/** The URL of the compare view. Either round may be left for the page to pick. */
export function compareHref(a?: RoundRef, b?: RoundRef, align?: Anchor, t?: number): string {
  const params = new URLSearchParams({ view: "compare" });
  if (a) params.set("a", formatRoundRef(a));
  if (b) params.set("b", formatRoundRef(b));
  if (align && align !== "start") params.set("align", align);
  if (t !== undefined && Math.abs(t) >= 0.05) params.set("t", t.toFixed(1));
  return `?${params}`;
}

/** The URL of the home page, preserving nothing — it takes no parameters. */
export const HOME_HREF = "./";
