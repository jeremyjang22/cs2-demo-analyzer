"""Load a CS2 radar overview image and map world coordinates onto it.

Valve ships each map's radar as a .png plus a .txt describing how the
screenshot was taken. The three fields that matter are pos_x / pos_y (the
world coordinate of the image's top-left pixel) and scale (world units per
pixel). Everything else in the .txt is loading-screen decoration.

    pixel_x = (world_x - pos_x) / scale
    pixel_y = (pos_y - world_y) / scale

Note the y flip: world y grows north, image y grows downward.
"""

import re
from pathlib import Path

import matplotlib.pyplot as plt
from PIL import Image


class RadarMap:
    """A map's radar image plus the world -> image transform for it."""

    def __init__(self, map_name: str, radar_dir: Path):
        self.map_name = map_name
        self.image_path = Path(radar_dir) / f"{map_name}.png"
        self.meta_path = Path(radar_dir) / f"{map_name}.txt"

        for path in (self.image_path, self.meta_path):
            if not path.exists():
                raise FileNotFoundError(
                    f"missing radar asset for {map_name}: {path}\n"
                    f"add {map_name}.png and {map_name}.txt to {Path(radar_dir)} "
                    f"— see the README there for where to get them"
                )

        meta = _parse_overview_txt(self.meta_path)
        self.pos_x = float(meta["pos_x"])
        self.pos_y = float(meta["pos_y"])
        self.scale = float(meta["scale"])

        # Multi-level maps split by altitude. Every section shares one
        # pos_x/pos_y/scale — only the background image differs — so the
        # world->pixel transform below is the same whichever floor you are on.
        self.sections = {}
        for name, bounds in (meta.get("verticalsections") or {}).items():
            if isinstance(bounds, dict):
                self.sections[name] = (float(bounds["AltitudeMin"]),
                                       float(bounds["AltitudeMax"]))

        self.images = {"default": Image.open(self.image_path)}
        for name in self.sections:
            if name == "default":
                continue
            extra = self.image_path.with_name(f"{map_name}_{name}.png")
            if not extra.exists():
                raise FileNotFoundError(
                    f"{map_name} declares a '{name}' section but {extra} is missing"
                )
            self.images[name] = Image.open(extra)

        self.image = self.images["default"]
        self.width, self.height = self.image.size

    @property
    def is_multi_level(self) -> bool:
        return len(self.images) > 1

    @property
    def section_names(self) -> list:
        """Section names, "default" first, then the rest highest floor down.

        Callers index into this list to identify a floor, so the order is part
        of the contract — keep it stable.
        """
        rest = sorted((n for n in self.images if n != "default"),
                      key=lambda n: -self.sections[n][1])
        return ["default"] + rest

    def section_for(self, z) -> str:
        """Which radar image a point at altitude `z` belongs on."""
        for name, (lo, hi) in self.sections.items():
            if lo <= z < hi:
                return name
        return "default"

    @property
    def extent(self):
        """Image bounds in world units, as matplotlib's (left, right, bottom, top).

        Passing this to imshow lets callers plot raw world x/y with no manual
        conversion — matplotlib places the pixels for us.
        """
        left = self.pos_x
        right = self.pos_x + self.width * self.scale
        top = self.pos_y
        bottom = self.pos_y - self.height * self.scale
        return (left, right, bottom, top)

    def world_to_pixel(self, world_x, world_y):
        """Convert world coordinates to image pixel coordinates.

        Accepts scalars or pandas/numpy arrays. Only needed if you are drawing
        onto the image directly; matplotlib callers should use `extent`.
        """
        return (world_x - self.pos_x) / self.scale, (self.pos_y - world_y) / self.scale

    def draw(self, ax=None, dim=0.55, section="default"):
        """Draw the radar as a plot background and return the axes.

        `dim` fades the radar so trajectory colors stay readable on top of it.
        `section` picks the floor on multi-level maps — see `section_for`.
        """
        if ax is None:
            _, ax = plt.subplots(figsize=(10, 10))

        image = self.images.get(section, self.image)
        ax.imshow(image, extent=self.extent, origin="upper", alpha=dim, zorder=0)
        ax.set_aspect("equal")
        ax.set_xlim(self.extent[0], self.extent[1])
        ax.set_ylim(self.extent[2], self.extent[3])
        ax.axis("off")
        return ax


def _parse_overview_txt(path: Path) -> dict:
    """Parse a Valve overview .txt (KeyValues) into a nested dict.

    This has to handle nesting, not just flat pairs: multi-level maps (Nuke,
    Vertigo, Train) carry a "verticalsections" block with one sub-block per
    floor. Flattening it would let the second section's AltitudeMin/Max
    overwrite the first's, leaving no way to tell the floors apart.
    """
    text = re.sub(r"//[^\n]*", "", path.read_text(encoding="utf-8", errors="replace"))
    tokens = [quoted or brace for quoted, brace in re.findall(r'"([^"]*)"|([{}])', text)]

    pos = 0

    def block() -> dict:
        nonlocal pos
        out = {}
        while pos < len(tokens):
            key = tokens[pos]
            pos += 1
            if key == "}":
                break
            if pos < len(tokens) and tokens[pos] == "{":
                pos += 1
                out[key] = block()
            elif pos < len(tokens):
                out[key] = tokens[pos]
                pos += 1
        return out

    root = block()
    # The file wraps everything in a single block named after the map.
    while len(root) == 1 and isinstance(next(iter(root.values())), dict):
        root = next(iter(root.values()))

    missing = {"pos_x", "pos_y", "scale"} - root.keys()
    if missing:
        raise ValueError(f"{path} is missing required key(s): {sorted(missing)}")
    return root
