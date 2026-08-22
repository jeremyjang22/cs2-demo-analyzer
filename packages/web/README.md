# Web viewer

React + TypeScript front end for the movement viewer. Data and app are separate:
Python writes `movement.json`, this fetches it.

## Run

```sh
npm install                        # once
npm run dev                        # http://localhost:5173
```

Pick a demo with `?demo=<folder name>`, e.g. `/?demo=nukepug`. Default is
`anubispug`.

## Getting data in

From `playground/python/src/demo_output_processor`:

```sh
python export_movement.py --demo nukepug        # -> public/data/nukepug/movement.json
python export_movement.py --demo a b --merge pro-player   # several demos, one payload
```

Radar images live in `public/radar/`, copied from `assets/radar/`. Both are
gitignored under `public/data/`; the radar PNGs are committed.

## Layout

| Path | Role |
|---|---|
| `src/types.ts` | payload shape, mirrors `export_movement.py` |
| `src/radar.ts` | world → pixel transform, floor image names |
| `src/track.ts` | pure sampling over a round's samples |
| `src/renderer.ts` | canvas drawing and the playback clock — **not** a React component |
| `src/components/` | panel UI |

The renderer is a plain class on purpose. A 60 fps draw loop must not run
through React's reconciler, so React mounts it once, pushes view state down, and
receives throttled callbacks for the clock and the round table.

`track.ts` and `radar.ts` hold the logic worth testing — the wrap-safe yaw
interpolation, the run splitter, the y-flipped projection.

```sh
npm test        # vitest
npm run build   # typecheck + production bundle
```
