# packages

Everything that is not exploration. Code graduates here from `playground/` once
something else depends on it.

| Package | Language | Role |
|---|---|---|
| `collector/` | Go | `.dem` → rounds, ticks, kills, utility, shots, damage, bomb, kits. The parsing core. |
| `round-collector/` | Go | CLI wrapper around `collector`. |
| `web/` | TypeScript | Demo viewer. Reads `movement.json`; draws nothing itself from a `.dem`. |

## The seam

The Go side and the web side never call each other. Go writes files; everything
downstream reads them.

```
data/<demo>.dem
      │  round-collector
      ▼
out/<demo>/          manifest.json · rounds · round_players · kills · utility
                     shots · damage · bomb · kits · ticks.csv.gz
      │  playground/python/…/export_movement.py
      ▼
packages/web/public/data/<demo>/movement.json
      │  fetched at runtime
      ▼
packages/web         the app, ~200 KB, cached independently of the data
```

That split is deliberate: the parsed output is the durable artifact, and any
consumer — the web app, a notebook, DuckDB — reads the same files rather than
going back to the demo.

## Running

```sh
# parse a demo
cd packages && go run ./round-collector -demo ../data/foo.dem -out ../out

# tests
go test ./...

# the viewer
cd packages/web && npm install && npm run dev
```

Output schema and its traps are documented in
[`docs/round-collector-schema.md`](../docs/round-collector-schema.md). Read the
"five things that will bite you" section before aggregating anything.
