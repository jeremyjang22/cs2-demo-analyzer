package collector

import "testing"

// The tracker's whole job is deciding what is worth a row. Emitting the first
// observation is the base case: without it a bomb that never changes hands
// would never appear at all.
func TestBombTrackerEmitsFirstObservation(t *testing.T) {
	var b bombTracker
	if !b.shouldEmit(BombCarried, 7, 100, 200, 0) {
		t.Error("first observation was suppressed, want a row")
	}
	if b.shouldEmit(BombCarried, 7, 100, 200, 0) {
		t.Error("an identical repeat produced a row, want suppression")
	}
}

// A carried bomb's position is the carrier's, and ticks.csv.gz already holds
// that once per tick. Repeating it here would be tens of thousands of rows
// saying nothing.
func TestBombTrackerIgnoresCarrierMovement(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombCarried, 7, 100, 200, 0)

	if b.shouldEmit(BombCarried, 7, 900, 900, 0) {
		t.Error("a carried bomb moving with its carrier produced a row, want suppression")
	}
}

// A bomb passed between two teammates never leaves the carried state, so
// state alone cannot detect it - which is exactly the handoff demoinfocs'
// BombDropped event documents itself as not firing for.
func TestBombTrackerEmitsOnCarrierChange(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombCarried, 7, 100, 200, 0)

	if !b.shouldEmit(BombCarried, 8, 100, 200, 0) {
		t.Error("a handoff to another player was suppressed, want a row")
	}
}

// A loose bomb has no carrier track shadowing it, so its movement is the only
// record of where it went. Below the epsilon is settling jitter; above it is
// a throw.
func TestBombTrackerEmitsLooseMovementOverEpsilon(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombDropped, 0, 100, 200, 0)

	if b.shouldEmit(BombDropped, 0, 101, 201, 0) {
		t.Error("sub-epsilon jitter produced a row, want suppression")
	}
	if !b.shouldEmit(BombDropped, 0, 100+moveEpsilon+1, 200, 0) {
		t.Error("a real move was suppressed, want a row")
	}
}

// Suppressed movement must not silently become the new baseline, or a bomb
// sliding one unit per frame would drift across the map without ever
// tripping the threshold.
func TestBombTrackerAccumulatesSuppressedDrift(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombDropped, 0, 100, 200, 0)

	const step = moveEpsilon / 2
	b.shouldEmit(BombDropped, 0, 100+step, 200, 0)
	if !b.shouldEmit(BombDropped, 0, 100+2*step+0.1, 200, 0) {
		t.Error("drift accumulated past the epsilon was still suppressed, want a row")
	}
}

// plant() exists so the BombPlanted handler can write its own row without the
// next poll immediately writing a duplicate.
func TestBombTrackerPlantSuppressesFollowUp(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombCarried, 7, 100, 200, 0)
	b.plant(7, "A", 300, 400, 0)

	if !b.planted {
		t.Error("plant() did not latch the planted state")
	}
	if b.shouldEmit(BombPlanted, 7, 300, 400, 0) {
		t.Error("the poll after a plant produced a duplicate row")
	}
	if b.site != "A" {
		t.Errorf("site = %q, want A", b.site)
	}
}

// A round's tracker state says nothing about the next round's. Leaking the
// planted latch would make every later round report a bomb already down.
func TestBombTrackerResetClearsRoundState(t *testing.T) {
	var b bombTracker
	b.plant(7, "B", 300, 400, 0)
	b.terminal = true
	b.reset()

	if b.planted || b.terminal || b.have || b.site != "" {
		t.Errorf("reset left state behind: %+v", b)
	}
}

// BomsiteUnknown is 0, which would render as a NUL byte in the CSV if it were
// treated as a rune like the two real sites are.
func TestSiteNameRejectsUnknown(t *testing.T) {
	if got := siteName(0); got != "" {
		t.Errorf("siteName(unknown) = %q, want empty", got)
	}
	if got := siteName('A'); got != "A" {
		t.Errorf("siteName('A') = %q, want A", got)
	}
	if got := siteName('B'); got != "B" {
		t.Errorf("siteName('B') = %q, want B", got)
	}
}

// The CSV header and the row emitter must stay in lockstep.
func TestBombAppendRowMatchesColumns(t *testing.T) {
	s := BombSample{Round: 1, Tick: 100, State: BombPlanted}
	row := s.AppendRow(nil)
	if len(row) != len(BombColumns()) {
		t.Fatalf("AppendRow produced %d values, BombColumns has %d",
			len(row), len(BombColumns()))
	}
}

func TestBombAppendRowFieldOrder(t *testing.T) {
	s := BombSample{
		Round: 4, Tick: 1200, Phase: PhaseLive, State: BombPlanted,
		CarrierSteamID: 77, CarrierTeam: 2, Site: "A",
		X: 1.5, Y: -2.25, Z: 3,
	}
	want := []string{"4", "1200", "live", "planted", "77", "2", "A", "1.50", "-2.25", "3.00"}
	row := s.AppendRow(nil)
	cols := BombColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}

// A defuse or an explosion reuses the plant's recorded position, and the
// consumer derives which floor of a multi-level map to draw it on from z.
// Leaving z at zero put Nuke's lower-bombsite detonations on the upper radar,
// which looked entirely plausible and was entirely wrong.
func TestBombTrackerKeepsPlantHeightForTheOutcome(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombCarried, 7, 0, 0, -500)
	b.plant(7, "B", 300, 400, -768)

	if b.z != -768 {
		t.Errorf("z after plant = %v, want -768 (the outcome is reported at this height)", b.z)
	}
}

// A loose bomb thrown down a level has to carry its new height too, or the
// row that records where it landed points at the wrong floor.
func TestBombTrackerUpdatesHeightWhenLooseBombMoves(t *testing.T) {
	var b bombTracker
	b.shouldEmit(BombDropped, 0, 0, 0, 100)

	if !b.shouldEmit(BombDropped, 0, moveEpsilon+1, 0, -400) {
		t.Fatal("a real move was suppressed")
	}
	if b.z != -400 {
		t.Errorf("z = %v, want -400", b.z)
	}
}
