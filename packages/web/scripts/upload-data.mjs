/**
 * Push exported demo payloads to the R2 bucket the Worker reads.
 *
 *   node scripts/upload-data.mjs              # every demo in public/data
 *   node scripts/upload-data.mjs nukepug      # just these
 *   node scripts/upload-data.mjs --dry-run
 *
 * This is deliberately NOT part of CI. The payloads are built from .dem files
 * that never enter the repo — CI has no way to produce them and nothing to
 * check them against, so publishing data stays a thing a person does on
 * purpose, from the machine that has the demos.
 *
 * Keys mirror the local paths exactly: public/data/nukepug/movement.json is
 * stored as data/nukepug/movement.json, which is the same URL the app fetches
 * in dev off Vite's static server. That symmetry is the whole trick — no code
 * path differs between local and production.
 */
import { spawnSync } from "node:child_process";
import { readdir, stat } from "node:fs/promises";
import { dirname, join, posix } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dataRoot = join(here, "..", "public", "data");

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const wanted = args.filter((a) => !a.startsWith("--"));

// Read the bucket name out of wrangler.jsonc rather than repeating it, so the
// two can never drift into uploading somewhere the Worker does not read.
const bucket = await readBucketName(join(here, "..", "wrangler.jsonc"));

let demos;
try {
  demos = (await readdir(dataRoot, { withFileTypes: true }))
    .filter((e) => e.isDirectory())
    .map((e) => e.name);
} catch {
  fail(`no ${dataRoot}\nExport a demo first:  python export_movement.py --demo <name>`);
}

if (wanted.length) {
  const missing = wanted.filter((w) => !demos.includes(w));
  if (missing.length) {
    fail(`not exported: ${missing.join(", ")}\navailable: ${demos.join(", ") || "(none)"}`);
  }
  demos = wanted;
}
if (!demos.length) fail(`${dataRoot} holds no demos`);

console.log(`${dryRun ? "would upload" : "uploading"} ${demos.length} demo(s) → r2://${bucket}\n`);

let total = 0;
for (const demo of demos) {
  const file = join(dataRoot, demo, "movement.json");
  const size = (await stat(file).catch(() => null))?.size;
  if (size === undefined) {
    console.log(`  ${demo}: no movement.json, skipped`);
    continue;
  }

  const key = posix.join("data", demo, "movement.json");
  console.log(`  ${key}  ${(size / 1024).toFixed(0)} KB`);
  total += size;
  if (dryRun) continue;

  // --remote targets the real bucket; without it wrangler writes to the local
  // simulator and the upload silently goes nowhere the Worker can see it.
  const r = spawnSync(
    "npx",
    ["--yes", "wrangler@4", "r2", "object", "put", `${bucket}/${key}`,
      "--file", file, "--content-type", "application/json", "--remote"],
    { stdio: "inherit", shell: process.platform === "win32" },
  );
  if (r.status !== 0) fail(`wrangler exited ${r.status} uploading ${key}`);
}

console.log(`\n${dryRun ? "would upload" : "uploaded"} ${(total / 1024 / 1024).toFixed(1)} MB`);

async function readBucketName(configPath) {
  const { readFile } = await import("node:fs/promises");
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
