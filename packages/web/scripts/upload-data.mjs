/**
 * Publish exported demos: `npm run upload-data` for what demo-catalog does.
 *
 *   node scripts/upload-data.mjs              # every demo in public/data
 *   node scripts/upload-data.mjs nukepug      # just these
 *   node scripts/upload-data.mjs --unlist x   # take a demo off the site
 *
 * The catalogue lives in the database now (packages/catalog,
 * docs/uploads.md): each demo is a row, and data/index.json is generated from
 * the rows and written to R2 by Go. This script used to build the index from
 * this machine's folders and merge it into the published one with wrangler;
 * that cannot work once uploads are processed on a machine that is not the
 * one publishing, so it hands over to `go run ./demo-catalog`, which reads
 * DATABASE_URL and the R2_* keys from packages/api/.env.
 *
 * Deliberately NOT part of CI: the payloads are built from .dem files that
 * never enter the repo, so publishing stays a thing a person does on purpose.
 */
import { spawnSync } from "node:child_process";
import { readdir, readFile, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, "..");
const dataRoot = join(web, "public", "data");
const packages = join(web, "..");

const args = process.argv.slice(2);
const unlist = [];
const wanted = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--unlist") {
    if (!args[i + 1] || args[i + 1].startsWith("--")) fail("--unlist needs a demo name");
    unlist.push(args[++i]);
  } else if (args[i] === "--dry-run" || args[i] === "--fresh") {
    fail(`${args[i]} is gone: the index is generated from the database now.\n` +
      "See what would change:  cd packages && go run ./demo-catalog publish -dry-run");
  } else if (!args[i].startsWith("--")) {
    wanted.push(args[i]);
  } else {
    fail(`unknown flag ${args[i]}`);
  }
}

// The bucket the Worker reads is the one wrangler.jsonc binds. Publishing
// anywhere else would succeed and show nothing, so the two must agree.
const bucket = await readBucketName(join(web, "wrangler.jsonc"));
if (process.env.R2_BUCKET && process.env.R2_BUCKET !== bucket) {
  fail(`R2_BUCKET is ${process.env.R2_BUCKET}, but the Worker reads ${bucket} (wrangler.jsonc)`);
}
const env = { ...process.env, R2_BUCKET: bucket };

if (unlist.length) go(["unlist", ...unlist]);

if (wanted.length || !unlist.length) {
  let demos = wanted;
  if (!demos.length) {
    const entries = await readdir(dataRoot, { withFileTypes: true }).catch(() => []);
    demos = [];
    for (const e of entries.filter((e) => e.isDirectory())) {
      if (await stat(join(dataRoot, e.name, "movement.json")).catch(() => null)) demos.push(e.name);
    }
    if (!demos.length) fail(`${dataRoot} holds no demos\nExport one first:  cd packages && go run ./demo-export <file.dem>`);
  }
  go(["put", ...demos]);
}

function go(argv) {
  const r = spawnSync("go", ["run", "./demo-catalog", ...argv],
    { cwd: packages, stdio: "inherit", env, shell: process.platform === "win32" });
  if (r.error) fail(`could not run go: ${r.error.message}`);
  if (r.status !== 0) process.exit(r.status ?? 1);
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
