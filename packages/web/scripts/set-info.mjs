/**
 * Declare facts about published demos without re-exporting them.
 *
 *   npm run set-info -- nukepug --date 2026-08-14
 *   npm run set-info -- nukepug anubispug --visibility private --owner 76561198230376396
 *   npm run set-info -- 1-604132bd-... --kind pug --visibility public --sample
 *   npm run set-info -- --dates-from ../../data            # backfill dates
 *   npm run set-info -- --dates-from ../../data --dry-run  # see what it would do
 *
 * Writes public/data/<demo>/info.json - the same file export_movement.py
 * writes, so either can set a field and neither loses the other's. Fields not
 * named on the command line are left alone. Run `npm run index-demos` (or
 * just `npm run dev`) afterwards to see the change; upload-data rebuilds the
 * index itself.
 *
 * --dates-from backfills a date from each .dem's modified time, for demos
 * that have none yet (--force overwrites). That timestamp is when the FILE was
 * written: right for a game your own client recorded, wrong for anything you
 * downloaded - every FACEIT demo carries its download day. So it is a thing you
 * run over files you know are your own recordings, and what it prints is what
 * it declared. A demo the folder does not hold is never touched.
 */
import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { demoNameOf, infoProblems, KINDS, localDate, validDate, VISIBILITIES } from "./demo-info.mjs";

const HELP = `set-info: declare facts about published demos (writes public/data/<demo>/info.json)

  npm run set-info -- <demo...> [--date YYYY-MM-DD] [--kind pug|match]
                               [--visibility public|link|private] [--owner STEAMID]
                               [--sample | --no-sample] [--title T] [--event E] [--dry-run]
  npm run set-info -- --dates-from <folder|file.dem> [--force] [--dry-run]

--dates-from uses each file's modified time: right for games your own client
recorded, wrong for downloads (a FACEIT demo carries its download day).`;

const here = dirname(fileURLToPath(import.meta.url));
const dataRoot = join(here, "..", "public", "data");

const args = process.argv.slice(2);
const flags = new Map();
const names = [];
const VALUED = new Set(["--date", "--kind", "--visibility", "--owner", "--title", "--event", "--dates-from"]);
for (let i = 0; i < args.length; i++) {
  const a = args[i];
  if (VALUED.has(a)) {
    if (args[i + 1] === undefined) fail(`${a} needs a value`);
    flags.set(a, args[++i]);
  } else if (a.startsWith("--")) {
    flags.set(a, true);
  } else {
    names.push(a);
  }
}
const known = new Set([...VALUED, "--sample", "--no-sample", "--dry-run", "--force", "--help"]);
for (const f of flags.keys()) if (!known.has(f)) fail(`unknown option ${f}`);
if (flags.has("--help") || args.length === 0) usage();

const dryRun = flags.has("--dry-run");
const published = new Set(
  (await readdir(dataRoot, { withFileTypes: true }).catch(() => []))
    .filter((e) => e.isDirectory()).map((e) => e.name),
);

if (flags.has("--dates-from")) {
  if (names.length) fail("--dates-from takes a folder or .dem files, not demo names");
  await backfillDates(flags.get("--dates-from"));
} else {
  if (!names.length) fail("name at least one demo, e.g. nukepug");
  await setFields(names);
}

async function setFields(demos) {
  const missing = demos.filter((d) => !published.has(d));
  if (missing.length) fail(`not exported: ${missing.join(", ")}`);

  const change = {};
  if (flags.has("--date")) {
    if (!validDate(flags.get("--date"))) fail(`--date wants YYYY-MM-DD, not ${flags.get("--date")}`);
    change.date = flags.get("--date");
  }
  if (flags.has("--kind")) {
    if (!KINDS.includes(flags.get("--kind"))) fail(`--kind is ${KINDS.join(" or ")}`);
    change.kind = flags.get("--kind");
  }
  if (flags.has("--visibility")) {
    if (!VISIBILITIES.includes(flags.get("--visibility"))) fail(`--visibility is ${VISIBILITIES.join(", ")}`);
    change.visibility = flags.get("--visibility");
  }
  for (const f of ["owner", "title", "event"]) if (flags.has(`--${f}`)) change[f] = String(flags.get(`--${f}`));
  if (flags.has("--sample")) change.sample = true;
  if (flags.has("--no-sample")) change.sample = false;
  if (!Object.keys(change).length) fail("nothing to set - give --date, --kind, --visibility, --owner, --sample, ...");

  for (const demo of demos) await write(demo, (info) => ({ ...info, ...change }));
}

async function backfillDates(source) {
  const s = await stat(source).catch(() => null);
  if (!s) fail(`no such file or folder: ${source}`);
  const files = s.isDirectory()
    ? (await readdir(source)).map((f) => join(source, f))
    : [source];

  let set = 0;
  for (const file of files) {
    const demo = demoNameOf(basename(file));
    if (!demo) continue;
    if (!published.has(demo)) { console.log(`  ${demo}: not exported, skipped`); continue; }
    const date = localDate((await stat(file)).mtimeMs);
    const info = await readInfo(demo);
    if (info.date && !flags.has("--force")) {
      console.log(`  ${demo}: already dated ${info.date}, kept (--force to replace with ${date})`);
      continue;
    }
    await write(demo, (i) => ({ ...i, date }), `date ${date} from ${basename(file)}'s modified time`);
    set++;
  }
  console.log(`\n${dryRun ? "would date" : "dated"} ${set} demo(s)`);
}

async function readInfo(demo) {
  return JSON.parse(await readFile(join(dataRoot, demo, "info.json"), "utf8").catch(() => "{}"));
}

async function write(demo, update, note) {
  const before = await readInfo(demo);
  const after = update(before);
  // The same checks index-demos applies, so a bad value fails here, where it
  // was typed, rather than at the next publish.
  const problems = infoProblems(after);
  if (problems.length) fail(`${demo}: ${problems.join("; ")}`);
  console.log(`  ${demo}: ${note ?? JSON.stringify(after)}${dryRun ? "  (dry run)" : ""}`);
  if (!dryRun) await writeFile(join(dataRoot, demo, "info.json"), JSON.stringify(after, null, 2) + "\n");
}

function usage() {
  console.log(HELP);
  process.exit(args.length === 0 ? 1 : 0);
}



function fail(message) {
  console.error(message);
  process.exit(1);
}
