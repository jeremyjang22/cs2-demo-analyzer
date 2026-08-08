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
                    f"missing radar asset {path} — see data/radar/README.md"
                )

        meta = _parse_overview_txt(self.meta_path)
        self.pos_x = float(meta["pos_x"])
        self.pos_y = float(meta["pos_y"])
        self.scale = float(meta["scale"])

        self.image = Image.open(self.image_path)
        self.width, self.height = self.image.size

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

    def draw(self, ax=None, dim=0.55):
        """Draw the radar as a plot background and return the axes.

        `dim` fades the radar so trajectory colors stay readable on top of it.
        """
        if ax is None:
            _, ax = plt.subplots(figsize=(10, 10))

        ax.imshow(self.image, extent=self.extent, origin="upper", alpha=dim, zorder=0)
        ax.set_aspect("equal")
        ax.set_xlim(self.extent[0], self.extent[1])
        ax.set_ylim(self.extent[2], self.extent[3])
        ax.axis("off")
        return ax


def _parse_overview_txt(path: Path) -> dict:
    """Pull "key" "value" pairs out of a Valve overview .txt (a KeyValues file).

    A real KeyValues parser is overkill here: the file is one flat block and we
    only need three scalars from it. Comments trail the values, so anchor the
    match on the quoted pair and ignore the rest of the line.
    """
    text = path.read_text(encoding="utf-8", errors="replace")
    pairs = re.findall(r'"([^"]+)"\s+"([^"]*)"', text)
    meta = dict(pairs)

    missing = {"pos_x", "pos_y", "scale"} - meta.keys()
    if missing:
        raise ValueError(f"{path} is missing required key(s): {sorted(missing)}")
    return meta
