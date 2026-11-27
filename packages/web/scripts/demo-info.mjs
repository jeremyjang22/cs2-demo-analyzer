/**
 * What goes into a demo's index entry, shared by index-demos and set-info.
 *
 * Two kinds of fact, kept apart on purpose:
 *
 *   DECLARED - info.json, written by a person (export_movement.py or
 *   set-info): kind, event, date, title, visibility, owner, sample. Nothing
 *   here is guessed from a file name or a timestamp; a date taken from a
 *   file's modified time is still declared, by whoever chose to run
 *   `set-info --dates-from` over files they know they recorded themselves.
 *
 *   MEASURED - movement.json: the score, and each player's K/D/A and ADR.
 *
 * Pure functions over parsed JSON, so they are tested without touching disk.
 */

export const VISIBILITIES = ["public", "link", "private"];
export const KINDS = ["pug", "match"];

const STEAMID = /^\d{17}$/;

export function validDate(s) {
  if (typeof s !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1, d));
  return t.getUTCFullYear() === y && t.getUTCMonth() === m - 1 && t.getUTCDate() === d;
}

/**
 * Problems with a declared info.json, as messages. Empty when it is usable.
 * index-demos refuses to write an index while any demo has one, so a typo in
 * a visibility can never publish something more widely than intended.
 */
export function infoProblems(info) {
  const out = [];
  if (info.kind !== undefined && !KINDS.includes(info.kind)) {
    out.push(`kind must be ${KINDS.join(" or ")}, not ${JSON.stringify(info.kind)}`);
  }
  if (info.visibility !== undefined && !VISIBILITIES.includes(info.visibility)) {
    out.push(`visibility must be ${VISIBILITIES.join(", ")}, not ${JSON.stringify(info.visibility)}`);
  }
  if (info.owner !== undefined && !STEAMID.test(String(info.owner))) {
    out.push(`owner must be a 64-bit steamid (17 digits), not ${info.owner}`);
  }
  if (info.date !== undefined && !validDate(info.date)) {
    out.push(`date must be a real YYYY-MM-DD, not ${JSON.stringify(info.date)}`);
  }
  if (info.sample !== undefined && typeof info.sample !== "boolean") {
    out.push(`sample must be true or false, not ${JSON.stringify(info.sample)}`);
  }
  // A sample is what a signed-out visitor is shown. The Worker would refuse a
  // private one to exactly the people it is for, so catch it here instead.
  if (info.sample === true && info.visibility !== "public") {
    out.push(`a sample must be public (it is shown to signed-out visitors); visibility is ${info.visibility ?? "undeclared"}`);
  }
  return out;
}

/**
 * The final score and every player's line, for the demo tables.
 *
 * `score` is [roster A, roster B] after the last round; `lines` maps a
 * steamid to [kills, deaths, assists, roster, adr]. Arrays rather than
 * objects because the index carries ten of them per demo.
 *
 * K/D/A is the payload's own final tally, which leaves suicides and teamkills
 * out of kills (movement_payload.player_stats). ADR is health actually taken
 * off ENEMIES per round - `hpt`, not raw damage, which runs about a third
 * high - the same rule as the viewer's live scoreboard (stats.ts).
 *
 * Merged payloads hold several games, where one score means nothing; they get
 * neither field.
 */
export function playSummary(payload) {
  if ((payload.demos ?? []).length > 1) return {};
  const rounds = Object.values(payload.rounds ?? {}).sort((a, b) => a.n - b.n);
  if (!rounds.length) return {};
  const last = rounds[rounds.length - 1];

  const roster = new Map((payload.players ?? []).map((p) => [String(p.id), p.team]));
  const damage = new Map();
  for (const hits of Object.values(payload.damage ?? {})) {
    for (const h of hits) {
      if (!h.by || h.by === h.v) continue;
      const a = roster.get(h.by), b = roster.get(h.v);
      if (a !== undefined && a === b) continue;
      damage.set(h.by, (damage.get(h.by) ?? 0) + (h.hpt ?? 0));
    }
  }

  const lines = {};
  for (const p of payload.players ?? []) {
    const id = String(p.id);
    const s = payload.stats?.[id] ?? { k: 0, d: 0, a: 0 };
    const adr = Math.round(((damage.get(id) ?? 0) / rounds.length) * 10) / 10;
    lines[id] = [s.k, s.d, s.a, p.team, adr];
  }
  return { score: [last.sa, last.sb], lines };
}

/**
 * Everything an index entry MEASURES from a payload, given its size in bytes.
 *
 * packages/catalog (Go) generates the published index from the database and
 * ports this function as catalog.Measure. Both are checked against the same
 * fixture, packages/catalog/testdata: change one and the other's test fails.
 */
export function measure(payload, bytes) {
  return {
    // More than one means a merged payload; the Pros pages leave those out,
    // since the games in them are listed on their own as well.
    games: (payload.demos ?? []).length || 1,
    map: payload.map ?? "unknown",
    rounds: Object.keys(payload.rounds ?? {}).length,
    players: (payload.players ?? []).length,
    // Shown so someone can see what a demo costs before opening it on a
    // phone. Kilobytes, because megabytes would round most of them to "1".
    kb: Math.round(bytes / 1024),
    // Round durations are relative to each round's own start, so the last
    // round's end is the length of a game's live play. t0 restarts at zero for
    // every demo in a merged payload, so each game is measured on its own and
    // the games are added up - the same reading timeline.ts gives the viewer.
    seconds: Math.round(playSeconds(payload.rounds ?? {})),
    // Final score and every player's K/D/A and ADR. All ten, not just the
    // uploader's: which line a table shows depends on who is looking.
    ...playSummary(payload),
  };
}

/** Live play across every game in a payload, merged or not. */
export function playSeconds(rounds) {
  const perGame = new Map();
  for (const r of Object.values(rounds)) {
    const dm = r.dm ?? 0;
    perGame.set(dm, Math.max(perGame.get(dm) ?? 0, (r.t0 ?? 0) + (r.dur ?? 0)));
  }
  let total = 0;
  for (const s of perGame.values()) total += s;
  return total;
}

/** A local calendar date for a file's modified time: the day it was recorded where you are. */
export function localDate(mtime) {
  const d = new Date(mtime);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** "nukepug.dem" or "x.dem.zst" → "nukepug" / "x"; null for anything else. */
export function demoNameOf(file) {
  const m = /^(.*)\.dem(\.zst)?$/i.exec(file);
  return m ? m[1] : null;
}
