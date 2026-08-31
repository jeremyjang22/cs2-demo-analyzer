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

export type Route =
  | { view: "home" }
  | { view: "viewer"; demo: string; start: number };

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

  const raw = params.get("t");
  const start = raw === null ? NaN : Number(raw);
  return {
    view: "viewer",
    demo,
    // A malformed ?t= should open the round at the start, not refuse to open
    // it. Negative is clamped for the same reason posAt clamps its index.
    start: Number.isFinite(start) && start > 0 ? start : 0,
  };
}

/** The URL for a demo, optionally at a moment. Used for links and history. */
export function viewerHref(demo: string, start?: number): string {
  const params = new URLSearchParams({ demo });
  if (start && start > 0) params.set("t", String(Math.round(start)));
  return `?${params}`;
}

/** The URL of the home page, preserving nothing — it takes no parameters. */
export const HOME_HREF = "./";
