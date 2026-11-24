/**
 * Equipment icons: the game's own, drawn as CSS masks.
 *
 * These used to be hand-built silhouettes, one per weapon class, on the
 * grounds that Valve's art is not ours and that a faithful MP9 and a faithful
 * MP7 look alike at 12 pixels. That was reversed on purpose: in a roster
 * people read the gun's shape before its name, and the class silhouettes made
 * every rifle the same rifle. Where the files come from, and the licensing
 * position, is written at the top of scripts/vendor-equipment.mjs.
 *
 * A mask rather than an <img> so the icon takes `currentColor`, and a caller
 * sets colour with CSS the way it did with the old SVGs: grenades keep their
 * tints, a sidearm stays muted. The cost is that a mask cannot size itself, so
 * the width comes from the viewBox ratios recorded when the set was vendored.
 */
import { iconWidth } from "../equipment";

const ROOT = `${import.meta.env.BASE_URL}equipment/`;

interface Props {
  /** File name under public/equipment, without .svg. */
  file: string;
  /** Height in pixels. The width follows from the icon's proportions. */
  height: number;
  /** Accessible name. Omit when visible text beside the icon already says it. */
  title?: string;
}

export function GameIcon({ file, height, title }: Props) {
  const url = `url("${ROOT}${file}.svg")`;
  return (
    <span
      className="gicon"
      role={title ? "img" : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      style={{
        width: iconWidth(file, height),
        height,
        WebkitMaskImage: url,
        maskImage: url,
      }}
    />
  );
}
