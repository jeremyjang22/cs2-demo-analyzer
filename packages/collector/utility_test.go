package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// The CSV header and the row emitter must stay in lockstep. If they drift,
// every column after the drift point silently holds the wrong data.
func TestUtilityAppendRowMatchesColumns(t *testing.T) {
	u := Utility{Round: 1, StartTick: 100, Kind: UtilSmoke}
	row := u.AppendRow(nil)
	if len(row) != len(UtilityColumns()) {
		t.Fatalf("AppendRow produced %d values, UtilityColumns has %d",
			len(row), len(UtilityColumns()))
	}
}

func TestUtilityAppendRowFieldOrder(t *testing.T) {
	u := Utility{
		Round: 3, StartTick: 500, EndTick: 1140, Phase: PhaseLive,
		Kind: UtilMolotov, ThrowerSteamID: 77, ThrowerTeam: common.TeamTerrorists,
		X: 1.5, Y: -2.25, Z: 3,
	}
	row := u.AppendRow(nil)
	want := []string{"3", "500", "1140", "live", "molotov", "77", "2", "1.50", "-2.25", "3.00"}
	cols := UtilityColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}

// An effect that never expires keeps EndTick zero, which readers treat as
// "still active at round end". An instantaneous one must NOT look like that,
// so it records EndTick == StartTick instead.
func TestZeroEndTickDistinguishesUnexpiredFromInstant(t *testing.T) {
	unexpired := Utility{StartTick: 900}
	instant := Utility{StartTick: 900, EndTick: 900}

	if unexpired.EndTick != 0 {
		t.Error("an unexpired effect should carry EndTick 0")
	}
	if instant.EndTick == 0 {
		t.Error("an instantaneous effect must not be indistinguishable from unexpired")
	}
}

func TestAppendUtilityWithNoOpenRoundIsDropped(t *testing.T) {
	a, got := collect()

	if idx := a.appendUtility(Utility{Kind: UtilSmoke}); idx != -1 {
		t.Errorf("appendUtility with no round returned %d, want -1", idx)
	}

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	idx := a.appendUtility(Utility{Round: 1, StartTick: 2100, Kind: UtilSmoke})
	if idx != 0 {
		t.Fatalf("first utility index = %d, want 0", idx)
	}
	a.closeUtility(idx, 3200)
	a.roundEnd(3300, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3400)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	util := (*got)[0].Utility
	if len(util) != 1 {
		t.Fatalf("round carried %d utility rows, want 1", len(util))
	}
	if util[0].EndTick != 3200 {
		t.Errorf("EndTick = %d, want 3200 (closeUtility should stamp the expiry)", util[0].EndTick)
	}
}

// closeUtility must ignore an index that belongs to a round already flushed,
// rather than corrupting the round that replaced it.
func TestCloseUtilityIgnoresStaleIndex(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.appendUtility(Utility{Round: 1, Kind: UtilSmoke})
	a.roundEnd(2000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(2100)

	a.roundStart(3000, 2)
	a.closeUtility(0, 3500) // index from round 1; round 2 has no utility yet
	a.roundEnd(4000, common.TeamCounterTerrorists, events.RoundEndReasonCTWin)
	a.roundEndOfficial(4100)

	if len(*got) != 2 {
		t.Fatalf("emitted %d rounds, want 2", len(*got))
	}
	if n := len((*got)[1].Utility); n != 0 {
		t.Errorf("round 2 gained %d utility rows from a stale close", n)
	}
}
