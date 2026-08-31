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

const here = dirname(fileURLToPath(import.meta.url));
const dataRoot = join(here, "..", "public", "data");

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
for (const entry of entries.filter((e) => e.isDirectory())) {
  const file = join(dataRoot, entry.name, "movement.json");
  const size = (await stat(file).catch(() => null))?.size;
  if (size === undefined) continue;

  const payload = JSON.parse(await readFile(file, "utf8"));
  demos.push({
    name: entry.name,
    map: payload.map ?? "unknown",
    rounds: Object.keys(payload.rounds ?? {}).length,
    players: (payload.players ?? []).length,
    // Shown so someone can see what a demo costs before opening it on a
    // phone. Kilobytes, because megabytes would round most of them to "1".
    kb: Math.round(size / 1024),
    // Round durations are relative to each round's own start, so the last
    // round's end is the length of the match's live play.
    seconds: Math.round(
      Math.max(0, ...Object.values(payload.rounds ?? {}).map((r) => (r.t0 ?? 0) + (r.dur ?? 0))),
    ),
  });
}

demos.sort((a, b) => a.name.localeCompare(b.name));
await writeFile(join(dataRoot, "index.json"), JSON.stringify({ demos }, null, 2) + "\n");
console.log(`demo index: ${demos.length} demo(s) -> public/data/index.json`);

async function writeEmpty() {
  const { mkdir } = await import("node:fs/promises");
  await mkdir(dataRoot, { recursive: true });
  await writeFile(join(dataRoot, "index.json"), JSON.stringify({ demos: [] }, null, 2) + "\n");
  console.log("demo index: no exports found, wrote an empty index");
}
