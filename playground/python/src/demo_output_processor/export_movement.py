"""Export a demo's movement payload as JSON for the web app.

    python export_movement.py --demo anubispug
    python export_movement.py --demo a b c --out ../../packages/web/public/data

Writes <out>/<demo>/movement.json, and an info.json beside it holding what
the demo itself cannot say: who may see it once published (--visibility,
--owner), whether it is a pug or a pro match (--kind), and optionally the
event, date and a readable title.

    python export_movement.py --demo nukepug --visibility private --owner 7656119...
    python export_movement.py --demo-dir ../../out/1-604132bd-... --kind pug --visibility public
    python export_movement.py --demo falcons-vitality-m1 --kind match --visibility public \
        --event "IEM Example 2026" --date 2026-11-02 --title "Falcons vs Vitality, map 1"

A demo published with no --visibility is private to nobody: the site fails
closed rather than show ten people's match to the world by default.

Unlike the standalone HTML generator, this
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
    parser.add_argument("--kind", choices=["pug", "match"],
                        help="what sort of game this is: a pug (FACEIT and the like) or a "
                             "pro match. DECLARED, never guessed from the file name; written "
                             "to info.json, which can be edited later without re-exporting")
    parser.add_argument("--event", help="with --kind: tournament or league, for pro matches")
    parser.add_argument("--date", help="with --kind: when it was played, YYYY-MM-DD")
    parser.add_argument("--title", help="with --kind: a readable name for lists, in place of "
                                         "the folder name")
    parser.add_argument("--visibility", choices=["public", "link", "private"],
                        help="who may see it on the site: public (listed for everyone), link "
                             "(anyone with the name, listed for the owner), private (owner "
                             "only). Undeclared means private")
    parser.add_argument("--owner", metavar="STEAMID",
                        help="64-bit steamid of the account that owns it on the site; "
                             "needed for link and private")
    args = parser.parse_args()
    if args.owner and not (args.owner.isdigit() and len(args.owner) == 17):
        parser.error(f"--owner wants a 64-bit steamid (17 digits), not {args.owner}")
    if (args.event or args.date or args.title) and not args.kind:
        parser.error("--event, --date and --title describe a demo; give --kind as well")

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

        if args.kind or args.visibility or args.owner:
            write_info(out / "info.json", args)

        positions = sum(len(r["x"]) for p in payload["players"] for r in p["rounds"])
        print(f"{name}: {len(payload['rounds'])} rounds | {len(payload['players'])} players | "
              f"{positions:,} positions @ {args.hz:g} Hz | {path.stat().st_size/1024:.0f} KB")


def write_info(path: Path, args) -> None:
    """Record the facts about a demo that only a person knows.

    Kept beside movement.json rather than inside it: the payload is what was
    measured, this is what someone declared, and it can be corrected by editing
    one small file. Fields not given on this run keep their previous values.
    """
    info = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
    for field in ("kind", "event", "date", "title", "visibility", "owner"):
        value = getattr(args, field)
        if value:
            info[field] = value
    path.write_text(json.dumps(info, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
