# Polyglot Playground — First-Run Cross-Library Findings

Date: 2026-05-27
Demo: `data/mega_ot_mirage.dem` (FACEIT Mirage overtime, 700MB)

## Headline result

All three parsers agree on the map name, the 10-player real roster, and
the steam IDs. They disagree on three things, all of which are
explainable by library design choices rather than parser bugs.

## Detailed comparison

| Field | Python (demoparser2) | Go (demoinfocs v5) | Rust (LaihoE/demoparser) |
|---|---|---|---|
| Map | de_mirage | de_mirage | de_mirage |
| Player count | 10 | 11 | 10 |
| Includes SourceTV? | no | yes | no |
| Tick 0 positions populated? | yes | no | yes |
| First live-round tick | 4238 | 2543 | 4238 |
| Live-round detection method | begin_new_match → round_freeze_end | AnnouncementMatchStarted → RoundStart | begin_new_match → round_freeze_end |

## Divergence 1: SourceTV in the player list

Go's `Participants().All()` exposes the SourceTV observer (SteamID 0).
demoparser2 (used by both Python and Rust) does not. This is library
design, not parser-state inconsistency.

When to care: cross-library player counts will be off by 1. Filter on
`SteamID64 != 0` to normalize.

## Divergence 2: Tick 0 positions

Python and Rust return populated player positions at tick 0; Go's
streaming parser hasn't built game-state entities yet at frame 0, so
positions are empty. Both are correct for what they are — demoparser2
does a bulk parse and can backfill; demoinfocs streams and must wait.

Worth noting: the tick-0 positions that Python and Rust report (the
pre-match spawn positions) swap sides exactly when you compare them to
the live-tick positions. CT-side coordinates at tick 0 become T-side at
tick 4238, and vice versa — the teams switched sides between warmup and
the first live round, as expected for a FACEIT match starting on the CT
side.

When to care: any "frame-0 state" comparison between Go and the other
two will look like a bug but isn't. The first useful tick in Go is
roughly the first FrameDone where game state is populated; check with
`len(Participants().All()) > 0`.

## Divergence 3: First live-round tick

| Lib | Tick | Anchor event |
|---|---|---|
| Python | 4238 | first `round_freeze_end` after `begin_new_match` at tick 2543 |
| Rust | 4238 | (same as Python — same parser core) |
| Go | 2543 | first non-warmup `RoundStart` after `AnnouncementMatchStarted` |

`round_freeze_end` and `RoundStart` are different points in the same
round lifecycle. The ~1695-tick gap (~26.5 seconds at 64 tick) is freeze
time — the period between round_start and round_freeze_end where players
can buy equipment but cannot shoot.

When to care: if you're computing round-relative times, pick which event
you treat as t=0 and stay consistent. For "first frame players can
actually engage" use `round_freeze_end` (tick 4238); for "first frame of
the round itself" use `round_start` (tick 2543).

## Divergence 4: Header fields exposed

A minor but notable difference: Python and Rust both surface `Demo type:
valve_demo_2` from the header. Go does not expose this field through its
`Header()` API. In the other direction, Go surfaces `TickRate` and
`Duration` in a "Playback Info" section; Python and Rust do not emit
these by default (demoparser2 has them available but the playground
doesn't print them).

When to care: low stakes — these are metadata fields useful for sanity
checks, not for analysis. Just be aware the Go `Header` struct has a
different shape than the demoparser2 header object.

## What we learned

1. Two of the three playgrounds share a parser core (demoparser2 is used
   by both Python and Rust — the Python package is a PyO3 wrapper around
   the same Rust library used directly in the Rust playground), so they
   agree exactly on player roster, tick-0 positions, and live-round tick.
   demoinfocs-golang is the only fully-independent reimplementation in
   the set.

2. The Go playground is the most useful cross-check — when it disagrees
   with the other two, the disagreement is meaningful (a different
   concept of "live round", SourceTV inclusion). When all three agree
   (map name, 10 real player SteamIDs), the data is trustworthy.

3. For future analyzer work, this means: prototype on Python (fast
   iteration), drop to Rust for hot paths (same data shapes, no
   conversion needed), use Go as a sanity-check oracle for tricky event
   ordering or lifecycle questions.

4. The tick-0 position swap (CT/T sides inverted between warmup and
   match start) is a concrete example of how stale pre-match state can
   silently mislead analysis. Never assume tick-0 positions reflect
   actual in-match team assignments.

## Open questions for later

- Does the SourceTV entry have meaningful tick-level data (positions,
  view angles) that could be used to verify the recording matches the
  match, or is it always empty?
- Does demoparser2's "tick 0" backfill use the first tick where entities
  exist, or does it interpolate from later frames? The positions are
  identical to spawn positions, suggesting they come from the first real
  entity-populated tick, not interpolation.
- Are there other event-name differences between demoparser2 and
  demoinfocs that haven't surfaced yet? (e.g., `player_hurt` vs
  `PlayerHurt`, `weapon_fire` vs `WeaponFire`, etc.)
- The Go playground omits `Demo type` from the header — is `valve_demo_2`
  actually meaningful for analysis (e.g., does it affect parsing paths),
  or purely informational?
