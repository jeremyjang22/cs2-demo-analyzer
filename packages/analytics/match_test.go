package analytics

import (
	"strings"
	"testing"
)

// Two halves and a substitute: 1-2 start T, swap to CT, and player 3 replaces
// player 2 after the swap. The split has to carry the team through both.
func TestSplitTeamsFollowsSwapsAndSubstitutes(t *testing.T) {
	m := &Match{Name: "demo", Rounds: []Round{
		round(1, tSide, []uint64{1, 2}, []uint64{11, 12}, nil),
		round(2, tSide, []uint64{11, 12}, []uint64{1, 2}, nil),
		round(3, tSide, []uint64{11, 12}, []uint64{1, 3}, nil),
		round(4, tSide, []uint64{1, 3}, []uint64{11, 12}, nil),
	}}
	s := splitTeams(m)

	if want := []int{0, 1, 1, 0}; len(s.tTeam) != 4 || s.tTeam[0] != 0 || s.tTeam[1] != 1 || s.tTeam[2] != 1 || s.tTeam[3] != 0 {
		t.Errorf("team on T by round = %v, want %v", s.tTeam, want)
	}
	for id, want := range map[uint64]int{1: 0, 2: 0, 3: 0, 11: 1, 12: 1} {
		if got := s.member[id]; got != want {
			t.Errorf("player %d on team %d, want %d", id, got, want)
		}
	}
}

func TestReportMergesANamedTeamAcrossDemosAndSides(t *testing.T) {
	// The same pool plays two demos with different lineups, starting on
	// opposite sides, and wins a round in each.
	first := &Match{Name: "a", Rounds: []Round{round(1, tSide, []uint64{1, 2}, []uint64{11, 12}, nil)}}
	second := &Match{Name: "b", Rounds: []Round{round(1, ctSide, []uint64{21, 22}, []uint64{1, 3}, nil)}}
	defs := []TeamDef{{Name: "us", IDs: map[uint64]bool{1: true, 2: true, 3: true}}}

	r := NewReport()
	r.Add(first, defs)
	r.Add(second, defs)

	us := r.teams["us"]
	if us == nil || us.demos != 2 || us.wins != [2]int{1, 1} || us.rounds != [2]int{1, 1} {
		t.Fatalf("team us = %+v, want 2 demos and a win on each side", us)
	}
	if got := r.players[playerKey{"us", 1}]; got.demos != 2 || got.rounds != 2 {
		t.Errorf("player 1 = %+v, want 2 demos and 2 rounds under the one team", got)
	}
	// Opponents nobody named stay separate, one key per demo.
	for _, key := range []string{"a/CT-start", "b/T-start"} {
		if r.teams[key] == nil {
			t.Errorf("no team %q; unnamed opponents should be keyed by demo", key)
		}
	}
}

func TestNameTeamsLeavesAnEvenSplitUnnamed(t *testing.T) {
	m := &Match{Name: "demo", Rounds: []Round{round(1, tSide, []uint64{1, 2}, []uint64{11, 12}, nil)}}
	keys, warnings := nameTeams(m, splitTeams(m), []TeamDef{{Name: "us", IDs: map[uint64]bool{1: true, 11: true}}})

	if keys != [2]string{"demo/T-start", "demo/CT-start"} {
		t.Errorf("keys = %v, want both left as demo-scoped defaults", keys)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"us"`) {
		t.Errorf("warnings = %v, want one naming the team", warnings)
	}
}

func TestParseTeamDefs(t *testing.T) {
	names := map[uint64]string{1: "Solitude", 2: "beter", 3: "twin", 4: "twin"}

	defs, err := ParseTeamDefs([]string{"us = solitude, 2 ,999"}, names)
	if err != nil {
		t.Fatal(err)
	}
	if d := defs[0]; d.Name != "us" || len(d.IDs) != 3 || !d.IDs[1] || !d.IDs[2] || !d.IDs[999] {
		t.Errorf("def = %+v, want us = {1, 2, 999}", d)
	}

	for _, bad := range []string{"no-equals", "us=", "us=nobody", "us=twin"} {
		if _, err := ParseTeamDefs([]string{bad}, names); err == nil {
			t.Errorf("ParseTeamDefs(%q) succeeded, want an error", bad)
		}
	}
}
