/**
 * Copy radar assets into public/ before dev and build.
 *
 * assets/radar/ is the single source: the collector, the Python plotting tools
 * and this app all read the same files, and committing a second copy here would
 * mean two sets to keep in step. public/radar/ is generated and gitignored.
 *
 * copyFile rather than cp: cp replicates the source's mode, and the chmod that
 * needs fails with EPERM on a OneDrive-backed working copy.
 */
import { copyFile, mkdir, readdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const source = join(here, "..", "..", "..", "assets", "radar");
const target = join(here, "..", "public", "radar");

const files = (await readdir(source)).filter((f) => f.endsWith(".png"));
if (files.length === 0) {
  console.error(`No radar images in ${source} — every map would render blank.`);
  process.exit(1);
}

await mkdir(target, { recursive: true });
await Promise.all(files.map((f) => copyFile(join(source, f), join(target, f))));
console.log(`radar: synced ${files.length} images from assets/radar`);
