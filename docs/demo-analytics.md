# demo-analytics output

What `demo-analytics` writes and exactly what each number counts. Written for
whoever is reading the CSVs.

```sh
cd packages

# every parsed demo under out/
go run ./demo-analytics ../out

# a team across its demos; the pool can be larger than five
go run ./demo-analytics -team "us=solitude,Titan4006,beter,76561198311308221" ../out

# straight from recordings, nothing written in between
go run ./demo-analytics -out ../out/_scrims ../data/scrim1.dem ../data/scrim2.dem
```

An input is a round-collector output folder, a folder of those, or a `.dem`
file, in any mix. A parsed folder loads in milliseconds because only the small
tables are read — never `ticks.csv.gz`. A `.dem` costs a full parse (seconds to
a minute) and produces byte-identical output to its parsed folder. Output goes
to `out/_analytics/` unless `-out` says otherwise.

## Seeing one game in the browser

```sh
cd packages
go run ./demo-analytics -web web/public/data ../out
```

`-web` also writes `<dir>/<demo>/analytics.json` — one per demo, beside the
`movement.json` the replay reads. Then `./scripts/dev.sh --web-only` (or
`npm run dev` in `packages/web`) and open a demo: the header has a
**Replay / Analytics** switch, or go straight to `?demo=<name>&view=analytics`.

The page is one game: record, first-kill and clutch conversion, a scoreboard,
the own-v-enemy grid, and the clutch, first-kill and weapon lists. Filters
narrow everything by team, side, before/after the plant, and — by clicking a
player — to the moments that player was in. Clicking a grid cell lists the
rounds behind it. Picking a round from the dropdown above the grid traces that round
across it: the path starts at the top left, moves right when an opponent dies
and down when one of yours does, and the list underneath names who died at
each step and to whom. Every row that names a moment has a **Watch** link that opens
the replay four seconds before it. With a whole team shown, clicking a
weapon opens it to who got those kills. The longer tables page ten rows at a
time, with a rows-per-page choice under each.

That payload is per-game facts rather than tables (`analytics.GamePayload` in
Go, `Analytics` in `packages/web/src/analytics.ts`); the page does the grouping
so the filters can combine. Unnamed teams are `Team 1` / `Team 2`, the same
order the replay uses; a `-team` name replaces the label. `npm run upload-data`
publishes `analytics.json` along with the replay when it exists.

## Teams

A demo does not name its teams in any way that survives a pug. It only says
which **side** each player is on, and sides swap at halftime and in overtime.
So the two teams of a demo are recovered from who plays alongside whom, and
followed through every swap. A substitute joins the team of the side they
first appear on.

Each team is then aggregated under a key:

- **`-team "Name=player,player,..."`** names a team by its player pool. A
  player is a steamid64 or an in-game name (case-insensitive, matched against
  every name seen in the demos given). In each demo, whichever team fields
  strictly more of the pool takes the name. The pool is not a lineup: list
  seven people and any five can turn up, or list one player to follow whatever
  team they were on. The flag repeats, for naming more than one team.
- **Every other team gets `<demo>/T-start` or `<demo>/CT-start`** — the side
  it began the demo on. Unnamed opponents are deliberately *not* merged across
  demos: they are different people each time.

If a pool is split evenly between both teams of a demo, neither is named and a
warning is printed. `manifest.json` records which key each demo's teams got.

## The tables

Aggregates, one row per group, summed over every demo given:

| File | One row per |
|---|---|
| `teams.csv` | team |
| `players.csv` | (team, player) |
| `team_weapons.csv`, `player_weapons.csv` | … and weapon |
| `team_openings.csv`, `player_openings.csv` | … and side |
| `team_clutches.csv`, `player_clutches.csv` | … and number of enemies |
| `team_situations.csv`, `player_situations.csv` | … and side, own count, enemy count |
| `lineup_situations.csv` | (team, own count, enemy count, exactly who was alive) |

Facts, one row per event, each carrying `demo` and `round` so you can slice
them any way the aggregates do not:

| File | One row per |
|---|---|
| `openings.csv` | round's first kill |
| `clutches.csv` | clutch attempt |
| `situations.csv` | (round, team, player count the round passed through) |

Every aggregate is counts, not rates, apart from a trailing `win_rate`
convenience column — so tables from separate runs can be added together.
A player who appears for two different team keys has a row under each.

## What each number means

**Kills** are scoreboard kills: a real killer on the opposite side, in live
play or the seven seconds after the round is won. Team kills, suicides and
world deaths are nobody's kill but are still the victim's **death**. Freezetime
deaths are ignored entirely, and a victim dies at most once per round (the
earliest row wins — see the duplicate-death note in
`round-collector-schema.md`). Checked against `kills.csv` on the reference
demos: totals reconcile exactly.

**Opening kill** is the first enemy kill of the round in live play. A team kill
or a fall that happens first is skipped, not counted as the opening.
`opening_kill_round_wins` is how many of those rounds the killer's team went on
to win; `opening_death_round_wins` is how many the victim's team won anyway.

**Situations** are the player counts a round passes through: the count at
freeze end (usually 5v5), then the count after each death. Each one is recorded
twice, once from each team's point of view, with `won` saying whether that team
won the round. So "how often do we convert a 4v5" is `wins / rounds` on the
`own_alive = 4, enemy_alive = 5` row — and it is the same events as the
opponent's 5v4 row, mirrored.

- A count is reached at most once per round, so `rounds` really is rounds.
- Deaths on the same tick are one transition. An HE that kills two takes a 5v5
  to a 5v3 without ever recording a 5v4.
- Counts with nobody left on one side are not recorded.
- `planted` is whether the bomb was already down when the count was reached.
  It is on the fact rows only; filter `situations.csv` to split a count into
  pre- and post-plant.
- `player_situations.csv` counts a player only for the counts they were
  **alive** in. `lineup_situations.csv` groups by the exact set of players
  alive, which is where "who is in the 2 of our 2v3s" is answered. For who was
  on the *other* side, `situations.csv` lists `enemy_players` per row.

**Clutch** is a player becoming the last one alive on their team with at least
one enemy still up; `enemies` is how many at that moment. It is **won when the
team wins the round**, the usual scoreboard meaning — a T who dies after the
plant and wins on the explosion still clutched. `kills` (enemies the player
killed from then on) and `survived` say how it was won. One attempt per team
per round at most.

## Things that will bite you

**Some rounds are left out of situations and clutches.** If a player started a
round, was not alive at its end, and has no death recorded in between, they
disconnected at a tick nobody observed (or were already dead when freezetime
ended). The alive counts of that round cannot be trusted — its "5v4" may have
been a 4v4 — so it contributes kills and an opening kill but no situations and
no clutches. `manifest.json` counts these per demo as `rounds_uncertain`. None
of the three reference demos has one.

**A round can start short-handed.** A player who is disconnected at freeze end
is simply not in the count, so a team's first situation of the round can be a
4v5. That is why the 5v5 row can show fewer rounds than the team played.

**Rounds with no winner are skipped completely**, kills included. That is the
round a demo cuts off in the middle of, counted as `rounds_skipped`.

**Small samples.** One demo gives a 2v4 a handful of rounds. Read `rounds`
before reading `win_rate`.

## Adding a statistic

`analytics.Report.Add` sees each round's metadata, kills and bomb rows
(`analytics.Round`). If the new number can be computed from those, add it there
and it works for parsed folders and `.dem` files alike. If it needs something
else the collector records — damage, utility, ticks — widen `analytics.Round`,
then fill the new field in both `LoadDir` (from the CSV) and `matchSink` (from
the live round).
