/**
 * Fetch CS2's equipment icons into public/equipment/ and record their shapes.
 *
 * Run by hand (`npm run vendor-equipment`), not on every build: the output is
 * committed, so a build never depends on GitHub being up. Re-run it only to
 * pick up a weapon the game added, after moving SOURCE_COMMIT forward.
 *
 * Where these come from, said plainly: they are Valve's HUD icons, extracted
 * from the CS2 game files. We take them from community CS2 projects that ship
 * them, each pinned to one commit so the set cannot change underneath us:
 * drweissbrot/cs-hud, a widely used broadcast overlay, for the equipment, and
 * DrEAmSs59/CS2-insight-agent for the kill-feed marks (headshot, wallbang),
 * which cs-hud does not carry. Those repos' licences cover their code, not
 * this art; the art is Valve's, used the way community CS tools commonly use
 * it. Chosen deliberately over hand-drawn silhouettes so a gun reads as that
 * gun rather than as its class.
 *
 * Alongside the files it writes src/equipment-sizes.json: each icon's
 * width-to-height ratio from its viewBox. The icons are drawn as CSS masks so
 * they can take any colour, and a mask cannot size itself the way an <img>
 * can, so the width has to be known up front. An AK-47 is nearly three times
 * wider than it is tall, a grenade under half.
 */
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** Game file names, which is what the icons are called upstream. */
const EQUIPMENT = [
  // rifles
  "ak47", "m4a1", "m4a1_silencer", "galilar", "famas", "sg556", "aug",
  // snipers
  "awp", "ssg08", "scar20", "g3sg1",
  // SMGs
  "mp9", "mac10", "mp7", "mp5sd", "ump45", "p90", "bizon",
  // shotguns and machine guns
  "nova", "xm1014", "mag7", "sawedoff", "m249", "negev",
  // pistols
  "glock", "usp_silencer", "hkp2000", "p250", "fiveseven", "tec9", "cz75a",
  "deagle", "revolver", "elite", "taser",
  // melee
  "knife",
  // grenades
  "hegrenade", "flashbang", "smokegrenade", "molotov", "incgrenade", "decoy",
  // gear
  "c4", "defuser", "kevlar", "assaultsuit",
];

const SOURCES = [
  {
    repo: "drweissbrot/cs-hud",
    commit: "5595dd02d67f0ca674d96d8c629e067ec6528c1b",
    dir: "src/themes/fennec/img/weapons",
    files: EQUIPMENT,
  },
  {
    repo: "DrEAmSs59/CS2-insight-agent",
    commit: "84e985629fe35e3ab89709ab4b414d847650a836",
    dir: "frontend/public/hud-death-notice",
    files: ["headshot", "penetrate"],
  },
];

/**
 * These files are served from our origin, so anything executable in one would
 * run as us. They are drawings and should contain nothing but shapes; refuse
 * the whole set rather than ship one that does not.
 */
const UNSAFE = /<script|\son[a-z]+\s*=|javascript:|<foreignObject|href\s*=/i;

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "public", "equipment");
const sizesFile = join(here, "..", "src", "equipment-sizes.json");

const fetched = await Promise.all(
  SOURCES.flatMap(({ repo, commit, dir, files }) =>
    files.map(async (name) => {
      const url = `https://raw.githubusercontent.com/${repo}/${commit}/${dir}/${name}.svg`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(`${name}.svg: HTTP ${res.status} from ${url}`);
      return { name, svg: await res.text() };
    }),
  ),
);

const sizes = {};
for (const { name, svg } of fetched) {
  if (UNSAFE.test(svg)) throw new Error(`${name}.svg contains something executable; refusing the set`);
  const vb = svg.match(/viewBox="\s*[-\d.]+[\s,]+[-\d.]+[\s,]+([\d.]+)[\s,]+([\d.]+)\s*"/);
  if (!vb) throw new Error(`${name}.svg has no viewBox, so its proportions are unknown`);
  sizes[name] = Math.round((Number(vb[1]) / Number(vb[2])) * 1000) / 1000;
}

await mkdir(outDir, { recursive: true });
await Promise.all(fetched.map(({ name, svg }) => writeFile(join(outDir, `${name}.svg`), svg)));
await writeFile(sizesFile, JSON.stringify(sizes, null, 2) + "\n");
const from = SOURCES.map((s) => `${s.repo}@${s.commit.slice(0, 7)}`).join(", ");
console.log(`equipment: ${fetched.length} icons from ${from}`);
