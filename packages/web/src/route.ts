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
}

export type Route =
  | { view: "home" }
  | { view: "viewer"; demo: string; start: number; at?: RoundMoment }
  | { view: "analytics"; demo: string };

/**
 * Parse a location search string into a route.
 *
 * Takes the string rather than reading `location` so it is testable and so a
 * caller can route from a popstate event's URL.
 */
export function routeFrom(search: string): Route {
  const params = new URLSearchParams(search);
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
  }
  return `?${params}`;
}

/** The URL of a demo's analytics page. */
export function analyticsHref(demo: string): string {
  return `?${new URLSearchParams({ demo, view: "analytics" })}`;
}

/** The URL of the home page, preserving nothing — it takes no parameters. */
export const HOME_HREF = "./";
