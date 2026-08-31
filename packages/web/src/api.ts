/**
 * The backend, as this app sees it.
 *
 * Every path is relative to /api on this same origin, which is not an accident
 * of convenience. In production a Cloudflare Worker serves the app and proxies
 * /api/* to the Go service, so the session cookie is first-party; the API sets
 * it SameSite=Lax and Steam login finishes with a cross-site redirect, which a
 * browser would not carry to a different origin. In development vite proxies
 * the same prefix to a local server. Same URLs either way, and nothing here
 * needs to know which it is talking to.
 */

const API = "/api";

/** Who is signed in. Mirrors the /me handler's JSON. */
export interface Me {
  /** SteamID as a string — it exceeds Number.MAX_SAFE_INTEGER. */
  steamid: string;
  name: string;
  avatar: string | null;
}

/**
 * The signed-in user, or null.
 *
 * A 401 is the ordinary answer for a signed-out visitor, so it resolves null
 * rather than throwing — being logged out is not an error and the home page
 * should not render an error state for it. A network failure or a 500 does
 * throw, because those mean the page cannot say either way.
 */
export async function fetchMe(signal?: AbortSignal): Promise<Me | null> {
  const res = await fetch(`${API}/me`, {
    // The session is a cookie. Same-origin already sends it, but being
    // explicit means this keeps working if the API ever moves origins.
    credentials: "include",
    signal,
  });
  if (res.status === 401) return null;
  if (!res.ok) throw new Error(`could not reach the API (HTTP ${res.status})`);
  return (await res.json()) as Me;
}

/**
 * Where to send the browser to start Steam login.
 *
 * A full navigation, never fetch(): Steam answers with a 302 to its own login
 * page and the user has to actually go there. The API bounces them back to
 * this origin when it is done.
 */
export const LOGIN_HREF = `${API}/auth/steam/login`;

export async function logout(): Promise<void> {
  await fetch(`${API}/auth/logout`, { method: "POST", credentials: "include" });
}

/**
 * A Steam profile URL for a 64-bit id, so a signed-in user can check the app
 * matched the right account.
 */
export function steamProfileHref(steamid: string): string {
  return `https://steamcommunity.com/profiles/${steamid}`;
}
