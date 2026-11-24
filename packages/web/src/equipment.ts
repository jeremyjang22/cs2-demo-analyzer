/**
 * Which CS2 icon draws which piece of equipment.
 *
 * The icons are the game's own, vendored into public/equipment/ by
 * scripts/vendor-equipment.mjs, which also says where they come from. This
 * module is only the lookup: the names the collector writes on one side, the
 * game's file names on the other. The roster and the kill feed both read it,
 * so a gun looks the same in a player's hands and in the kill it made.
 */
import SIZES from "./equipment-sizes.json";

/**
 * Display name to icon file. Keyed on the exact strings the collector writes,
 * which are demoinfocs' display names.
 *
 * The M4s are the trap. The game files call the M4A4 `m4a1` and the M4A1-S
 * `m4a1_silencer`, while demoinfocs calls the M4A1-S plain "M4A1". So "M4A1"
 * here must map to the silenced file, not to the file of the same name. Both
 * strings occur in the reference demos.
 */
const WEAPON_FILES: Record<string, string> = {
  "AK-47": "ak47",
  "M4A4": "m4a1",
  "M4A1": "m4a1_silencer",
  "M4A1-S": "m4a1_silencer",
  "Galil AR": "galilar",
  "FAMAS": "famas",
  "SG 553": "sg556",
  "AUG": "aug",
  "AWP": "awp",
  "SSG 08": "ssg08",
  "SCAR-20": "scar20",
  "G3SG1": "g3sg1",
  "MP9": "mp9",
  "MAC-10": "mac10",
  "MP7": "mp7",
  "MP5-SD": "mp5sd",
  "UMP-45": "ump45",
  "P90": "p90",
  "PP-Bizon": "bizon",
  "Nova": "nova",
  "XM1014": "xm1014",
  "MAG-7": "mag7",
  "Sawed-Off": "sawedoff",
  "M249": "m249",
  "Negev": "negev",
  "Glock-18": "glock",
  "USP-S": "usp_silencer",
  "P2000": "hkp2000",
  "P250": "p250",
  "Five-SeveN": "fiveseven",
  "Tec-9": "tec9",
  "CZ75 Auto": "cz75a",
  "Desert Eagle": "deagle",
  "R8 Revolver": "revolver",
  "Dual Berettas": "elite",
  "Zeus x27": "taser",
  "Knife": "knife",
  "HE Grenade": "hegrenade",
  "Flashbang": "flashbang",
  "Smoke Grenade": "smokegrenade",
  "Molotov": "molotov",
  "Incendiary Grenade": "incgrenade",
  "Decoy Grenade": "decoy",
  "C4": "c4",
};

/** The icon file for a weapon, or undefined for one this table has not met. */
export function weaponFile(name: string): string | undefined {
  return WEAPON_FILES[name];
}

/**
 * Grenade code to icon and colour, keyed by the characters the collector packs
 * into the `nades` string. Colours match the circles the map draws for the
 * same grenades, so a pip here and a bloom there agree.
 */
export const NADES: Record<string, { file: string; color: string; title: string }> = {
  h: { file: "hegrenade", color: "#ff6b57", title: "HE grenade" },
  f: { file: "flashbang", color: "#ffe9a3", title: "Flashbang" },
  s: { file: "smokegrenade", color: "#d6dee6", title: "Smoke" },
  m: { file: "molotov", color: "#ff8a3d", title: "Molotov" },
  i: { file: "incgrenade", color: "#ffab60", title: "Incendiary" },
  d: { file: "decoy", color: "#aab4c2", title: "Decoy" },
};

/** Gear that is not a weapon. */
export const GEAR = {
  armor: "kevlar",
  armorHelmet: "assaultsuit",
  kit: "defuser",
  bomb: "c4",
} as const;

/** The kill feed's marks for how a kill happened. */
export const MARKS = {
  headshot: "headshot",
  wallbang: "penetrate",
} as const;

/**
 * Width in pixels of an icon drawn `height` pixels tall, from its viewBox.
 * Falls back to square for a file with no recorded size, which only happens if
 * this module and the vendored set drift apart; the tests guard that.
 */
export function iconWidth(file: string, height: number): number {
  const ratio = (SIZES as Record<string, number>)[file] ?? 1;
  return Math.round(height * ratio);
}

/** Every file this module can ask for. For the tests, and nothing else. */
export function referencedFiles(): string[] {
  return [
    ...Object.values(WEAPON_FILES),
    ...Object.values(NADES).map((n) => n.file),
    ...Object.values(GEAR),
    ...Object.values(MARKS),
  ];
}

/** Every file the vendored set has a size for. */
export function vendoredFiles(): string[] {
  return Object.keys(SIZES);
}
