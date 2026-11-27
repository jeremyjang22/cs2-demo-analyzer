# What is inside a CS2 demo

What a `.dem` file actually contains, measured on the demos this project has
parsed. Read this before deciding something "isn't in the demo", and before
deciding something is. `round-collector-schema.md` covers what we write *out*;
this covers what comes *in*.

## The short version

A demo is not a video and not a log of events. It is a **recording of the
network traffic** a spectator received from the game server, tick by tick. To
"play" one, a client feeds those packets back into itself exactly as if it
were watching live. Every parser, including demoinfocs, is a reimplementation
of that client.

So the question "is X in the demo?" really means: *did the server send it to a
spectator?* Positions, health, and weapons are sent, because a spectator's screen
needs them. Bullet impact points, the map geometry and a dropped defuse kit are
not, so they are not in the file.

By bytes, a demo is mostly **player input and entity state**. The game events
that every tutorial starts with (`player_death`, `weapon_fire`) are under 1%
of the file.

## Reference demos

All numbers below come from these three, scanned at the framing level (see
[How these numbers were measured](#how-these-numbers-were-measured)).

| Demo | Map | Rounds | Length | Ticks | Size |
|---|---|---|---|---|---|
| `anubispug.dem` | de_anubis | 19 | 30.3 min | 116,440 | 243 MB |
| `05-11-2026_mirage_44-32-10.dem` | de_mirage | 48 (overtime) | 83.4 min | 320,182 | 699 MB |
| `1-604132bd-….dem.zst` (m0nesy pug) | de_mirage | 21 | 39.0 min | 149,674 | 215 MB zst / 291 MB unpacked |

All three are **FACEIT server-side SourceTV recordings**: the header says
`client_name = "SourceTV Demo"` and `ServerInfo.is_hltv = true`. That is the
kind of demo FACEIT and Valve hand out. A demo you record yourself from the
console with `record` is a different kind of file: it holds only what your
own client received. Nothing in this project has been tested against one.

Tournament demos are SourceTV recordings too, and the same format. The one
that has been through the parser, `faze-vs-spirit-m2-cache` (BLAST Bounty
2026, de_cache, 22 rounds, 327 MB unpacked), is cited below only where it
differs: it is the demo that showed [where the match starts](#where-the-match-starts)
is not the same everywhere.

**The header names the server, which tells a FACEIT pug from a tournament
match**, `server_name` in `DEM_FileHeader`:

| Demo | `server_name` |
|---|---|
| FACEIT pugs | `FACEIT.com register to play here` |
| BLAST tournament | `BLAST.tv Premier CS2 Server` |

Each organiser writes its own string and can change it, so this is a hint,
never a rule. Nothing in this project reads it, and a demo's `kind` stays
declared in `info.json` rather than guessed from it.

Every one runs at **64 ticks per second** (`tick_interval = 0.015625`).

## Size

A demo costs roughly **125–140 KB per second of match**, or about 8 MB a
minute. It grows with duration, not with how much happens: a 48-round
overtime match is about three times the size of a 19-round one because it
lasted nearly three times as long.

**`.dem.zst` saves only about a quarter, not two thirds.** Across all six
m0nesy pug demos the compressed file is 73–74% of the unpacked size. The
reason is that most of a demo is already compressed internally (see below), and
compressing compressed data gains little. The demo-worker's own zstd, keeping
the BLAST demo's original, did no better: 327 MB to 257 MB, 79%.
`collector.OpenDemo` streams the `.zst` without unpacking it to disk, so this
only matters for storage.

## Layout on disk

```
offset 0    "PBDEMS2\0"                  8 bytes, the Source 2 demo magic
offset 8    int32  offset of FileInfo    so a reader can jump to the summary
offset 12   int32  offset of SpawnGroups
offset 16   command, command, command, … until DEM_Stop
```

Each command is three varints and a payload:

```
varint  type       EDemoCommands, with bit 64 set if the payload is snappy-compressed
varint  tick
varint  size
bytes   payload    a protobuf message, snappy-compressed if flagged
```

Nearly every command is compressed (109,132 of 116,484 in Anubis), so a demo
is already snappy-compressed internally.

### The commands, by share of the file

Anubis; the other two demos have the same shape.

| Command | Count | On disk | What it is |
|---|---|---|---|
| `DEM_Packet` | 116,435 | **99.4%** | One per tick. The actual recording; see the next section |
| `DEM_FullPacket` | 31 | 0.55% | A complete snapshot of every entity, about once a minute. Lets a player seek without replaying from the start |
| `DEM_SignonPacket` | 10 | 0.02% | Connection setup: server info, string tables, the event list |
| `DEM_SendTables` | 1 | 40 KB | The schema for every entity class: which properties exist and how each is bit-encoded |
| `DEM_StringTables` | 1 | 24 KB | Initial lookup tables (below) |
| `DEM_ClassInfo` | 1 | 3 KB | Class ID → class name, e.g. `CCSPlayerPawn` |
| `DEM_FileHeader` | 1 | 168 B | Server name, map, build number, `demo_version_name = valve_demo_2` |
| `DEM_FileInfo` | 1 | 13 B | Duration in seconds, ticks and frames. Written last; the header offset points to it |
| `DEM_SyncTick`, `DEM_SpawnGroups`, `DEM_Stop` | 1 each | ~0 | Markers |

The Mirage demo also has 16 small `DEM_Recovery` records. Nothing in this
project reads them.

`FileInfo` has a slot for round start ticks (`game_info.cs.round_start_ticks`),
but it is **empty in every FACEIT demo here**. Round boundaries have to be
worked out from the game state, which is what the collector does.

## Inside a packet

A `DEM_Packet` payload is a bitstream of the server's own network messages,
each one a type ID (variable-width bits), a size and a protobuf body. This is
where the content of the match actually is. Shares are of decompressed
bytes:

| Message | Anubis | Mirage OT | m0nesy pug | What it is |
|---|---|---|---|---|
| `svc_UserCmds` | 57.7% | 72.6% | 59.3% | **Every player's input, every tick.** See below |
| `svc_PacketEntities` | 33.3% | 24.3% | 32.8% | Entity state changes: positions, health, weapons, everything |
| `svc_VoiceData` | 7.0% | 1.7% | 6.1% | **Players' voice chat audio** |
| `net_Tick` | 0.6% | 0.5% | 0.6% | Tick marker, one per packet |
| `GE_Source1LegacyGameEvent` | 0.5% | 0.4% | 0.5% | The game events: deaths, shots, grenades |
| `GE_SosStartSoundEvent` | 0.4% | 0.4% | 0.5% | Sounds played |
| everything else | ~0.5% | ~0.3% | ~0.5% | Bullet effects, particles, decals, chat, scoreboard updates |

The rest of this document goes through these in order of size.

## Player input (`svc_UserCmds`), the biggest thing in the file

Each tick, the server re-broadcasts the input command each player's client
sent it. Anubis has 1,156,312 of them, which is 116,440 ticks × 10 players to
within 1%, across exactly 10 player slots.

Decoded by protobuf field number against the `CSGOUserCmdPB` definition
demoinfocs ships (`msg/proto/cs_usercmd.proto`), the commands carry:

| Field | Present in | Meaning |
|---|---|---|
| `client_tick` | 94% | the tick the client thought it was |
| `random_seed` | 98% | spread seed for that command |
| `input_history` | ~99% | view angles with render-tick and interpolation fractions, which is the sub-tick timing data |
| `viewangles` | 46% | pitch / yaw |
| `prediction_offset_ticks_x256` | 35% | |
| `mousedx` / `mousedy` | 17% / 9% | raw mouse movement |
| `buttons_pb` | 7% | button state |
| `subtick_moves` | 2% | per-key press timing within a tick |

The low percentages for buttons and mouse are expected. These look
**delta-encoded**: a field is sent only when it changes, so `buttons_pb`
appears when a key goes down or up, not every tick.

**This is inferred, not verified field by field.** The outer wrapper
(`CMsgServerUserCmd`: player slot, command number, the tick it executed on)
decodes cleanly. The payload, however, sits in **field 6 of that wrapper, which
the protobuf bundled with demoinfocs v5.2.0 does not declare**. The declared
`data` field is empty in 99.7% of commands. As a result:

- demoinfocs does not surface user commands at all, and **nothing in this
  project reads them**. The `buttons` column in `ticks.csv.gz` comes from the
  player pawn's `m_nButtonDownMaskPrev` property instead, which is why it lags
  position by one tick (trap 2 in `round-collector-schema.md`).
- The field mapping in the table above matches the shape of `CSGOUserCmdPB`,
  but Valve can renumber fields between builds. Before building anything on
  these values, confirm them against a parser that tracks Valve's current
  protobufs (demoparser2 decodes user commands).

This is the largest untapped data in the file. Sub-tick key timing, raw mouse
deltas and the true button state are exactly what counterstrafe and aim
analysis would want.

## Entity state (`svc_PacketEntities`), where nearly everything we use comes from

The world is a set of networked **entities**, each an instance of a class
with typed properties. The demos here declare **211 classes**
(222 in the newer m0nesy build). The ones this project reads:

| Class | Holds |
|---|---|
| `CCSPlayerPawn` | the body in the world: position, view angles, health, armor, stance, helmet and kit (`m_pItemServices`), buttons and view punch (movement and camera services), flash duration |
| `CCSPlayerController` | the person: name, steamid, team, money, connection |
| `CCSGameRulesProxy` | round phase, freeze period, win reason, timeouts |
| `CAK47`, `CWeaponAWP`, `CWeaponM4A1`, … | one entity per gun in the world, with ammo and `m_fAccuracyPenalty` |
| `C*GrenadeProjectile`, `CInferno` | grenades in flight and fires on the ground |
| `CC4`, `CPlantedC4` | the bomb, carried or dropped, then planted |

Most of the 211 are map furniture: lights, fog, doors, sound volumes, props
and a fish (`CFish`).

**Packets carry deltas, not snapshots.** Each tick's `svc_PacketEntities`
lists only the properties that changed. Anubis has 3.3 million entity updates
over 116,440 ticks, about 29 entities changing per tick. A parser has to start
from the beginning, or from a `DEM_FullPacket`, and apply every delta in
order. That is why parsing is a forward-only stream, and why `OpenDemo` can
decompress on the fly.

**Entities report existence, not events.** This is the source of the
CLAUDE.md warning about lifetime versus motion: a grenade entity is created on
throw and destroyed later, and everything in between is position updates that
nobody announces. Anything about *motion* has to be polled from entity state
every frame.

**Some things are never entities.** A dropped defuse kit, kevlar and a helmet
are only properties on a player, so they stop existing in the demo when their
owner dies. `kits.csv` is reconstructed for that reason; its section in
`round-collector-schema.md` has the evidence.

### String tables

Small lookup tables the entity data refers to by index: `userinfo` (64 slots:
player names and steamids), `instancebaseline` (default property values per
class, so deltas can be small), `EntityNames`, `EffectDispatch`, `lightstyles`,
and a few that are always empty.

## Game events, under 1% of the file

The Source 1 style game events: named, with a fixed list of keys. Each demo
**declares 271 event types**, but only **about 50 ever fire**. Anubis, 19 rounds:

| Event | Count | | Event | Count |
|---|---|---|---|---|
| `item_equip` | 5,548 | | `player_blind` | 115 |
| `show_survival_respawn_status` | 3,240 | | `smokegrenade_detonate` | 90 |
| `weapon_fire` | 3,133 | | `flashbang_detonate` | 85 |
| `item_pickup` | 1,234 | | `hegrenade_detonate` | 76 |
| `weapon_reload` | 907 | | `inferno_startburn` | 66 |
| `player_footstep` | 807 | | `bomb_pickup` / `bomb_dropped` | 48 / 35 |
| `player_hurt` | 639 | | `round_freeze_end` | 21 |
| `player_spawn` | 214 | | `bomb_planted` | 11 |
| `player_death` | 144 | | `bomb_defused` | 1 |

Things to know:

- **The two biggest are noise.** `item_equip` fires on every weapon switch.
  `show_survival_respawn_status` belongs to a survival mode, yet fires
  thousands of times in a competitive match. Nothing here uses it.
- **`round_start` and `round_end` never fire.** They are declared and never
  sent, in all three demos. demoinfocs synthesizes its `RoundStart` and
  `RoundEnd` from game-rules entity properties (`m_bFreezePeriod`,
  `m_eRoundWinReason`) instead. `bomb_exploded` is similarly unreliable: it
  fired 3 times in Mirage but never in Anubis, even though a bomb did explode
  there (`bomb.csv` has the row). demoinfocs derives `BombExplode` from entity
  state too.
- **`player_death` counts are not kill counts.** They include warmup and
  disconnects. `round-collector-schema.md` covers the filtering.
- `player_footstep` is sparse (807 in a whole match), so it is not a record of
  every step. Use positions.

The collector registers demoinfocs handlers for kills, damage, shots,
grenades and the bomb, and the two below that start the match. Those are the
only parts of this section it uses.

### Where the match starts

A demo records more than the match: warmup, and often a stray round before
it. demoinfocs also synthesizes a `RoundStart` at tick 0. The collector
throws everything away until the match has started, and two game events say
when that is:

| Event | demoinfocs | When it fires |
|---|---|---|
| `begin_new_match` | `events.MatchStart` | just before round 1's `RoundStart` |
| `round_announce_match_start` | `events.AnnouncementMatchStarted` | wherever the server sends it; see below |

The announcement does not fire in the same place everywhere. In the BLAST
demo it comes at the **end of round 1's freeze time**:

| Tick | BLAST demo |
|---|---|
| 0 | `RoundStart` (synthesized, not a round) |
| 375 | `begin_new_match`, then round 1's `RoundStart` |
| 4,300 | freeze time ends, then `round_announce_match_start` |
| 7,151 | round 1 ends |

In the FACEIT pugs (nukepug, anubispug) it fires at tick 1,280, during the
stray round recorded before the warmup, long before round 1.

The collector used to start the match at the announcement alone. FACEIT
demos came out right because of where theirs happens to fall, but the BLAST
demo lost round 1, dropped as if it were warmup, and published 21 rounds and
the wrong score. It now starts the match at whichever of the two comes first
(`register` in `collector.go`). The pugs come out byte-identical either way.

A match restarted after a knife round, which could fire `begin_new_match`
twice, has not been seen in a demo here.

## Voice and chat, which are in the file too

**`svc_VoiceData` is the players' voice chat**: 6–7% of a pug demo and 1.7% of
the Mirage match. Chat messages are there
too, as `UM_SayText2`. Neither is extracted by this project.

This matters when **sharing demo files**: a `.dem` handed to someone else
carries the voices and chat of all ten players. The payloads we publish
(`movement.json`) contain neither. The originals the demo-worker keeps in
storage (`raw/`, `docs/demo-worker.md`) do, which is one reason that prefix is
never served.

## Smaller things

- **`GE_FireBulletsId`**: one per shot. Its definition carries the shooter's
  origin and angles, weapon, spread seed, inaccuracy, spread and recoil index,
  plus sub-tick attack timing in an `Extra` block. It still has **no impact
  point**. A demo never traces bullets, which is why `shots.csv` has no
  endpoint column. Nothing here reads this message; `shots.csv` comes from the
  `weapon_fire` event.
- **`GE_SosStartSoundEvent`**: sounds as they play (gunshots, footsteps,
  utility). This is a better footstep record than `player_footstep`, though
  nothing here uses it.
- **`TE_*` and `UM_ParticleManager`**: visual effects: blood, impacts,
  explosions.
- **`CS_UM_EndOfMatchAllPlayersData`, `CS_UM_PlayerStatsUpdate`**: end-of-match
  scoreboard data.
- **The map itself is not in the file.** The demo names the map
  (`de_mirage`) and the game loads it from disk. That is why the radar
  images are committed separately in `assets/radar/` and never come from a
  demo.

## What the collector takes from all this

| Demo source | Becomes |
|---|---|
| `CCSPlayerPawn` / `CCSPlayerController` state, polled every tick | `ticks.csv.gz`, `round_players.csv`, `players.csv` |
| `CCSGameRulesProxy` state, via demoinfocs' round events, from the match start on | `rounds.csv`, the `phase` column |
| `player_death`, `player_hurt`, `weapon_fire` events | `kills.csv`, `damage.csv`, `shots.csv` |
| grenade events + projectile and inferno entities, polled | `utility.csv`, `trajectories.csv` |
| `CC4` / `CPlantedC4` entities + bomb events | `bomb.csv` |
| defuse-kit property transitions (**inferred**) | `kits.csv` |
| `svc_UserCmds`, voice, chat, sounds, effects | nothing yet |

So between two thirds and three quarters of the bytes in a demo (user
commands and voice: 65% in Anubis, 74% in Mirage, of decompressed packet
bytes) never reach any output. That is a large part of why 670 MB becomes
19 MB.

## How these numbers were measured

With a throwaway Go program that walks the raw framing described above,
without demoinfocs's game-state layer. It reads each command, snappy-decodes
it, splits `DEM_Packet` payloads into their inner messages, and tallies counts
and bytes per type. Message names come from the enum name maps in
demoinfocs' `msg` package. Game event names come from the
`GE_Source1LegacyGameEventList` message each demo carries. User commands were
decoded generically with `protowire` by field number.

A full scan takes 3–10 seconds per demo, against minutes for a real parse,
because it never builds entities. To re-measure on a new build, rebuild the
same thing: the layout section above is the whole spec.
