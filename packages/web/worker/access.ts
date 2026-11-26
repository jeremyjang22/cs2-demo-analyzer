/**
 * Who may see which demo.
 *
 * A demo holds ten people's movement for a whole match, so access is declared
 * per demo rather than assumed. The declaration is two fields in the demo's
 * info.json - `owner` (a steamid) and `visibility` - which index-demos copies
 * into index.json, which is the one file the Worker reads to decide. The words
 * are the `demo_visibility` enum in packages/api/schema/schema.sql, so a demo
 * means the same thing whether it was published by hand or, later, uploaded:
 *
 *   public   listed for, and opens for, every signed-in viewer
 *   link     opens for any signed-in viewer holding the name, listed only
 *            for its owner
 *   private  owner only, listed and opened
 *
 * A visitor who is not signed in sees the samples and nothing else: the
 * entries index-demos marks `"sample": true`, which it refuses to write
 * unless they are also public. That is a site policy rather than something a
 * demo declares - the pro demos are public to accounts, and the samples are
 * the shop window for everyone else - so it lives in one function,
 * `signedOutMay`, and loosening it later is a change there alone.
 *
 * Everything here fails closed. A demo missing from the index is not served,
 * and an entry that declares no visibility is private - a demo published
 * before this existed, or by someone who forgot, stays hidden rather than
 * leaking, and index-demos says so loudly when it writes one.
 */

export type Visibility = "public" | "link" | "private";

/** The fields of an index entry this module reads. */
export interface Access {
  name: string;
  visibility?: string;
  owner?: string;
  sample?: boolean;
}

export function visibilityOf(entry: Access): Visibility {
  const v = entry.visibility;
  return v === "public" || v === "link" ? v : "private";
}

function isOwner(entry: Access, viewer: string | null): boolean {
  return viewer !== null && entry.owner !== undefined && String(entry.owner) === viewer;
}

/**
 * The only thing a visitor who is not signed in may see. `=== true` rather
 * than truthiness, and public as well, so a malformed index fails closed.
 */
export function signedOutMay(entry: Access): boolean {
  return entry.sample === true && visibilityOf(entry) === "public";
}

/** May this viewer fetch the demo's files? `entry` undefined: not published. */
export function canOpen(entry: Access | undefined, viewer: string | null): boolean {
  if (!entry) return false;
  if (viewer === null) return signedOutMay(entry);
  return visibilityOf(entry) !== "private" || isOwner(entry, viewer);
}

/** Should the demo appear in this viewer's copy of the index? */
export function isListed(entry: Access, viewer: string | null): boolean {
  if (viewer === null) return signedOutMay(entry);
  return visibilityOf(entry) === "public" || isOwner(entry, viewer);
}

/**
 * The demo an R2 key belongs to: "data/nukepug/movement.json" → "nukepug".
 * Null for anything that is not a file inside a demo's folder - the index
 * itself, or a path with no folder - which the caller must treat as its own
 * case rather than as some demo's.
 */
export function demoOfKey(key: string): string | null {
  const m = /^data\/([^/]+)\/[^/]+$/.exec(key);
  if (!m) return null;
  const name = m[1];
  // "." and ".." would name a directory, not a demo; R2 keys are flat strings
  // so they could not escape anything, but they can never match an entry.
  return name === "." || name === ".." ? null : name;
}
