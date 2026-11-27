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

/** One of your uploads. Mirrors uploads.Upload in the API. */
export interface Upload {
  id: string;
  /** What it is published as: ?demo=<name> once ready. */
  name: string;
  status: "uploading" | "queued" | "parsing" | "ready" | "failed";
  filename: string;
  size: number;
  /** Why processing failed, when it did. */
  error?: string;
  createdAt: string;
}

export interface UploadsState {
  /** On the uploader allowlist, and the server has somewhere to put files. */
  canUpload: boolean;
  maxBytes: number;
  uploads: Upload[];
  /**
   * Whether a demo-worker has reported in within the last two minutes. Absent
   * when the server could not tell, which reads as online: say nothing
   * rather than something wrong.
   */
  processing?: { online: boolean };
}

/** Where to PUT the file, or - for a file already known by its hash - the demo it already is. */
export interface Ticket {
  upload: Upload;
  url?: string;
  expires?: string;
  duplicate: boolean;
}

/** Your uploads and whether you may upload; null when signed out. */
export async function fetchUploads(signal?: AbortSignal): Promise<UploadsState | null> {
  const res = await fetch(`${API}/uploads`, { credentials: "include", signal });
  if (res.status === 401) return null;
  return ok<UploadsState>(res);
}

/** Declare a file before sending it: its name, size and SHA-256. */
export async function startUpload(req: { filename: string; size: number; sha256: string }): Promise<Ticket> {
  return ok<Ticket>(await fetch(`${API}/uploads`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  }));
}

/** Say the PUT finished; the server checks the file arrived and queues it. */
export async function completeUpload(id: string): Promise<Upload> {
  return ok<Upload>(await fetch(`${API}/uploads/${encodeURIComponent(id)}/complete`, {
    method: "POST",
    credentials: "include",
  }));
}

/** The body of a 2xx, or an Error carrying the API's own message. */
async function ok<T>(res: Response): Promise<T> {
  if (res.ok) return (await res.json()) as T;
  let message = `HTTP ${res.status}`;
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) message = body.error;
  } catch {
    // not JSON: a proxy error page; the status says enough
  }
  throw new Error(message);
}
