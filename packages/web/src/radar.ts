/**
 * World -> radar-image coordinates.
 *
 * Valve ships each map's radar as a .png plus a .txt describing how the
 * screenshot was taken. Three fields matter: pos_x / pos_y (the world
 * coordinate of the image's top-left pixel) and scale (world units per pixel).
 * The exporter copies those into the payload so this never has to reimplement
 * the constants — see radar.py for the Python side.
 *
 * Note the y flip: world y grows north, image y grows downward. Getting that
 * backwards mirrors the map vertically and still looks plausible, which is
 * exactly why it is tested.
 */

import type { RadarMeta } from "./types";

/** Converts world coordinates to pixels within a `panelPx` square canvas. */
export interface Projector {
  x(worldX: number): number;
  y(worldY: number): number;
}

export function makeProjector(radar: RadarMeta, panelPx: number): Projector {
  const k = panelPx / radar.size;
  return {
    x: (wx) => ((wx - radar.pos_x) / radar.scale) * k,
    y: (wy) => ((radar.pos_y - wy) / radar.scale) * k,
  };
}

/** Filename for a floor's radar image, matching the assets/radar layout. */
export function radarImage(map: string, section: string): string {
  return section === "default" ? `${map}.png` : `${map}_${section}.png`;
}
