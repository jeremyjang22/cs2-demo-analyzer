package analytics

import (
	"cmp"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// schemaVersion follows the collector's rule: appending a trailing column or a
// new table bumps the minor part, removing or reordering bumps the major.
const schemaVersion = "1.0"

// Table is one output CSV.
type Table struct {
	Name    string
	Columns []string
	Rows    [][]string
}

type tally struct{ n, wins int }

func (t *tally) add(won bool) {
	t.n++
	if won {
		t.wins++
	}
}

// openTally counts opening duels from one side of them.
type openTally struct{ kills, killWins, deaths, deathWins int }

func (a *openTally) plus(b openTally) {
	a.kills += b.kills
	a.killWins += b.killWins
	a.deaths += b.deaths
	a.deathWins += b.deathWins
}

func (a openTally) cells() []string {
	return []string{itoa(a.kills), itoa(a.killWins), itoa(a.deaths), itoa(a.deathWins)}
}

var openColumns = []string{
	"opening_kills", "opening_kill_round_wins", "opening_deaths", "opening_death_round_wins",
}

type teamSide struct {
	team string
	side int
}

type playerSide struct {
	team string
	id   uint64
	side int
}

type teamCount struct {
	team       string
	side       int
	own, enemy int
}

type playerCount struct {
	team       string
	id         uint64
	side       int
	own, enemy int
}

type lineupKey struct {
	team       string
	own, enemy int
	players    string
}

type teamClutch struct {
	team    string
	enemies int
}

type playerClutch struct {
	team    string
	id      uint64
	enemies int
}

func itoa(n int) string { return strconv.Itoa(n) }

func flag(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func idStr(id uint64) string { return strconv.FormatUint(id, 10) }

// rate is wins over n to three places, or empty when there is nothing to
// divide by - an absent rate, not a zero one.
func rate(wins, n int) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(wins)/float64(n), 'f', 3, 64)
}

func (r *Report) name(id uint64) string { return r.Names[id] }

func (r *Report) idList(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = idStr(id)
	}
	return strings.Join(parts, ";")
}

func (r *Report) nameList(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = r.name(id)
	}
	return strings.Join(parts, ";")
}

// sortedKeys returns a map's keys in a fixed order. Every table goes through
// it: Go randomises map iteration, and without it the same demos would write
// different bytes on every run.
func sortedKeys[K comparable, V any](m map[K]V, by func(a, b K) int) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, by)
	return keys
}

// Tables builds every output table from what has been added so far.
func (r *Report) Tables() []Table {
	openTeam := make(map[teamSide]*openTally)
	openPlayer := make(map[playerSide]*openTally)
	for _, o := range r.Openings {
		vs := 1 - o.KillerSide
		for _, t := range []*openTally{
			bump(openTeam, teamSide{o.KillerTeam, o.KillerSide}),
			bump(openPlayer, playerSide{o.KillerTeam, o.Killer, o.KillerSide}),
		} {
			t.kills++
			if o.KillerTeamWon {
				t.killWins++
			}
		}
		for _, t := range []*openTally{
			bump(openTeam, teamSide{o.VictimTeam, vs}),
			bump(openPlayer, playerSide{o.VictimTeam, o.Victim, vs}),
		} {
			t.deaths++
			if !o.KillerTeamWon {
				t.deathWins++
			}
		}
	}

	clutchTeam := make(map[teamClutch]*tally)
	clutchPlayer := make(map[playerClutch]*tally)
	for _, c := range r.Clutches {
		bump(clutchTeam, teamClutch{c.Team, len(c.Enemies)}).add(c.Won)
		bump(clutchPlayer, playerClutch{c.Team, c.Player, len(c.Enemies)}).add(c.Won)
	}

	sitTeam := make(map[teamCount]*tally)
	sitPlayer := make(map[playerCount]*tally)
	sitLineup := make(map[lineupKey]*tally)
	lineupIDs := make(map[lineupKey][]uint64)
	for _, s := range r.Situations {
		own, enemy := len(s.Own), len(s.Enemy)
		bump(sitTeam, teamCount{s.Team, s.Side, own, enemy}).add(s.Won)
		for _, id := range s.Own {
			bump(sitPlayer, playerCount{s.Team, id, s.Side, own, enemy}).add(s.Won)
		}
		lk := lineupKey{s.Team, own, enemy, r.idList(s.Own)}
		bump(sitLineup, lk).add(s.Won)
		lineupIDs[lk] = s.Own
	}

	byTeamCount := func(a, b teamCount) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.side, b.side),
			cmp.Compare(b.own, a.own), cmp.Compare(b.enemy, a.enemy))
	}

	teams := Table{Name: "teams", Columns: append([]string{
		"team", "demos", "rounds", "wins", "t_rounds", "t_wins", "ct_rounds", "ct_wins", "kills", "deaths",
	}, append(slices.Clone(openColumns), "clutch_attempts", "clutch_wins")...)}
	teamOpenings := Table{Name: "team_openings", Columns: append([]string{"team", "side", "rounds"}, openColumns...)}
	for _, team := range sortedKeys(r.teams, cmp.Compare[string]) {
		t := r.teams[team]
		var open openTally
		for s := range 2 {
			o := openTeam[teamSide{team, s}]
			if o == nil {
				o = &openTally{}
			}
			open.plus(*o)
			if t.rounds[s] > 0 {
				teamOpenings.Rows = append(teamOpenings.Rows,
					append([]string{team, sideName(s), itoa(t.rounds[s])}, o.cells()...))
			}
		}
		var clutches tally
		for k, c := range clutchTeam {
			if k.team == team {
				clutches.n += c.n
				clutches.wins += c.wins
			}
		}
		// A team is on exactly one side each round, so its totals are the sum
		// of the two sides.
		row := []string{
			team, itoa(t.demos), itoa(t.rounds[sideT] + t.rounds[sideCT]), itoa(t.wins[sideT] + t.wins[sideCT]),
			itoa(t.rounds[sideT]), itoa(t.wins[sideT]), itoa(t.rounds[sideCT]), itoa(t.wins[sideCT]),
			itoa(t.kills), itoa(t.deaths),
		}
		row = append(row, open.cells()...)
		teams.Rows = append(teams.Rows, append(row, itoa(clutches.n), itoa(clutches.wins)))
	}

	byPlayer := func(a, b playerKey) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(r.players[b].kills, r.players[a].kills),
			cmp.Compare(a.id, b.id))
	}
	players := Table{Name: "players", Columns: append([]string{
		"team", "steamid", "name", "demos", "rounds", "round_wins", "kills", "deaths", "assists", "headshots",
	}, append(slices.Clone(openColumns), "clutch_attempts", "clutch_wins")...)}
	for _, key := range sortedKeys(r.players, byPlayer) {
		p := r.players[key]
		var open openTally
		for s := range 2 {
			if o := openPlayer[playerSide{key.team, key.id, s}]; o != nil {
				open.plus(*o)
			}
		}
		var clutches tally
		for k, c := range clutchPlayer {
			if k.team == key.team && k.id == key.id {
				clutches.n += c.n
				clutches.wins += c.wins
			}
		}
		row := []string{
			key.team, idStr(key.id), r.name(key.id), itoa(p.demos), itoa(p.rounds), itoa(p.roundWins),
			itoa(p.kills), itoa(p.deaths), itoa(p.assists), itoa(p.headshots),
		}
		row = append(row, open.cells()...)
		players.Rows = append(players.Rows, append(row, itoa(clutches.n), itoa(clutches.wins)))
	}

	playerOpenings := Table{Name: "player_openings",
		Columns: append([]string{"team", "steamid", "name", "side"}, openColumns...)}
	for _, k := range sortedKeys(openPlayer, func(a, b playerSide) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.id, b.id), cmp.Compare(a.side, b.side))
	}) {
		playerOpenings.Rows = append(playerOpenings.Rows,
			append([]string{k.team, idStr(k.id), r.name(k.id), sideName(k.side)}, openPlayer[k].cells()...))
	}

	// Weapons: most-used first within each team or player.
	teamWeapon := make(map[[2]string]*weaponTotals)
	for k, w := range r.weapons {
		t := bump(teamWeapon, [2]string{k.team, k.weapon})
		t.kills += w.kills
		t.headshots += w.headshots
	}
	teamWeapons := Table{Name: "team_weapons", Columns: []string{"team", "weapon", "kills", "headshots"}}
	for _, k := range sortedKeys(teamWeapon, func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(teamWeapon[b].kills, teamWeapon[a].kills),
			cmp.Compare(a[1], b[1]))
	}) {
		w := teamWeapon[k]
		teamWeapons.Rows = append(teamWeapons.Rows, []string{k[0], k[1], itoa(w.kills), itoa(w.headshots)})
	}
	playerWeapons := Table{Name: "player_weapons",
		Columns: []string{"team", "steamid", "name", "weapon", "kills", "headshots"}}
	for _, k := range sortedKeys(r.weapons, func(a, b weaponKey) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.id, b.id),
			cmp.Compare(r.weapons[b].kills, r.weapons[a].kills), cmp.Compare(a.weapon, b.weapon))
	}) {
		w := r.weapons[k]
		playerWeapons.Rows = append(playerWeapons.Rows,
			[]string{k.team, idStr(k.id), r.name(k.id), k.weapon, itoa(w.kills), itoa(w.headshots)})
	}

	teamClutches := Table{Name: "team_clutches", Columns: []string{"team", "enemies", "attempts", "wins", "win_rate"}}
	for _, k := range sortedKeys(clutchTeam, func(a, b teamClutch) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.enemies, b.enemies))
	}) {
		c := clutchTeam[k]
		teamClutches.Rows = append(teamClutches.Rows,
			[]string{k.team, itoa(k.enemies), itoa(c.n), itoa(c.wins), rate(c.wins, c.n)})
	}
	playerClutches := Table{Name: "player_clutches",
		Columns: []string{"team", "steamid", "name", "enemies", "attempts", "wins", "win_rate"}}
	for _, k := range sortedKeys(clutchPlayer, func(a, b playerClutch) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.id, b.id), cmp.Compare(a.enemies, b.enemies))
	}) {
		c := clutchPlayer[k]
		playerClutches.Rows = append(playerClutches.Rows,
			[]string{k.team, idStr(k.id), r.name(k.id), itoa(k.enemies), itoa(c.n), itoa(c.wins), rate(c.wins, c.n)})
	}

	teamSituations := Table{Name: "team_situations",
		Columns: []string{"team", "side", "own_alive", "enemy_alive", "rounds", "wins", "win_rate"}}
	for _, k := range sortedKeys(sitTeam, byTeamCount) {
		t := sitTeam[k]
		teamSituations.Rows = append(teamSituations.Rows,
			[]string{k.team, sideName(k.side), itoa(k.own), itoa(k.enemy), itoa(t.n), itoa(t.wins), rate(t.wins, t.n)})
	}
	playerSituations := Table{Name: "player_situations",
		Columns: []string{"team", "steamid", "name", "side", "own_alive", "enemy_alive", "rounds", "wins", "win_rate"}}
	for _, k := range sortedKeys(sitPlayer, func(a, b playerCount) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(a.id, b.id),
			byTeamCount(teamCount{"", a.side, a.own, a.enemy}, teamCount{"", b.side, b.own, b.enemy}))
	}) {
		t := sitPlayer[k]
		playerSituations.Rows = append(playerSituations.Rows, []string{
			k.team, idStr(k.id), r.name(k.id), sideName(k.side), itoa(k.own), itoa(k.enemy),
			itoa(t.n), itoa(t.wins), rate(t.wins, t.n)})
	}
	lineups := Table{Name: "lineup_situations",
		Columns: []string{"team", "own_alive", "enemy_alive", "players", "names", "rounds", "wins", "win_rate"}}
	for _, k := range sortedKeys(sitLineup, func(a, b lineupKey) int {
		return cmp.Or(cmp.Compare(a.team, b.team), cmp.Compare(b.own, a.own), cmp.Compare(b.enemy, a.enemy),
			cmp.Compare(sitLineup[b].n, sitLineup[a].n), cmp.Compare(a.players, b.players))
	}) {
		t := sitLineup[k]
		lineups.Rows = append(lineups.Rows, []string{
			k.team, itoa(k.own), itoa(k.enemy), k.players, r.nameList(lineupIDs[k]),
			itoa(t.n), itoa(t.wins), rate(t.wins, t.n)})
	}

	return []Table{
		teams, players,
		teamWeapons, playerWeapons,
		teamOpenings, playerOpenings,
		teamClutches, playerClutches,
		teamSituations, playerSituations, lineups,
		r.openingFacts(), r.clutchFacts(), r.situationFacts(),
	}
}

func (r *Report) openingFacts() Table {
	t := Table{Name: "openings", Columns: []string{
		"demo", "round", "tick", "killer_steamid", "killer_name", "killer_team", "killer_side",
		"victim_steamid", "victim_name", "victim_team", "weapon", "headshot", "killer_team_won",
	}}
	for _, o := range r.Openings {
		t.Rows = append(t.Rows, []string{
			o.Demo, itoa(int(o.Round)), itoa(int(o.Tick)),
			idStr(o.Killer), r.name(o.Killer), o.KillerTeam, sideName(o.KillerSide),
			idStr(o.Victim), r.name(o.Victim), o.VictimTeam, o.Weapon, flag(o.Headshot), flag(o.KillerTeamWon),
		})
	}
	return t
}

func (r *Report) clutchFacts() Table {
	t := Table{Name: "clutches", Columns: []string{
		"demo", "round", "tick", "steamid", "name", "team", "side", "enemies", "planted",
		"kills", "survived", "won", "enemy_players", "enemy_names",
	}}
	for _, c := range r.Clutches {
		t.Rows = append(t.Rows, []string{
			c.Demo, itoa(int(c.Round)), itoa(int(c.Tick)), idStr(c.Player), r.name(c.Player),
			c.Team, sideName(c.Side), itoa(len(c.Enemies)), flag(c.Planted),
			itoa(c.Kills), flag(c.Survived), flag(c.Won), r.idList(c.Enemies), r.nameList(c.Enemies),
		})
	}
	return t
}

func (r *Report) situationFacts() Table {
	t := Table{Name: "situations", Columns: []string{
		"demo", "round", "tick", "team", "side", "own_alive", "enemy_alive", "planted", "won",
		"own_players", "own_names", "enemy_players", "enemy_names",
	}}
	for _, s := range r.Situations {
		t.Rows = append(t.Rows, []string{
			s.Demo, itoa(int(s.Round)), itoa(int(s.Tick)), s.Team, sideName(s.Side),
			itoa(len(s.Own)), itoa(len(s.Enemy)), flag(s.Planted), flag(s.Won),
			r.idList(s.Own), r.nameList(s.Own), r.idList(s.Enemy), r.nameList(s.Enemy),
		})
	}
	return t
}

type manifest struct {
	SchemaVersion string              `json:"schema_version"`
	Demos         []DemoInfo          `json:"demos"`
	Warnings      []string            `json:"warnings"`
	Rows          map[string]int      `json:"rows"`
	Columns       map[string][]string `json:"columns"`
}

// Write puts every table in dir as <name>.csv, with a manifest.json beside
// them listing the demos that went in and the columns that came out.
func (r *Report) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	m := manifest{
		SchemaVersion: schemaVersion,
		Demos:         r.Demos,
		Warnings:      append([]string{}, r.Warnings...),
		Rows:          make(map[string]int),
		Columns:       make(map[string][]string),
	}
	for _, t := range r.Tables() {
		if err := writeTable(filepath.Join(dir, t.Name+".csv"), t); err != nil {
			return err
		}
		m.Rows[t.Name] = len(t.Rows)
		m.Columns[t.Name] = t.Columns
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func writeTable(path string, t Table) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", filepath.Base(path), cerr)
		}
	}()
	w := csv.NewWriter(f)
	if err := w.Write(t.Columns); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := w.WriteAll(t.Rows); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Summary prints the headline numbers for the given teams - or, with none
// given, for every team in the report.
func (r *Report) Summary(w io.Writer, teams []string) {
	if len(teams) == 0 {
		teams = sortedKeys(r.teams, cmp.Compare[string])
	}
	for _, team := range teams {
		t := r.teams[team]
		if t == nil {
			continue
		}
		rounds := t.rounds[sideT] + t.rounds[sideCT]
		wins := t.wins[sideT] + t.wins[sideCT]
		fmt.Fprintf(w, "\n%s  -  %d demo(s), %d-%d over %d rounds (T %d/%d, CT %d/%d)\n",
			team, t.demos, wins, rounds-wins, rounds,
			t.wins[sideT], t.rounds[sideT], t.wins[sideCT], t.rounds[sideCT])

		var first, firstWon, conceded, concededWon int
		for _, o := range r.Openings {
			switch team {
			case o.KillerTeam:
				first++
				if o.KillerTeamWon {
					firstWon++
				}
			case o.VictimTeam:
				conceded++
				if !o.KillerTeamWon {
					concededWon++
				}
			}
		}
		fmt.Fprintf(w, "  first kill:  got it %d of %d rounds; won %s after getting it, %s after conceding it\n",
			first, first+conceded, fraction(firstWon, first), fraction(concededWon, conceded))

		clutches := make(map[int]*tally)
		for _, c := range r.Clutches {
			if c.Team == team {
				bump(clutches, len(c.Enemies)).add(c.Won)
			}
		}
		fmt.Fprint(w, "  clutches:   ")
		if len(clutches) == 0 {
			fmt.Fprint(w, " none")
		}
		for _, n := range sortedKeys(clutches, cmp.Compare[int]) {
			fmt.Fprintf(w, " 1v%d: %d/%d", n, clutches[n].wins, clutches[n].n)
		}
		fmt.Fprintln(w)

		counts := make(map[[2]int]*tally)
		for _, s := range r.Situations {
			if s.Team == team {
				bump(counts, [2]int{len(s.Own), len(s.Enemy)}).add(s.Won)
			}
		}
		fmt.Fprintln(w, "  rounds won from each player count (own v enemy):")
		line := 0
		for _, k := range sortedKeys(counts, func(a, b [2]int) int {
			return cmp.Or(cmp.Compare(b[0], a[0]), cmp.Compare(b[1], a[1]))
		}) {
			if line%4 == 0 {
				fmt.Fprint(w, "   ")
			}
			fmt.Fprintf(w, " %dv%d %-12s", k[0], k[1], fraction(counts[k].wins, counts[k].n))
			if line++; line%4 == 0 {
				fmt.Fprintln(w)
			}
		}
		if line%4 != 0 {
			fmt.Fprintln(w)
		}
	}
}

// fraction renders "7/12 (58%)", or "0/0" when there is nothing to rate.
func fraction(wins, n int) string {
	if n == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d (%d%%)", wins, n, (wins*100+n/2)/n)
}
