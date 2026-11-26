/**
 * Push exported demo payloads to the R2 bucket the Worker reads.
 *
 *   node scripts/upload-data.mjs              # every demo in public/data
 *   node scripts/upload-data.mjs nukepug      # just these
 *   node scripts/upload-data.mjs --dry-run    # say what would change, change nothing
 *   node scripts/upload-data.mjs --unlist x   # take a demo off the site
 *   node scripts/upload-data.mjs --fresh      # first ever publish: no index yet
 *
 * This is deliberately NOT part of CI. The payloads are built from .dem files
 * that never enter the repo — CI has no way to produce them and nothing to
 * check them against, so publishing data stays a thing a person does on
 * purpose, from the machine that has the demos.
 *
 * Any machine can publish. The index (data/index.json) describes the BUCKET,
 * so it is merged, not rebuilt: what is already published stays listed, and
 * only the demos uploaded now are added or replaced (mergeIndex). It used to
 * be rebuilt from this machine's folders alone, and an upload from a second
 * machine would have published an index of one demo - the Worker serves
 * nothing the index does not list, so every other demo would have vanished.
 * If the published index cannot be read, nothing is uploaded.
 *
 * Keys mirror the local paths exactly: public/data/nukepug/movement.json is
 * stored as data/nukepug/movement.json, which is the same URL the app fetches
 * in dev off Vite's static server. That symmetry is the whole trick — no code
 * path differs between local and production.
 */
import { spawnSync } from "node:child_process";
import { mkdtemp, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, posix } from "node:path";
import { fileURLToPath } from "node:url";
import { mergeIndex } from "./demo-info.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const dataRoot = join(here, "..", "public", "data");

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const fresh = args.includes("--fresh");
const unlist = [];
const wanted = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--unlist") {
    if (!args[i + 1] || args[i + 1].startsWith("--")) fail("--unlist needs a demo name");
    unlist.push(args[++i]);
  } else if (!args[i].startsWith("--")) {
    wanted.push(args[i]);
  }
}

// Read the bucket name out of wrangler.jsonc rather than repeating it, so the
// two can never drift into uploading somewhere the Worker does not read.
const bucket = await readBucketName(join(here, "..", "wrangler.jsonc"));

let local = [];
try {
  local = (await readdir(dataRoot, { withFileTypes: true }))
    .filter((e) => e.isDirectory())
    .map((e) => e.name);
} catch {
  if (!unlist.length) fail(`no ${dataRoot}\nExport a demo first:  cd packages && go run ./demo-export <file.dem>`);
}

// Only unlisting: nothing local is uploaded, whatever is on disk.
let demos = unlist.length && !wanted.length ? [] : local;
if (wanted.length) {
  const missing = wanted.filter((w) => !local.includes(w));
  if (missing.length) {
    fail(`not exported: ${missing.join(", ")}\navailable: ${local.join(", ") || "(none)"}`);
  }
  demos = wanted;
}
if (!demos.length && !unlist.length) fail(`${dataRoot} holds no demos`);

const uploads = [];
for (const demo of demos) {
  const file = join(dataRoot, demo, "movement.json");
  if ((await stat(file).catch(() => null)) === null) {
    console.log(`  ${demo}: no movement.json, skipped`);
    continue;
  }
  uploads.push([posix.join("data", demo, "movement.json"), file]);

  // Optional: a demo without it still replays - its analytics page just says
  // there is nothing published.
  const analytics = join(dataRoot, demo, "analytics.json");
  if ((await stat(analytics).catch(() => null)) !== null) {
    uploads.push([posix.join("data", demo, "analytics.json"), analytics]);
  }
}
const uploading = demos.filter((d) => uploads.some(([key]) => key === `data/${d}/movement.json`));

// This machine's entries, built (and checked) by index-demos from info.json
// and each payload. Only the uploaded demos' entries are used.
let localIndex = [];
if (uploading.length) {
  const r = spawnSync("node", [join(here, "index-demos.mjs")],
    { stdio: "inherit", shell: process.platform === "win32" });
  if (r.status !== 0) fail("could not build the demo index");
  localIndex = JSON.parse(await readFile(join(dataRoot, "index.json"), "utf8")).demos;
}

const tmp = await mkdtemp(join(tmpdir(), "upload-data-"));
try {
  const published = await readPublishedIndex(join(tmp, "published.json"));
  const merged = mergeIndex(published, localIndex, uploading, unlist);
  const indexFile = join(tmp, "index.json");
  await writeFile(indexFile, JSON.stringify({ demos: merged.demos }, null, 2) + "\n");

  console.log(`\n${dryRun ? "would publish" : "publishing"} → r2://${bucket}`);
  const say = (label, names) => names.length && console.log(`  ${label}: ${names.join(", ")}`);
  say("add", merged.added);
  say("replace", merged.replaced);
  say("unlist", merged.removed);
  say("keep as published", merged.kept);
  for (const name of unlist.filter((n) => !merged.removed.includes(n))) {
    console.log(`  (${name} is not published; nothing to unlist)`);
  }
  console.log("");

  // Who each demo will be visible to, so the line that says what is being
  // published also says to whom.
  const access = new Map(merged.demos.map((d) => [d.name, d.visibility === "public" ? "public"
    : `${d.visibility ?? "private"}${d.owner ? ` to ${d.owner}` : ", NO OWNER"}`]));

  let total = 0;
  for (const [key, file] of [...uploads, ["data/index.json", indexFile]]) {
    const size = (await stat(file)).size;
    total += size;
    const demo = key.split("/")[1];
    const who = key.endsWith("/movement.json") ? `  [${access.get(demo) ?? "?"}]` : "";
    console.log(`  ${key}  ${(size / 1024).toFixed(0)} KB${who}`);
    if (dryRun) continue;

    // --remote targets the real bucket; without it wrangler writes to the local
    // simulator and the upload silently goes nowhere the Worker can see it.
    // The local wrangler from devDependencies, so the version that uploads is
    // the version in the lockfile rather than whatever npx resolves today.
    // The index goes last: a payload is never listed before it is there.
    const r = wrangler(["r2", "object", "put", `${bucket}/${key}`,
      "--file", file, "--content-type", "application/json", "--remote"], "inherit");
    if (r.status !== 0) fail(`wrangler exited ${r.status} uploading ${key}`);
  }
  if (merged.removed.length) {
    console.log("\nUnlisted demos are no longer served, but their files stay in the bucket.");
  }
  console.log(`\n${dryRun ? "would upload" : "uploaded"} ${(total / 1024 / 1024).toFixed(1)} MB`);
} finally {
  await rm(tmp, { recursive: true, force: true });
}

/**
 * The index as published now. Fails the whole upload if it cannot be read:
 * publishing without it would replace a full index with this machine's part.
 * --fresh is the one exception, for a bucket that has never had one.
 */
async function readPublishedIndex(file) {
  if (fresh) return [];
  const r = wrangler(["r2", "object", "get", `${bucket}/data/index.json`, "--file", file, "--remote"], "pipe");
  if (r.status !== 0) {
    const why = `${r.stdout ?? ""}${r.stderr ?? ""}`.trim().split("\n").slice(-3).join("\n");
    fail(`could not read the published index, so nothing was uploaded:\n${why}\n\n` +
      "Signed in?  npx wrangler login\n" +
      "If nothing has ever been published to this bucket, run again with --fresh.");
  }
  return JSON.parse(await readFile(file, "utf8")).demos ?? [];
}

function wrangler(argv, stdio) {
  return spawnSync("npx", ["wrangler", ...argv],
    { stdio, encoding: "utf8", shell: process.platform === "win32" });
}

async function readBucketName(configPath) {
  const text = await readFile(configPath, "utf8");
  // Enough of a JSONC reader for this one field: strip // comments, then match.
  const stripped = text.replace(/^\s*\/\/.*$/gm, "");
  const match = stripped.match(/"bucket_name"\s*:\s*"([^"]+)"/);
  if (!match) fail(`could not find bucket_name in ${configPath}`);
  return match[1];
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
