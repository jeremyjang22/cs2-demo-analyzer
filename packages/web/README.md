# Web viewer

React + TypeScript front end for the movement viewer. Data and app are separate:
Python writes `movement.json`, this fetches it.

## Run

```sh
npm install                        # once
npm run dev                        # http://localhost:5173
```

`/` is the home page: it lists what has been published and offers Steam
sign-in. A demo opens at `?demo=<folder name>`, e.g. `/?demo=nukepug`, with an
optional `&t=<seconds>` to land on a moment.

Routing is by query parameter rather than path, and that is forced rather than
chosen: `base: "./"` makes every asset URL relative, so a page served at
`/demo/nukepug` would ask for `/demo/assets/index.js` and get nothing. It also
means every `?demo=` link shared before the home page existed still works.

Sign-in needs the API. `npm run dev` proxies `/api/*` to `localhost:8080` —
the same prefix the Worker proxies in production, so one relative URL works in
both. To develop against a deployed API instead:

```sh
VITE_API_TARGET=https://<service>.code.run npm run dev
```

## Getting data in

From `playground/python/src/demo_output_processor`:

```sh
python export_movement.py --demo nukepug        # -> public/data/nukepug/movement.json
python export_movement.py --demo a b --merge pro-player   # several demos, one payload
```

Radar images live in `public/radar/`, copied from `assets/radar/`. Both are
gitignored under `public/data/`; the radar PNGs are committed.

`npm run build` deletes `dist/data` after Vite runs. Vite copies everything
under `public/` into the build, and `public/data` is only there so `vite dev`
serves payloads off the same URLs the Worker serves in production — shipping
them in the bundle would upload megabytes of stale data shadowing the R2 route
that answers for them. CI never had the files to begin with, so pruning is what
makes a local deploy produce the same thing CI does.

## Layout

| Path | Role |
|---|---|
| `src/route.ts` | which view the URL asks for — the whole router |
| `src/stats.ts` | K/D/A and ADR as of a moment, and the scoreboard order |
| `src/api.ts` | the backend as this app sees it: `/me`, login, logout |
| `src/types.ts` | payload shape, mirrors `export_movement.py` |
| `src/radar.ts` | world → pixel transform, floor image names |
| `src/track.ts` | pure sampling over a round's samples, including the loadout lookup |
| `src/effects.ts` | pure windowing for short-lived events: tracers, damage, bomb state |
| `src/renderer.ts` | canvas drawing and the playback clock — **not** a React component |
| `src/components/` | panel UI |

The renderer is a plain class on purpose. A 60 fps draw loop must not run
through React's reconciler, so React mounts it once, pushes view state down, and
receives throttled callbacks for the clock and the round table.

`track.ts`, `effects.ts` and `radar.ts` hold the logic worth testing — the
wrap-safe yaw interpolation, the run splitter, the y-flipped projection, and the
event window that has to stay correct when you scrub backwards.

## What's on the map

Beyond player dots, cones and utility:

- **The bomb.** A square, never a circle, so it is never mistaken for a player.
  Amber while carried (following the carrier, not the pickup spot), amber with a
  halo while loose on the ground, red and pulsing once planted — the pulse
  accelerates with the 40-second fuse. The panel names who has it.
- **Dropped defuse kits**, as blue diamonds — so the three things that can be
  on the floor read as three silhouettes: players are circles, the bomb is a
  square, a kit is a diamond. They appear where a CT died holding one and
  vanish when a teammate picks one up. Note these are *derived*: CS2 never
  networks a dropped kit as an entity, so the collector reconstructs them from
  the moment the pawn flag changes. See `docs/round-collector-schema.md`.
- **Health, cash and equipment**, per player, live. Health is a bar and a
  number, because the bar is what you read across ten rows and the number is
  what you need when the question is whether one more bullet does it; on the
  map a hurt player gets an arc around their dot, and a healthy one gets
  nothing, so "someone is hurt" stays impossible to miss. Equipment is icons:
  armour and helmet, defuse kit, the C4, both weapon slots and a pip per
  grenade. The weapon icons are per CLASS — rifle, sniper, SMG, shotgun,
  machine gun, pistol — drawn rather than shipped, because Valve's art is not
  ours to redistribute and at 12 pixels an MP9 and an MP7 are the same smudge
  anyway. The name beside the icon says which gun.
- **Grenade flight paths**, drawn as they fly rather than all at once: the line
  grows out of the thrower's hand and arrives when the grenade does, so it
  never gives away the landing spot before it lands. Shares the Utility toggle.
- **Death markers** where each player fell this round, in one grey rather than
  in player colours — ten coloured crosses would compete with ten coloured
  dots, and the question a death map answers is about the shape of the set.
  Read off the tracks: a track that ends in a death ends at the death.

The scoreboard is **live**: K/D/A and ADR are accumulated up to wherever the
clock is, not taken from the payload's final totals, and each team re-sorts by
kills as the match plays. ADR is built from damage the victim actually lost —
raw damage includes the over-damage of every killing blow and puts the
reference Anubis demo's mean at 103.6, which no player has ever posted.
- **Bullet tracers** for 0.35s, coloured by shooter. A tracer that ends in a dot
  hit somebody; one that fades out hit nothing we can locate — a demo only
  records where a shot stopped when it damaged a player, so the fade is the
  honest way to draw "that direction, distance unknown".
- **Damage markers** on whoever took the hit: an expanding ring sized by how
  much, and the number itself, coloured by source — bullet, HE, fire, fall,
  bomb. Simultaneous hits from one attacker (a shotgun blast) are one marker.

Tracers and damage share the "Fire" toggle; the bomb and dropped kits share
the "Bomb" toggle.

## The postround

Rounds play past their own win condition. CS2 gives survivors a flat seven
seconds before the next freeze time, and they are not idle: people run rifles
into corners to save them, pick up what the dead dropped, and get hunted down
doing it. The clock runs through all of it.

It is marked, not hidden — the map looks identical either side of the win
condition, and a viewer who cannot tell will read a save as a fight nobody is
winning:

- the transport shows why the round ended and how far past it you are
  (`bomb detonated · +2.2s`),
- the timeline hatches the tail of each chapter,
- a death in the tail is dimmed in the kill feed and tagged with its offset.

`RoundMeta.dur` still means contested play only, so anything asking "how long
was this round fought over" is unaffected; `post` carries the tail and
`chapterLength()` adds them.

```sh
npm test        # vitest
npm run build   # typecheck + production bundle
```
