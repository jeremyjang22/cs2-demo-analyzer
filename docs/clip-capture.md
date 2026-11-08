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
| `-recorder` | `CS` | CS Demo Manager's recording system: `CS` (CS2's own recorder) or `HLAE`; empty uses the app's setting |
| `-fps` / `-width` / `-height` | 60 / 1920 / 1080 | video size; 0 uses the app's setting |

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

## Known problems (November 2026)

Found on the first real run, CS Demo Manager 3.20.1, HLAE 2.193.0:

- **CS Demo Manager can't find `psql`.** Add `C:\Program Files\PostgreSQL\<version>\bin`
  to your user PATH and restart the app (and any open terminal).
- **"Invalid FFmpeg executable".** CS Demo Manager only accepts a build that
  reports a release number (`n7.1`, `9.0.2`), not a `master` build
  (`N-12345-g…`). The FFmpeg HLAE's installer ships works.
- **`EPERM … HLAE\ffmpeg\ffmpeg.ini`.** CS Demo Manager writes into HLAE's
  folder, which Program Files does not allow. Copy HLAE to a writable folder
  and point CS Demo Manager there.
- **The run hangs and the demo just plays.** The CS2 update of 2026-11-04 broke
  the CS2 plugin shipped with CS Demo Manager 3.20.1: it loads the sequences but
  never runs a command. The fixed `static/cs2/server.dll` from CS Demo Manager's
  main branch (commit ac9f858) works; drop it over the installed one (back the
  old one up) until a release ships it.
- **With that plugin, HLAE crashes CS2 on the first seek.** CS2's own recorder
  works, hence `-recorder CS` by default. You lose HLAE's killfeed control.
- **The camera leaves the player.** CS2 moves the spectator camera to the
  killer when the watched player dies, and a mouse click cycles players.
  clip-list re-spectates the clip's player every half second; don't touch the
  machine while it records.

## Not done yet

- Recording with HLAE (see above).
- Triggering it from the web app's Watch links, or a queue the laptop polls.
