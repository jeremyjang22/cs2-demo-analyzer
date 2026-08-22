package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// The CSV header and the row emitter must stay in lockstep. If they drift,
// every column after the drift point silently holds the wrong data.
func TestKillAppendRowMatchesColumns(t *testing.T) {
	k := Kill{Round: 1, Tick: 100, VictimSteamID: 76561198000000000}
	row := k.AppendRow(nil)
	if len(row) != len(KillColumns()) {
		t.Fatalf("AppendRow produced %d values, KillColumns has %d",
			len(row), len(KillColumns()))
	}
}

// distinctiveKill returns a Kill whose field values are pairwise distinct, so
// that swapping two same-typed adjacent fields in AppendRow shows up as a
// changed value rather than passing the length check above.
func distinctiveKill() Kill {
	return Kill{
		Round:           7,
		Tick:            4242,
		Phase:           PhaseLive,
		KillerSteamID:   111,
		KillerTeam:      common.TeamTerrorists,
		VictimSteamID:   222,
		VictimTeam:      common.TeamCounterTerrorists,
		AssisterSteamID: 333,
		AssistedFlash:   true,
		Weapon:          "AK-47",
		Headshot:        true,
		Penetrated:      2,
		NoScope:         false,
		ThroughSmoke:    true,
		AttackerBlind:   false,
		Distance:        12.5,
	}
}

func TestKillAppendRowFieldOrder(t *testing.T) {
	k := distinctiveKill()
	row := k.AppendRow(nil)
	want := []string{
		"7", "4242", "live",
		"111", "2", "222", "3",
		"333", "1",
		"AK-47", "1", "2", "0", "1",
		"0", "12.50",
	}
	if len(row) != len(want) {
		t.Fatalf("got %d fields, want %d", len(row), len(want))
	}
	cols := KillColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}

// A kill outside any open round - warmup, or the gap between
// roundEndOfficial and the next roundStart - has no round to belong to and
// must be dropped rather than panic or attach to the wrong round.
func TestAppendKillWithNoOpenRoundIsDropped(t *testing.T) {
	a, got := collect()

	a.appendKill(Kill{VictimSteamID: 1}) // before any roundStart

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	a.appendKill(Kill{Round: 1, VictimSteamID: 2})
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	a.appendKill(Kill{VictimSteamID: 3}) // round already flushed

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	kills := (*got)[0].Kills
	if len(kills) != 1 {
		t.Fatalf("round carried %d kills, want 1", len(kills))
	}
	if kills[0].VictimSteamID != 2 {
		t.Errorf("kept victim %d, want the one recorded inside the round (2)",
			kills[0].VictimSteamID)
	}
}

// A restart discards the open round; its kills must go with it rather than
// leaking into the round that replaces it.
func TestKillsDiscardedWithRestartedRound(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.appendKill(Kill{Round: 1, VictimSteamID: 1})
	a.roundStart(2000, 1) // restart, no official end
	a.appendKill(Kill{Round: 1, VictimSteamID: 2})
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	kills := (*got)[0].Kills
	if len(kills) != 1 || kills[0].VictimSteamID != 2 {
		t.Fatalf("kills = %+v, want only the post-restart kill (victim 2)", kills)
	}
}
