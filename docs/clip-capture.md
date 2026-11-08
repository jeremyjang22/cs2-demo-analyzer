# Clip capture

Record the moments the analytics already find (opening kills, multikills,
clutches, whole rounds) as video from one player's point of view, without
anyone scrubbing a demo by hand.

```
out/<demo>/  (round-collector)  or  data/<demo>.dem
   │  clip-list (Go, any machine)
   ▼
clips.json                 every clip: demo, round, start/end tick, player
csdm/<demo>.json           the same clips as CS Demo Manager video jobs
   │  copied to the recording laptop
   │  scripts/clips/record-clips.ps1 (Windows, CS2 installed)
   ▼
CS Demo Manager → CS2 + HLAE → FFmpeg → one video per clip (or one reel per demo)
```

The one rule still holds: Go writes files and nothing calls back. `clip-list`
does not know a laptop exists; the laptop never runs the parser.

## Why CS Demo Manager does the recording

Recording a clip means: launch CS2 through HLAE, load the demo, jump to a tick,
lock the camera to one player, start recording, stop at another tick, encode.
The fragile part is "at a tick". CS2's console has no reliable way to run a
command at a given demo tick, and recent CS2 updates have made `spec_player`
and `demo_gototick` on the same tick unreliable. A script that types into the
console over `-netconport` can only guess at timing.

[CS Demo Manager](https://cs-demo-manager.com) (free, open source) already
solves this with its own CS2 plugin that runs each command at the exact tick,
and it drives HLAE and FFmpeg for you. Its CLI takes a JSON list of sequences
(`csdm video --config-file`), which is what `clip-list -csdm` writes. So we
pick the moments; it does the camera work.

## 1. Make the clip list (any machine)

```sh
cd packages

# Desmond's openings, 3k+ rounds and won clutches across every parsed demo
go run ./clip-list -player Desmond -csdm ../out/_clips/csdm -out ../out/_clips/clips.json ../out

# straight from recordings, every kind, one reel per demo
go run ./clip-list -player 76561198000000001 -kinds opening,multikill,clutch,round \
    -reel -csdm ../out/_clips/csdm -demo-dir 'D:\cs2\demos' ../data/scrim1.dem
```

| Flag | Default | Meaning |
|---|---|---|
| `-player` | everyone | comma list of steamid64s or in-game names, resolved like `demo-analytics -team` |
| `-kinds` | `opening,multikill,clutch` | any of `opening`, `multikill`, `clutch`, `round` |
| `-min-multi` | 3 | kills in one round that count as a multikill |
| `-all-clutches` | off | also record clutches that were lost |
| `-before` / `-after` | 3 / 2 | seconds of lead-in and tail around each moment |
| `-no-merge` | off | by default an opening that starts a 4k, or a 3k that is also a clutch, is one clip |
| `-csdm <dir>` | none | also write one CS Demo Manager job per demo |
| `-demo-dir` | `C:\demos` | where the `.dem` files are **on the laptop** |
| `-video-dir` | CSDM setting | where the laptop writes videos, one folder per demo |
| `-reel` | off | join each demo's clips into a single video |

What each kind covers:

- **opening**: the round's first kill, when the followed player got it.
- **multikill**: first to last kill of a round with `-min-multi` or more kills.
- **clutch**: from the moment the player is last alive to the end of the
  round, so a defuse or a hide is in it. Won clutches only unless `-all-clutches`.
- **round**: freeze end to round end, for every round the player played.

Every clip stays inside its own round. `clips.json` carries an `id`, a
`label` ("1v2 clutch, 2 kill(s), won"), the event ticks, and `sequence`, the
number CS Demo Manager names that clip's output by (`N-sequence`).

## 2. Set up the laptop (once)

1. Install CS2 through Steam and sign in.
2. Install [CS Demo Manager](https://cs-demo-manager.com/docs/installation)
   and go through its first start, including the PostgreSQL database it asks
   for. It only reads demos it has analysed, which is why the script runs
   `csdm analyze` first.
3. In CS Demo Manager, **Settings → Video**: install HLAE and FFmpeg with the
   buttons there (keep both updated; CS2 updates break old HLAE builds), pick
   HLAE as the recording system and FFmpeg as the encoder, and set resolution
   and framerate. The script never overrides these.
4. Make sure the `csdm` command works in PowerShell (`csdm help`). If it is not
   on PATH, pass `-Csdm "C:\...\csdm.exe"` to the script.
5. Record one sequence from the app by hand to check the whole chain works
   before automating it.

HLAE starts CS2 with `-insecure`, so VAC is off for that session and you
cannot join secure servers until CS2 is restarted normally. Use the laptop
only for recording while it runs.

## 3. Record

Copy the `.dem` files into the `-demo-dir` folder and the `csdm/` folder
anywhere on the laptop, then in PowerShell:

```powershell
.\scripts\clips\record-clips.ps1 -ConfigDir C:\clips\csdm
.\scripts\clips\record-clips.ps1 -ConfigDir C:\clips\csdm -Only scrim1 -SkipAnalyze
```

It records one demo at a time and prints a table of what worked. Leave the
laptop alone while it runs: CS2 takes over the screen.

Rough cost: recording runs at or below real time, so a clip list's printed
"seconds of footage" is a fair lower bound on how long the laptop is busy,
plus about a minute per demo for CS2 to start and load.

## Not done yet

- Running it on the laptop end to end. The JSON matches CS Demo Manager's
  `VideoCommandConfig` as of its November 2026 source, but has not been fed
  to a real `csdm video` yet.
- Triggering it from the web app's Watch links, or a queue the laptop polls.
