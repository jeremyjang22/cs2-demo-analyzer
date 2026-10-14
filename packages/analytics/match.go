// Package analytics turns the collector's round facts into match statistics:
// weapon kills, opening duels, clutches, and how often each X-vs-Y player
// count converts into a round win. It aggregates by player and by team, across
// one demo or many.
//
// It needs only a round's metadata, kills and bomb rows - never the tick
// stream - so a Match can be built either from a parsed output folder
// (LoadDir) or straight from a .dem (FromDemo), and the two produce the same
// report.
package analytics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// Match is one demo's worth of input.
type Match struct {
	// Name identifies the demo in every fact row. It is the demo's basename,
	// so it must be unique within one report.
	Name string
	Map  string
	// TickRate converts ticks to seconds for the web payload. Zero when the
	// demo never reported one; nothing else here depends on it.
	TickRate float64
	Names    map[uint64]string
	Rounds   []Round
}

// Round is the slice of collector.Round the analytics read.
type Round struct {
	Meta  collector.RoundMeta
	Kills []collector.Kill
	Bomb  []collector.BombSample
}

// Sides are indexed 0 = T, 1 = CT throughout, so "the other side" is 1-s.
const (
	sideT  = 0
	sideCT = 1
)

func sideIndex(t common.Team) (int, bool) {
	switch t {
	case common.TeamTerrorists:
		return sideT, true
	case common.TeamCounterTerrorists:
		return sideCT, true
	}
	return 0, false
}

func sideName(s int) string {
	if s == sideT {
		return "T"
	}
	return "CT"
}

// teamSplit says which of a match's two teams was on which side, round by
// round. Team 0 is whoever started on T.
//
// The demo never names a team in a way that survives a pug - it only says
// which SIDE a player is on, and sides swap at halftime and again in overtime.
// So the teams are recovered from who plays alongside whom.
type teamSplit struct {
	// tTeam[i] is the team index playing T in Rounds[i].
	tTeam []int
	// member is the team each player was first seen on.
	member map[uint64]int
}

func (s teamSplit) teamOn(round, side int) int {
	if side == sideT {
		return s.tTeam[round]
	}
	return 1 - s.tTeam[round]
}

// splitTeams follows the rosters through side swaps. Each round, the players
// already assigned to a team vote on which team is now playing T; anyone new
// (a substitute) joins the team of the side they appeared on. A tied vote
// keeps the previous round's answer, which is what an empty or unreadable
// roster should do.
func splitTeams(m *Match) teamSplit {
	s := teamSplit{tTeam: make([]int, len(m.Rounds)), member: make(map[uint64]int, 12)}
	tTeam := 0
	for i := range m.Rounds {
		players := m.Rounds[i].Meta.Players
		var votes [2]int
		for _, p := range players {
			side, ok := sideIndex(p.Team)
			team, known := s.member[p.SteamID]
			if !ok || !known {
				continue
			}
			if side == sideT {
				votes[team]++
			} else {
				votes[1-team]++
			}
		}
		if votes[0] != votes[1] {
			tTeam = 0
			if votes[1] > votes[0] {
				tTeam = 1
			}
		}
		s.tTeam[i] = tTeam
		for _, p := range players {
			side, ok := sideIndex(p.Team)
			if _, known := s.member[p.SteamID]; !ok || known {
				continue
			}
			s.member[p.SteamID] = s.teamOn(i, side)
		}
	}
	return s
}

// TeamDef names a team by the players who play for it. The list is a pool, not
// a lineup: it can hold seven people of whom any five turn up to a given game,
// or a single player whose team you want to follow from demo to demo.
type TeamDef struct {
	Name string
	IDs  map[uint64]bool
}

// ParseTeamDefs resolves "Name=player,player,..." specs. A player is a
// steamid64, or a name matched case-insensitively against names - which is
// every name seen across the demos being analysed.
func ParseTeamDefs(specs []string, names map[uint64]string) ([]TeamDef, error) {
	byName := make(map[string][]uint64, len(names))
	for id, n := range names {
		byName[strings.ToLower(n)] = append(byName[strings.ToLower(n)], id)
	}

	defs := make([]TeamDef, 0, len(specs))
	for _, spec := range specs {
		name, list, ok := strings.Cut(spec, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("team %q: want Name=player,player,...", spec)
		}
		def := TeamDef{Name: name, IDs: make(map[uint64]bool)}
		for _, tok := range strings.Split(list, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if id, err := strconv.ParseUint(tok, 10, 64); err == nil {
				def.IDs[id] = true
				continue
			}
			ids := byName[strings.ToLower(tok)]
			switch len(ids) {
			case 0:
				return nil, fmt.Errorf("team %q: no player named %q in these demos", name, tok)
			case 1:
				def.IDs[ids[0]] = true
			default:
				return nil, fmt.Errorf("team %q: %d players are named %q; use a steamid", name, len(ids), tok)
			}
		}
		if len(def.IDs) == 0 {
			return nil, fmt.Errorf("team %q: no players listed", name)
		}
		defs = append(defs, def)
	}
	return defs, nil
}

// nameTeams gives each of a match's two teams the key it is aggregated under.
//
// A team takes a TeamDef's name when it fields strictly more of that
// definition's players than the other team does. Everything else gets a key
// scoped to the demo, so two unrelated opponents are never summed together
// just because nobody named them.
func nameTeams(m *Match, split teamSplit, defs []TeamDef) (keys [2]string, warnings []string) {
	keys = [2]string{m.Name + "/T-start", m.Name + "/CT-start"}
	var named [2]bool
	for _, def := range defs {
		var n [2]int
		for id, team := range split.member {
			if def.IDs[id] {
				n[team]++
			}
		}
		if n[0] == n[1] {
			// Zero on both sides is the ordinary case of a demo that simply
			// does not involve this team; only a real split is worth a line.
			if n[0] > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"%s: team %q has %d players on each side, so neither was named", m.Name, def.Name, n[0]))
			}
			continue
		}
		team := 0
		if n[1] > n[0] {
			team = 1
		}
		if named[team] {
			warnings = append(warnings, fmt.Sprintf(
				"%s: team %q matches a side already named %q", m.Name, def.Name, keys[team]))
			continue
		}
		keys[team], named[team] = def.Name, true
	}
	return keys, warnings
}

func sortedIDs(set map[uint64]bool) []uint64 {
	ids := make([]uint64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
