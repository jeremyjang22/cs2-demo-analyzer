"""Export a demo's movement payload as JSON for the web app.

    python export_movement.py --demo anubispug
    python export_movement.py --demo a b c --out ../../packages/web/public/data

Writes <out>/<demo>/movement.json. Unlike the standalone HTML generator, this
keeps the data separate from the app that draws it: the radar images are
referenced by map name and served as static files rather than base64'd into
every rebuild.
"""

import argparse
import json
from pathlib import Path

import movement_payload as mp
from radar import RadarMap

WEB_DATA = mp.REPO_ROOT / "packages" / "web" / "public" / "data"


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--demo", nargs="+", metavar="NAME",
                        help="demo folder name(s) under out/, e.g. --demo anubispug")
    parser.add_argument("--demo-dir", type=Path, nargs="+", metavar="PATH",
                        help="explicit path(s), if the output lives outside out/")
    parser.add_argument("--out", type=Path, default=WEB_DATA,
                        help=f"output root (default {WEB_DATA})")
    parser.add_argument("--hz", type=float, default=mp.DEFAULT_HZ,
                        help=f"samples per second (default {mp.DEFAULT_HZ:g})")
    parser.add_argument("--merge", metavar="NAME",
                        help="write all demos into one payload under this name, "
                             "instead of one file each")
    args = parser.parse_args()

    demo_dirs = mp.resolve_demo_dirs(args.demo, args.demo_dir)
    groups = {args.merge: demo_dirs} if args.merge else {d.name: [d] for d in demo_dirs}

    for name, dirs in groups.items():
        manifest = json.loads((dirs[0] / "manifest.json").read_text())
        radar = RadarMap(manifest["map"], mp.RADAR_DIR)
        payload = mp.build_payload(dirs, radar, args.hz)

        out = Path(args.out) / name
        out.mkdir(parents=True, exist_ok=True)
        path = out / "movement.json"
        path.write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")

        positions = sum(len(r["x"]) for p in payload["players"] for r in p["rounds"])
        print(f"{name}: {len(payload['rounds'])} rounds | {len(payload['players'])} players | "
              f"{positions:,} positions @ {args.hz:g} Hz | {path.stat().st_size/1024:.0f} KB")


if __name__ == "__main__":
    main()
