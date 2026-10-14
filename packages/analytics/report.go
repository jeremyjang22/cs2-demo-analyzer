package analytics

import (
	"sort"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// Opening is a round's first kill of an enemy.
type Opening struct {
	Demo  string
	Round int32
	Tick  int32

	Killer, Victim         uint64
	KillerTeam, VictimTeam string
	KillerSide             int
	Weapon                 string
	Headshot               bool
	KillerTeamWon          bool
}

// Situation is one team's view of a player count the round passed through:
// "we were 4 against their 3, and went on to win". Every count a round reaches
// produces two of these, one per team, so a conversion rate is just wins over
// rows for whichever team you filter to.
type Situation struct {
	Demo  string
	Round int32
	Tick  int32 // when this count was reached; the freeze-end tick for the opening count

	Team       string
	Side       int
	Own, Enemy []uint64 // who was alive, sorted
	Planted    bool     // the bomb was already down when this count was reached
	Won        bool
}

// Clutch is a player left as the last one alive on their team with at least
// one enemy still up. It is won when their TEAM wins the round, which is the
// usual scoreboard meaning: a T who dies after the plant and wins on the
// explosion still clutched. Survived and Kills say how.
type Clutch struct {
	Demo  string
	Round int32
	Tick  int32

	Player   uint64
	Team     string
	Side     int
	Enemies  []uint64 // alive when the clutch began; len is the N in 1vN
	Planted  bool
	Kills    int // enemies the player killed from that point on
	Survived bool
	Won      bool
}

// Death is one death as the scoreboard reads it, with the credit already
// decided: Killer is 0 unless an enemy got the kill, and Assister is 0 unless
// an enemy of the victim helped. It is what the totals are summed from, kept
// so a consumer can re-sum them by side or by moment.
type Death struct {
	Round int32
	Tick  int32

	Victim, Killer, Assister uint64
	VictimTeam               string
	Weapon                   string
	Headshot                 bool
	// Postround is a death after the round was already won: on the
	// scoreboard, but not part of how the round went.
	Postround bool
}

// DemoInfo records what one demo contributed, for the manifest.
type DemoInfo struct {
	Name   string    `json:"name"`
	Map    string    `json:"map"`
	Teams  [2]string `json:"teams"`
	Rounds int       `json:"rounds"`
	// RoundsSkipped never produced a winner - the demo cut off mid-round.
	RoundsSkipped int `json:"rounds_skipped"`
	// RoundsUncertain count toward kills and openings but not toward
	// situations or clutches. See rosterUncertain.
	RoundsUncertain int `json:"rounds_uncertain"`
}

type playerKey struct {
	team string
	id   uint64
}

type weaponKey struct {
	team   string
	id     uint64
	weapon string
}

type playerTotals struct {
	demos, rounds, roundWins          int
	kills, deaths, assists, headshots int
}

type teamTotals struct {
	demos         int
	rounds, wins  [2]int // by side
	kills, deaths int
}

type weaponTotals struct{ kills, headshots int }

// Report accumulates matches. Add each one, then Write or Summary.
type Report struct {
	Demos      []DemoInfo
	Names      map[uint64]string
	Openings   []Opening
	Situations []Situation
	Clutches   []Clutch
	// Deaths holds every match added, in order; it carries no demo name, so
	// it is only meaningful to read on a single-match report.
	Deaths   []Death
	Warnings []string

	players map[playerKey]*playerTotals
	teams   map[string]*teamTotals
	weapons map[weaponKey]*weaponTotals
}

func NewReport() *Report {
	return &Report{
		Names:   make(map[uint64]string),
		players: make(map[playerKey]*playerTotals),
		teams:   make(map[string]*teamTotals),
		weapons: make(map[weaponKey]*weaponTotals),
	}
}

func bump[K comparable, V any](m map[K]*V, k K) *V {
	v := m[k]
	if v == nil {
		v = new(V)
		m[k] = v
	}
	return v
}

// Add folds one match into the report.
func (r *Report) Add(m *Match, defs []TeamDef) {
	split := splitTeams(m)
	keys, warnings := nameTeams(m, split, defs)
	r.Warnings = append(r.Warnings, warnings...)
	for id, name := range m.Names {
		r.Names[id] = name
	}

	info := DemoInfo{Name: m.Name, Map: m.Map, Teams: keys}
	appeared := make(map[playerKey]bool, 12)

	for i := range m.Rounds {
		rd := &m.Rounds[i]
		winner, decided := sideIndex(rd.Meta.Winner)
		if !decided {
			info.RoundsSkipped++
			continue
		}
		info.Rounds++
		teamOf := func(side int) string { return keys[split.teamOn(i, side)] }

		for s := range 2 {
			t := bump(r.teams, teamOf(s))
			t.rounds[s]++
			if s == winner {
				t.wins[s]++
			}
		}
		sideOf := make(map[uint64]int, len(rd.Meta.Players))
		for _, p := range rd.Meta.Players {
			s, ok := sideIndex(p.Team)
			if !ok || p.JoinedLate {
				continue
			}
			sideOf[p.SteamID] = s
			key := playerKey{teamOf(s), p.SteamID}
			appeared[key] = true
			pt := bump(r.players, key)
			pt.rounds++
			if s == winner {
				pt.roundWins++
			}
		}

		deaths := realDeaths(rd.Kills)
		r.addKills(deaths, sideOf, teamOf)

		live := deaths[:0:0]
		for _, k := range deaths {
			if k.Phase == collector.PhaseLive {
				live = append(live, k)
			}
		}
		r.addOpening(m.Name, rd, live, teamOf, winner)
		if rosterUncertain(rd, live) {
			info.RoundsUncertain++
			continue
		}
		r.walkRound(m.Name, rd, live, teamOf, winner)
	}

	for key := range appeared {
		r.players[key].demos++
	}
	for _, key := range keys {
		bump(r.teams, key).demos++
	}
	r.Demos = append(r.Demos, info)
}

// realDeaths is the round's deaths outside freezetime, one per victim, in tick
// order.
//
// Freezetime rows are disconnects and suicides, not kills. One per victim
// because demoinfocs can fire a second Kill for an already-dead player at the
// RoundEnd tick; keeping the earliest is the dedupe kills.csv's docs prescribe.
// Postround deaths stay in - they count on the scoreboard - and callers that
// describe the contested round filter to PhaseLive themselves.
func realDeaths(kills []collector.Kill) []collector.Kill {
	out := make([]collector.Kill, 0, len(kills))
	for _, k := range kills {
		if k.Phase != collector.PhaseFreeze && k.VictimSteamID != 0 {
			out = append(out, k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	seen := make(map[uint64]bool, len(out))
	n := 0
	for _, k := range out {
		if seen[k.VictimSteamID] {
			continue
		}
		seen[k.VictimSteamID] = true
		out[n] = k
		n++
	}
	return out[:n]
}

// enemyKill reports whether k is a kill the scoreboard would credit: a real
// killer on the opposite side. That excludes world deaths, suicides and team
// kills, all of which still count as the victim's death.
func enemyKill(k collector.Kill) bool {
	ks, kok := sideIndex(k.KillerTeam)
	vs, vok := sideIndex(k.VictimTeam)
	return k.KillerSteamID != 0 && kok && vok && ks != vs
}

func (r *Report) addKills(deaths []collector.Kill, sideOf map[uint64]int, teamOf func(int) string) {
	for _, k := range deaths {
		vs, ok := sideIndex(k.VictimTeam)
		if !ok {
			continue
		}
		victimTeam := teamOf(vs)
		fact := Death{
			Round: k.Round, Tick: k.Tick, Victim: k.VictimSteamID, VictimTeam: victimTeam,
			Weapon: k.Weapon, Headshot: k.Headshot, Postround: k.Phase == collector.PhasePostRound,
		}
		bump(r.players, playerKey{victimTeam, k.VictimSteamID}).deaths++
		bump(r.teams, victimTeam).deaths++

		if enemyKill(k) {
			killerTeam := teamOf(1 - vs)
			pt := bump(r.players, playerKey{killerTeam, k.KillerSteamID})
			wt := bump(r.weapons, weaponKey{killerTeam, k.KillerSteamID, k.Weapon})
			pt.kills++
			wt.kills++
			if k.Headshot {
				pt.headshots++
				wt.headshots++
			}
			bump(r.teams, killerTeam).kills++
			fact.Killer = k.KillerSteamID
		}
		// The kill row carries no assister team, so it comes from the roster.
		// Helping to kill a teammate is not an assist.
		if as, ok := sideOf[k.AssisterSteamID]; ok && as != vs {
			bump(r.players, playerKey{teamOf(as), k.AssisterSteamID}).assists++
			fact.Assister = k.AssisterSteamID
		}
		r.Deaths = append(r.Deaths, fact)
	}
}

func (r *Report) addOpening(demo string, rd *Round, live []collector.Kill, teamOf func(int) string, winner int) {
	for _, k := range live {
		if !enemyKill(k) {
			continue
		}
		ks, _ := sideIndex(k.KillerTeam)
		r.Openings = append(r.Openings, Opening{
			Demo: demo, Round: rd.Meta.Number, Tick: k.Tick,
			Killer: k.KillerSteamID, Victim: k.VictimSteamID,
			KillerTeam: teamOf(ks), VictimTeam: teamOf(1 - ks),
			KillerSide: ks, Weapon: k.Weapon, Headshot: k.Headshot,
			KillerTeamWon: ks == winner,
		})
		return
	}
}

// rosterUncertain reports a round whose alive counts cannot be trusted: a
// player who started it, was not alive at the end, and has no death recorded
// in between. That is a mid-round disconnect that produced no kill event (or a
// player already dead when freezetime ended), and there is no telling at which
// tick the team went a man down - so a "5v4" taken from this round might
// really have been a 4v4. Such rounds are left out of situations and clutches
// rather than guessed at.
func rosterUncertain(rd *Round, live []collector.Kill) bool {
	died := make(map[uint64]bool, len(live))
	for _, k := range live {
		died[k.VictimSteamID] = true
	}
	for _, p := range rd.Meta.Players {
		if _, ok := sideIndex(p.Team); !ok || p.JoinedLate {
			continue
		}
		if !p.Survived && !died[p.SteamID] {
			return true
		}
	}
	return false
}

// plantTick is when the bomb went down, or 0 if it never did.
func plantTick(bomb []collector.BombSample) int32 {
	for _, b := range bomb {
		if b.State == collector.BombPlanted {
			return b.Tick
		}
	}
	return 0
}

// walkRound replays the round's deaths and records every player count it
// passes through, plus any clutch that opens along the way.
func (r *Report) walkRound(demo string, rd *Round, live []collector.Kill, teamOf func(int) string, winner int) {
	alive := [2]map[uint64]bool{{}, {}}
	survived := make(map[uint64]bool, len(rd.Meta.Players))
	for _, p := range rd.Meta.Players {
		if s, ok := sideIndex(p.Team); ok && !p.JoinedLate {
			alive[s][p.SteamID] = true
			survived[p.SteamID] = p.Survived
		}
	}

	planted := plantTick(rd.Bomb)
	clutch := [2]int{-1, -1} // index into r.Clutches, per side
	last := [2]int{-1, -1}

	snapshot := func(tick int32) {
		n := [2]int{len(alive[sideT]), len(alive[sideCT])}
		// A count with nobody on one side is the round ending, not a
		// situation. An unchanged count is a death that was not in the
		// roster, and would otherwise record the same state twice.
		if n[0] == 0 || n[1] == 0 || n == last {
			return
		}
		last = n
		ids := [2][]uint64{sortedIDs(alive[sideT]), sortedIDs(alive[sideCT])}
		down := planted > 0 && planted <= tick
		for s := range 2 {
			r.Situations = append(r.Situations, Situation{
				Demo: demo, Round: rd.Meta.Number, Tick: tick,
				Team: teamOf(s), Side: s, Own: ids[s], Enemy: ids[1-s],
				Planted: down, Won: s == winner,
			})
			if n[s] == 1 && clutch[s] < 0 {
				clutch[s] = len(r.Clutches)
				r.Clutches = append(r.Clutches, Clutch{
					Demo: demo, Round: rd.Meta.Number, Tick: tick,
					Player: ids[s][0], Team: teamOf(s), Side: s, Enemies: ids[1-s],
					Planted: down, Survived: survived[ids[s][0]], Won: s == winner,
				})
			}
		}
	}

	snapshot(rd.Meta.FreezeEndTick)
	for i := 0; i < len(live); {
		// Deaths on one tick - an HE taking two, a trade inside a frame - are
		// one transition. Splitting them would invent a count nobody played.
		j := i
		for ; j < len(live) && live[j].Tick == live[i].Tick; j++ {
			k := live[j]
			if enemyKill(k) {
				ks, _ := sideIndex(k.KillerTeam)
				if c := clutch[ks]; c >= 0 && r.Clutches[c].Player == k.KillerSteamID {
					r.Clutches[c].Kills++
				}
			}
			delete(alive[sideT], k.VictimSteamID)
			delete(alive[sideCT], k.VictimSteamID)
		}
		snapshot(live[i].Tick)
		i = j
	}
}
