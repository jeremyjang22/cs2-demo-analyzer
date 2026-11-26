/**
 * Who is asking, read from the API's own session cookie.
 *
 * The Go API signs `sid` as "steamid.expiry.signature" (HMAC-SHA256, base64url
 * without padding; packages/api/internal/auth/session.go). The Worker checks
 * that signature itself, with the same SESSION_KEY, rather than asking the API
 * via /me: the API scales to zero on Northflank, and a private demo should not
 * wait on a cold start, or fail outright when the API is down, just to learn a
 * steamid the cookie already carries.
 *
 * The cost is two implementations of one format. They are pinned to a shared
 * test vector, asserted in both session.test.ts and the Go session_test.go.
 */

export const COOKIE_NAME = "sid";

/**
 * The signed-in steamid, or null for anyone not provably signed in: no cookie,
 * a malformed or forged one, an expired one, or no key to check it with.
 * Every failure is the same answer because every failure means the same thing
 * to the caller - treat them as a stranger.
 */
export async function viewerOf(
  cookieHeader: string | null,
  key: string | undefined,
  now: Date = new Date(),
): Promise<string | null> {
  if (!key) return null;
  const raw = readCookie(cookieHeader, COOKIE_NAME);
  if (!raw) return null;

  const parts = raw.split(".");
  if (parts.length !== 3) return null;
  const [id, exp, sig] = parts;
  // Digits only: the steamid is compared as a string against owners in the
  // index, so "076…" or "7.6e16" must never parse as someone else's id.
  if (!/^\d{1,20}$/.test(id) || !/^-?\d{1,20}$/.test(exp)) return null;

  const cryptoKey = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(key),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["verify"],
  );
  const signature = fromBase64Url(sig);
  if (!signature) return null;
  // subtle.verify compares in constant time, which is the property the Go
  // side gets from subtle.ConstantTimeCompare.
  const ok = await crypto.subtle.verify(
    "HMAC",
    cryptoKey,
    signature,
    new TextEncoder().encode(`${id}.${exp}`),
  );
  if (!ok) return null;

  // Go: time.Now().After(expiry) rejects. Seconds, like the cookie.
  if (now.getTime() / 1000 > Number(exp)) return null;
  return id;
}

function readCookie(header: string | null, name: string): string | null {
  if (!header) return null;
  for (const part of header.split(";")) {
    const eq = part.indexOf("=");
    if (eq < 0) continue;
    if (part.slice(0, eq).trim() === name) return part.slice(eq + 1).trim();
  }
  return null;
}

function fromBase64Url(s: string): Uint8Array | null {
  if (!/^[A-Za-z0-9_-]*$/.test(s)) return null;
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
  const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
  try {
    return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
  } catch {
    return null;
  }
}
