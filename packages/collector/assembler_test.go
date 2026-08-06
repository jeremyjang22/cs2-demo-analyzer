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
	// Seed Complete=true so the reset in finish() is observable; the zero value
	// is already false, so without seeding, finish() could be removed and the
	// test would still pass.
	a.cur.Meta.Complete = true
	a.finish()

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	if (*got)[0].Meta.Complete {
		t.Error("Complete = true, want false for a flushed partial round")
	}
}

// roundEnd has a guard against duplicate calls; the first should win.
func TestDuplicateRoundEndIgnoresSecond(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	// Call roundEnd again with different values; should be ignored.
	a.roundEnd(3100, common.TeamCounterTerrorists, events.RoundEndReasonCTWin)
	a.roundEndOfficial(3300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	m := (*got)[0].Meta
	// First roundEnd wins: EndTick and Winner should be from the first call, not the second.
	if m.EndTick != 3000 {
		t.Errorf("EndTick = %d, want 3000 (from first roundEnd)", m.EndTick)
	}
	if m.Winner != common.TeamTerrorists {
		t.Errorf("Winner = %v, want T (from first roundEnd)", m.Winner)
	}
	if m.Reason != events.RoundEndReasonTerroristsWin {
		t.Errorf("Reason = %v, want TerroristsWin (from first roundEnd)", m.Reason)
	}
}

func TestFinishWithNoPendingRoundEmitsNothing(t *testing.T) {
	a, got := collect()
	a.finish()
	if len(*got) != 0 {
		t.Fatalf("emitted %d rounds, want 0", len(*got))
	}
}

// active() must report true for the whole life of an open round, across every
// phase, and flip back to false once the round flushes.
func TestActiveReturnsTrueWhenRoundOpen(t *testing.T) {
	a, got := collect()

	if a.active() {
		t.Error("active() = true before any round opened")
	}

	a.roundStart(1000, 1)
	if !a.active() {
		t.Error("active() = false after roundStart, want true")
	}

	a.freezeEnd(2000, nil)
	if !a.active() {
		t.Error("active() = false in live phase, want true")
	}

	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	if !a.active() {
		t.Error("active() = false in postround phase, want true")
	}

	a.roundEndOfficial(3300)
	if a.active() {
		t.Error("active() = true after roundEndOfficial flushed the round")
	}

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
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

// setTimeout is how the Collector attributes an observed timeout to a round.
// It must land on Meta.TimeoutBefore/TimeoutTeam of the round that is
// eventually emitted, not get lost or silently apply to the wrong round.
func TestSetTimeoutMarksRound(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.setTimeout(common.TeamTerrorists)
	a.freezeEnd(2000, nil)
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	m := (*got)[0].Meta
	if !m.TimeoutBefore {
		t.Error("TimeoutBefore = false, want true")
	}
	if m.TimeoutTeam != common.TeamTerrorists {
		t.Errorf("TimeoutTeam = %v, want T", m.TimeoutTeam)
	}
}

// A round with no timeout must not carry a stray TimeoutBefore=true - the
// zero value must survive untouched when setTimeout is never called.
func TestNoTimeoutLeavesTimeoutBeforeFalse(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	if (*got)[0].Meta.TimeoutBefore {
		t.Error("TimeoutBefore = true, want false when setTimeout was never called")
	}
}

// setTimeout with no round open (e.g. called speculatively before the first
// RoundStart) must not panic - mirrors the nil guard every other assembler
// method has.
func TestSetTimeoutNoOpWithNoRoundOpen(t *testing.T) {
	a, got := collect()
	a.setTimeout(common.TeamCounterTerrorists) // must not panic
	if len(*got) != 0 {
		t.Fatalf("emitted %d rounds, want 0", len(*got))
	}
}

// A player who connects after freezetime ended is absent from the economy
// snapshot (which only ran once, at freezeEnd) but present in the tick
// stream. Without backfilling, round_players.csv would silently drop them -
// breaking any INNER JOIN against ticks.csv.gz.
func TestBackfillRosterAddsPlayerObservedOnlyInTicks(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.freezeEnd(2000, []PlayerRound{{SteamID: 7, Team: common.TeamTerrorists, MoneyAtFreezeEnd: 800}})
	a.appendTick(PlayerTick{Tick: 2100, SteamID: 7, Team: common.TeamTerrorists, IsAlive: true})
	// 8 joined after freezetime end: never in the economy snapshot, but
	// shows up in the tick stream mid-round.
	a.appendTick(PlayerTick{Tick: 2200, SteamID: 8, Team: common.TeamCounterTerrorists, IsAlive: true})
	a.appendTick(PlayerTick{Tick: 2300, SteamID: 8, Team: common.TeamCounterTerrorists, IsAlive: false})
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(*got))
	}
	players := (*got)[0].Meta.Players
	if len(players) != 2 {
		t.Fatalf("round has %d players, want 2 (original + backfilled)", len(players))
	}

	var backfilled *PlayerRound
	for i := range players {
		if players[i].SteamID == 8 {
			backfilled = &players[i]
		}
	}
	if backfilled == nil {
		t.Fatal("player 8 (observed only in ticks) is missing from round_players roster")
	}
	if !backfilled.JoinedLate {
		t.Error("JoinedLate = false, want true for a player backfilled from the tick stream")
	}
	if backfilled.MoneyAtFreezeEnd != 0 || backfilled.EquipValueAtFreezeEnd != 0 {
		t.Errorf("backfilled economy = (%d, %d), want (0, 0) - never observed at freezetime end",
			backfilled.MoneyAtFreezeEnd, backfilled.EquipValueAtFreezeEnd)
	}
	if backfilled.Team != common.TeamCounterTerrorists {
		t.Errorf("backfilled Team = %v, want CT (from their ticks)", backfilled.Team)
	}
	// Their last sampled tick had IsAlive=false.
	if backfilled.Survived {
		t.Error("backfilled Survived = true, want false (last observed tick was dead)")
	}

	// The originally-snapshotted player must be untouched.
	if players[0].SteamID != 7 || players[0].JoinedLate {
		t.Errorf("original roster entry corrupted: %+v", players[0])
	}
}

// A round where every player was correctly captured by the freezetime
// snapshot must not gain any spurious backfilled entries.
func TestBackfillRosterNoOpWhenTicksMatchRoster(t *testing.T) {
	a, got := collect()

	a.roundStart(1000, 1)
	a.freezeEnd(2000, []PlayerRound{{SteamID: 7, MoneyAtFreezeEnd: 800}})
	a.appendTick(PlayerTick{Tick: 2100, SteamID: 7})
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300)

	players := (*got)[0].Meta.Players
	if len(players) != 1 {
		t.Fatalf("round has %d players, want 1 (no backfill needed)", len(players))
	}
	if players[0].JoinedLate {
		t.Error("JoinedLate = true on the only, already-known player, want false")
	}
}

// setMaxRounds caps how many rounds flush() ever emits. demoinfocs can open
// round maxRounds+1 (RoundStart) before the maxRounds-triggered Cancel()
// actually stops parsing (see setMaxRounds's doc comment); finish() must not
// flush that extra round as an incomplete one, or "-max-rounds N" silently
// produces N+1 rounds on disk.
func TestMaxRoundsSkipsTrailingRoundOpenedAfterCap(t *testing.T) {
	a, got := collect()
	a.setMaxRounds(1)

	a.roundStart(1000, 1)
	a.freezeEnd(2000, nil)
	a.roundEnd(3000, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
	a.roundEndOfficial(3300) // round 1: reaches the cap, must still emit

	// Round 2 opens (the ordering quirk this guards against) but never
	// finishes before parsing stops.
	a.roundStart(4000, 2)
	a.appendTick(PlayerTick{Tick: 4001})
	a.finish()

	if len(*got) != 1 {
		t.Fatalf("emitted %d rounds, want 1 (round 2 must be discarded once the cap is reached)", len(*got))
	}
	if (*got)[0].Meta.Number != 1 {
		t.Errorf("emitted round number = %d, want 1", (*got)[0].Meta.Number)
	}
}

// A cap of zero (the default) must impose no limit at all.
func TestZeroMaxRoundsImposesNoLimit(t *testing.T) {
	a, got := collect()
	a.setMaxRounds(0)

	for n := int32(1); n <= 3; n++ {
		a.roundStart(1000*n, n)
		a.freezeEnd(2000*n, nil)
		a.roundEnd(3000*n, common.TeamTerrorists, events.RoundEndReasonTerroristsWin)
		a.roundEndOfficial(3300 * n)
	}

	if len(*got) != 3 {
		t.Fatalf("emitted %d rounds, want 3 (maxRounds=0 must not cap anything)", len(*got))
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
