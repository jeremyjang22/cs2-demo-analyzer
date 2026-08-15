# Radar overlays

Map overview images and their calibration metadata, used to plot world
coordinates from `ticks.csv.gz` onto a picture of the map.

Each map needs two files with matching names:

- `<map>.png` — the radar image (1024×1024 for de_mirage)
- `<map>.txt` — Valve's overview KeyValues file, which carries the
  `pos_x` / `pos_y` / `scale` fields the transform depends on

## Source

Downloaded from [2mlml/cs2-radar-images](https://github.com/2mlml/cs2-radar-images),
which mirrors the radar assets shipped in the CS2 game files.

## The transform

`pos_x` / `pos_y` are the world coordinates of the image's **top-left pixel**,
and `scale` is world units per pixel:

```
pixel_x = (world_x - pos_x) / scale
pixel_y = (pos_y - world_y) / scale   # y flips: world y grows north, image y grows down
```

For de_mirage that is `pos_x=-3230`, `pos_y=1713`, `scale=5.0`.

Verified against the data rather than assumed: the first tick of a round for a
CT player lands at normalized image coordinates (0.29, 0.70), against the
(0.28, 0.70) that the same `.txt` declares for `CTSpawn`.

The `bombA_*` / `bombB_*` / `*Spawn_*` entries in the `.txt` are approximate
loading-screen icon placements, not survey points — use them as a sanity check,
not as ground truth.

## Adding another map

Drop the two files in here named after the map (`de_inferno.png`,
`de_inferno.txt`); `radar.py` picks them up by the map name in `manifest.json`,
so no code change is needed.
