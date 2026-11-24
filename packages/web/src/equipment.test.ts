import { describe, expect, it } from "vitest";
import { GEAR, iconWidth, MARKS, NADES, referencedFiles, vendoredFiles, weaponFile } from "./equipment";

describe("weaponFile", () => {
  // Every weapon string seen in the reference demos. A name missing here
  // renders as text with no icon, which is easy to not notice.
  const observed = [
    "AK-47", "AWP", "C4", "Desert Eagle", "Dual Berettas", "FAMAS", "Five-SeveN",
    "Galil AR", "Glock-18", "HE Grenade", "Incendiary Grenade", "Knife", "M4A1",
    "M4A4", "MAC-10", "MP7", "MP9", "Molotov", "P2000", "P250", "SSG 08",
    "Tec-9", "USP-S", "XM1014",
  ];

  it.each(observed)("has an icon for %s", (name) => {
    expect(weaponFile(name)).toBeDefined();
  });

  it("keeps the two M4s apart", () => {
    // demoinfocs' "M4A1" is the silenced rifle; the game's m4a1 file is the M4A4.
    expect(weaponFile("M4A1")).toBe("m4a1_silencer");
    expect(weaponFile("M4A4")).toBe("m4a1");
  });

  it("returns undefined for a weapon it has not met", () => {
    expect(weaponFile("Laser Rifle")).toBeUndefined();
  });
});

describe("the vendored set", () => {
  it("has a size for every file the lookup can ask for", () => {
    const have = new Set(vendoredFiles());
    expect(referencedFiles().filter((f) => !have.has(f))).toEqual([]);
  });

  it("has every file on disk", () => {
    // Not imported, only listed: the keys are the files that exist.
    const onDisk = new Set(
      Object.keys(import.meta.glob("../public/equipment/*.svg"))
        .map((p) => p.replace(/^.*\/(.+)\.svg$/, "$1")),
    );
    expect(vendoredFiles().filter((f) => !onDisk.has(f))).toEqual([]);
  });

  it("covers every grenade code, piece of gear and kill mark", () => {
    expect(Object.keys(NADES).sort()).toEqual(["d", "f", "h", "i", "m", "s"]);
    expect(Object.values(GEAR)).toHaveLength(4);
    expect(Object.keys(MARKS).sort()).toEqual(["headshot", "wallbang"]);
  });
});

describe("iconWidth", () => {
  it("draws a rifle wide and a grenade narrow at the same height", () => {
    expect(iconWidth("ak47", 11)).toBeGreaterThan(25);
    expect(iconWidth("hegrenade", 11)).toBeLessThan(11);
  });

  it("falls back to square for an unknown file", () => {
    expect(iconWidth("nope", 12)).toBe(12);
  });
});
