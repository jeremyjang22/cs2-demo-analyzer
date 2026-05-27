# Rust playground

CS2 demo exploration using the Rust crate behind
[demoparser2](https://github.com/LaihoE/demoparser) (the underlying parser
that the published Python / JS / WASM packages wrap).

## Run

```sh
cargo build --release
./target/release/hello-demo ../../data/mega_ot_mirage.dem
# or, equivalently:
cargo run --release -- ../../data/mega_ot_mirage.dem
```

`--release` matters — the demo is ~700MB and debug builds will be glacial.

Output:
- Header (map, server, demo type)
- Player list (steam IDs from `player_md`)
- Player positions on tick 0
- Player positions on the first tick where `round_freeze_end` fires after
  `begin_new_match` (start of round 1 live play)

## Where the dependency comes from

There is **no published `demoparser2` crate on crates.io**. The upstream
[LaihoE/demoparser](https://github.com/LaihoE/demoparser) repo ships:

- `src/parser/` — Rust crate named **`parser`** (generic name, not
  published)
- `src/python/` — PyO3 wrapper that becomes `demoparser2` on PyPI
- `src/node/`, `src/wasm/` — other bindings

So this playground uses a git dependency pointing at a pinned commit, and
renames the crate locally via `package = "parser"`:

```toml
demoparser = { git = "https://github.com/LaihoE/demoparser", rev = "<sha>", package = "parser" }
```

`rev = "..."` pins it for reproducibility — `Cargo.lock` is checked in and
captures every transitive version too.

## Where the live-round filter lives

`src/main.rs`, function `parse_events` + the live-tick detection block in
`main`. Search for `round_freeze_end`.

The logic mirrors the Python playground: take the maximum
`begin_new_match` tick (CS2 fires it at tick 0 as a demo-start artifact
*and* at the real match start), then take the smallest `round_freeze_end`
strictly greater than that. For `mega_ot_mirage.dem` this lands at tick
**4238**, identical to the Python output.

## API notes (`parser` crate, git rev `54e320f`)

This Rust crate is **much lower-level** than the Python `demoparser2`
wrapper. There is no `DemoParser` struct with `parse_header()` /
`parse_ticks()` / `parse_event()` convenience methods. Instead:

1. Memory-map the demo: `parser::first_pass::parser_settings::create_mmap(path)`
2. Build a Huffman lookup table once: `create_huffman_lookup_table()`
3. Construct a `ParserInputs` describing what you want — props, events,
   ticks, etc. (15+ fields; defaults set in this playground via the
   `base_inputs` helper).
4. For the header alone:
   `FirstPassParser::new(&inputs).parse_header_only(bytes)` →
   `AHashMap<String, String>`
5. For anything else: `Parser::new(inputs, ParsingMode::Normal).parse_demo(bytes)`
   → `DemoOutput`.

`DemoOutput` is a struct full of:
- `header: Option<AHashMap<String, String>>`
- `player_md: Vec<PlayerEndMetaData>` — `{ steamid, name, team_number }`
- `game_events: Vec<GameEvent>` — `{ name, tick, fields: Vec<EventField> }`
- `df: AHashMap<u32, PropColumn>` — the "wide-table" of requested player
  props, keyed by prop id (which you look up via
  `output.prop_controller.prop_infos`)
- `df_per_player`, `projectiles`, `voice_data`, etc.

`PropColumn.data: Option<VarVec>` is a tagged union (`F32(Vec<Option<f32>>)`,
`String(Vec<Option<String>>)`, `XYZVec(...)`, ...). All `VarVec` variants
inside a single parse are index-aligned: index *i* of `X` matches index *i*
of `Y`, `Z`, `name`. Restricting `wanted_ticks` to one tick gives you one
row per player.

## What gets re-parsed

Each "section" of the output calls `parse_demo` again with a different
`ParserInputs`:

- Header — `FirstPassParser::parse_header_only` (the cheap path, stops at
  the file-header message)
- Players — full `parse_demo` with `only_header: true` (counter-intuitive
  flag name; it still walks far enough to populate `player_md`)
- Tick 0 / live-tick positions — full `parse_demo` with
  `wanted_player_props = ["X","Y","Z","name"]` and `wanted_ticks = [tick]`
- Events — full `parse_demo` with
  `wanted_events = ["round_freeze_end", "begin_new_match"]`

The Python wrapper does the same thing — every method on `DemoParser`
re-parses from scratch with different inputs. That's slow but it's the
shape of the underlying crate. If you want a single-pass run, you'd
combine everything into one `ParserInputs` and walk `DemoOutput` once.

## Cross-library findings

| | Python (demoparser2) | Go (demoinfocs-golang v5) | Rust (`parser`) |
|--|--|--|--|
| Map | `de_mirage` | `de_mirage` | `de_mirage` |
| Player count | 10 | 11 (incl SourceTV) | 10 |
| First live tick | 4238 (round_freeze_end after begin_new_match) | 2543 (RoundStart after AnnouncementMatchStarted) | 4238 |
| Tick 0 positions | populated (back-filled) | empty (no game state yet) | populated (matches Python) |

Python and Rust agree exactly — same parser core, same logic. Go's
`demoinfocs-golang` is an independent reimplementation and surfaces
different event concepts.

## Output format

Matches the Python and Go playgrounds (`== Header ==`, `== Players ==`,
`== Tick 0 (raw first tick) ==`, `== First live-round tick ==`).
