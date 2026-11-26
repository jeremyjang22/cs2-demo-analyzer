/**
 * Write public/data/index.json: what demos exist, for the home page to list.
 *
 * The payloads themselves are the source of truth — this reads the map, round
 * count and roster straight out of each movement.json rather than keeping a
 * second list someone has to remember to update. Reading a megabyte of JSON to
 * pull four fields is wasteful in the abstract and irrelevant here: it runs
 * over a handful of files, at build time, on a developer's machine.
 *
 * Runs before `vite dev` and before an upload, so the index a browser fetches
 * always matches the payloads sitting next to it. The file goes to R2 with
 * everything else, and is served at the same /data/index.json URL in both
 * places.
 */
import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { infoProblems, playSummary } from "./demo-info.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const dataRoot = join(here, "..", "public", "data");

// The pro catalog, committed beside the app that shows it. Which pros a demo
// contains is worked out here from its roster, never tagged by hand: a pro
// match with ten catalogued players lands on all ten pages without anyone
// listing it ten times.
const catalog = JSON.parse(await readFile(join(here, "..", "src", "pros.json"), "utf8"));
const proOf = new Map();
for (const pro of catalog.pros ?? []) {
  for (const id of pro.steamids ?? []) proOf.set(String(id), pro.slug);
}

let entries;
try {
  entries = await readdir(dataRoot, { withFileTypes: true });
} catch {
  // No exports yet is a normal state for a fresh clone, not a failure. Write
  // an empty index so the home page renders its "nothing here" state rather
  // than a fetch error, which would look like something is broken.
  await writeEmpty();
  process.exit(0);
}

const demos = [];
const problems = [];
const undeclared = [];
const orphaned = [];
for (const entry of entries.filter((e) => e.isDirectory())) {
  const file = join(dataRoot, entry.name, "movement.json");
  const size = (await stat(file).catch(() => null))?.size;
  if (size === undefined) continue;

  const payload = JSON.parse(await readFile(file, "utf8"));
  // Declared by a person (export_movement.py, set-info), never measured.
  const info = JSON.parse(
    await readFile(join(dataRoot, entry.name, "info.json"), "utf8").catch(() => "{}"),
  );
  for (const p of infoProblems(info)) problems.push(`${entry.name}/info.json: ${p}`);

  // Who may see it - also declared, never inferred from the roster: being IN
  // a demo is not the same as having published it. The Worker enforces this
  // (worker/access.ts) and treats anything undeclared as private, so a demo
  // with no declaration is not leaked, only hidden; say so here, where the
  // person who can fix it is looking.
  const visibility = info.visibility ?? "private";
  const owner = info.owner === undefined ? undefined : String(info.owner);
  if (info.visibility === undefined) undeclared.push(entry.name);
  else if (visibility !== "public" && owner === undefined) orphaned.push(entry.name);

  const pros = [...new Set(
    (payload.players ?? []).map((p) => proOf.get(String(p.id))).filter(Boolean),
  )].sort();
  demos.push({
    name: entry.name,
    ...(info.title ? { title: info.title } : {}),
    kind: info.kind === "pug" || info.kind === "match" ? info.kind : "unknown",
    ...(info.event ? { event: info.event } : {}),
    ...(info.date ? { date: info.date } : {}),
    visibility,
    ...(owner ? { owner } : {}),
    // What a signed-out visitor is shown on the home page.
    ...(info.sample === true ? { sample: true } : {}),
    // More than one means a merged payload; the Pros pages leave those out,
    // since the games in them are listed on their own as well.
    games: (payload.demos ?? []).length || 1,
    pros,
    map: payload.map ?? "unknown",
    rounds: Object.keys(payload.rounds ?? {}).length,
    players: (payload.players ?? []).length,
    // Shown so someone can see what a demo costs before opening it on a
    // phone. Kilobytes, because megabytes would round most of them to "1".
    kb: Math.round(size / 1024),
    // Round durations are relative to each round's own start, so the last
    // round's end is the length of a game's live play. t0 restarts at zero for
    // every demo in a merged payload, so each game is measured on its own and
    // the games are added up - the same reading timeline.ts gives the viewer.
    seconds: Math.round(playSeconds(payload.rounds ?? {})),
    // Final score and every player's K/D/A and ADR. All ten, not just the
    // uploader's: which line a table shows depends on who is looking.
    ...playSummary(payload),
  });
}

// Refuse rather than warn. A bad visibility or a private sample would publish
// something wider, or narrower, than whoever wrote info.json meant.
if (problems.length) {
  console.error(["demo index NOT written:", ...problems].join("\n  "));
  process.exit(1);
}

demos.sort((a, b) => a.name.localeCompare(b.name));
await writeFile(join(dataRoot, "index.json"), JSON.stringify({ demos }, null, 2) + "\n");
console.log(`demo index: ${demos.length} demo(s) -> public/data/index.json`);
if (undeclared.length) {
  console.warn(`  no visibility declared, so private to nobody once published: ${undeclared.join(", ")}`);
  console.warn("  declare it: npm run set-info -- <demo> --visibility ... [--owner <steamid>]");
}
if (orphaned.length) {
  console.warn(`  not public but no owner, so nobody can open them: ${orphaned.join(", ")}`);
}

async function writeEmpty() {
  const { mkdir } = await import("node:fs/promises");
  await mkdir(dataRoot, { recursive: true });
  await writeFile(join(dataRoot, "index.json"), JSON.stringify({ demos: [] }, null, 2) + "\n");
  console.log("demo index: no exports found, wrote an empty index");
}

/** Live play across every game in a payload, merged or not. */
function playSeconds(rounds) {
  const perGame = new Map();
  for (const r of Object.values(rounds)) {
    const dm = r.dm ?? 0;
    perGame.set(dm, Math.max(perGame.get(dm) ?? 0, (r.t0 ?? 0) + (r.dur ?? 0)));
  }
  let total = 0;
  for (const s of perGame.values()) total += s;
  return total;
}
