package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// collect returns an assembler plus a pointer to the slice it emits into.
func collect() (*assembler, *[]*Round) {
	var got []*Round
	a := newAssembler(func(r *Round) { got = append(got, r) })
	return a, &got
}

func TestNormalRoundEmitsOnceComplete(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	if a.phase() != PhaseFreeze {
		t.Errorf("after roundStart phase = %v, want freeze", a.phase())
	}

	a.freezeEnd(2000, []PlayerRound{{SteamID: 7, MoneyAtFreezeEnd: 800}})
	if a.phase() != PhaseLive {
		t.Errorf("after freezeEnd phase = %v, want live", a.phase())
	}

	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	if a.phase() != PhasePostRound {
		t.Errorf("after roundEnd phase = %v, want postround", a.phase())
	}

	a.roundEndOfficial(3300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	m := (*got)[0].Meta
	if !m.Complete {
		t.Error("Complete = false, want true")
	}
	if m.Number != 1 || m.StartTick != 1000 || m.FreezeEndTick != 2000 ||
		m.EndTick != 3000 || m.OfficialEndTick != 3300 {
		t.Errorf("boundary ticks wrong: %+v", m)
	}
	if m.Winner != common.TeamTerrorists {
		t.Errorf("Winner = %v, want T", m.Winner)
	}
	if len(m.Players) != 1 || m.Players[0].MoneyAtFreezeEnd != 800 {
		t.Errorf("economy snapshot wrong: %+v", m.Players)
	}
}

// mp_restartgame fires a second RoundStart with no intervening end. The partial
// round must be discarded, not emitted as a truncated round.
func TestRestartDiscardsPartialRound(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	a.appendTick(PlayerTick{Tick: 2100})

	a.roundStart(5000, 1) // restart

	if len(*got) != 0 {
		t.Fatalf("emitted %d rounds on restart, want 0", len(*got))
	}

	a.freezeEnd(6000, nil)
	a.roundEnd(7000, common.TeamCounterTerrorists, events.RoundEndReasonCTWin)
	a.roundEndOfficial(7300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	if (*got)[0].Meta.StartTick != 5000 {
		t.Errorf("StartTick = %d, want 5000 (the restarted round)",
			(*got)[0].Meta.StartTick)
	}
	if n := len((*got)[0].Ticks); n != 0 {
		t.Errorf("restarted round carried %d stale ticks, want 0", n)
	}
}

// Demos routinely cut off before RoundEndOfficial on the final round. That
// round is still worth having - it just must be marked incomplete.
func TestFinishFlushesPendingRoundAsIncomplete(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 12)
	a.freezeEnd(2000, nil)
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.finish()

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	if (*got)[0].Meta.Complete {
		t.Error("Complete = true, want false for a flushed partial round")
	}
}

func TestFinishWithNoPendingRoundEmitsNothing(t *testing.T) {
	a, got := collect()
	a.finish()
	if len(*got) != 0 {
		t.Fatalf("emitted %d rounds, want 0", len(*got))
	}
}

// Stray events before the first RoundStart must not panic or emit.
func TestEventsBeforeRoundStartAreIgnored(t *testing.T) {
	a, got := collect()

	a.freezeEnd(100, nil)
	a.roundEnd(200, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(300)
	a.appendTick(PlayerTick{Tick: 400})

	if len(*got) != 0 {
		t.Fatalf("emitted %d rounds, want 0", len(*got))
	}
	if a.active() {
		t.Error("active() = true with no round open")
	}
}

func TestAppendTickOnlyWhenRoundOpen(t *testing.T) {
	a, got := collect()

	a.appendTick(PlayerTick{Tick: 1}) // dropped - no round open
	a.roundStart(1000, 1)
	a.appendTick(PlayerTick{Tick: 1001})
	a.appendTick(PlayerTick{Tick: 1002})
	a.roundEnd(2000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(2300)

	if n := len((*got)[0].Ticks); n != 2 {
		t.Errorf("round holds %d ticks, want 2", n)
	}
}
