package collector

import "testing"

// A kit leaving a player's inventory is the only signal a drop ever produces,
// because CS2 does not network the kit itself as an entity.
func TestKitTrackerDropOnLosingKit(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 0, 0)

	got := k.observe(1, false, 100, 200)
	if got.event != KitDropped {
		t.Fatalf("event = %q, want %q", got.event, KitDropped)
	}
	if got.id == 0 {
		t.Error("a dropped kit got no id; nothing can pair a pickup to it")
	}
}

// A player who has had a kit all along must not produce a row every frame.
func TestKitTrackerSilentWhileNothingChanges(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 0, 0)
	for i := 0; i < 5; i++ {
		if got := k.observe(1, true, float32(i), 0); got.event != "" {
			t.Fatalf("frame %d produced %q, want no event", i, got.event)
		}
	}
}

// Walking over a dropped kit is a pickup, and it must consume that specific
// kit so a viewer knows which one vanished.
func TestKitTrackerPickupClaimsTheNearbyKit(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 500, 500)
	dropped := k.observe(1, false, 500, 500)

	got := k.observe(2, true, 520, 510)
	if got.event != KitTaken {
		t.Fatalf("event = %q, want %q", got.event, KitTaken)
	}
	if got.id != dropped.id {
		t.Errorf("taken kit id = %d, want %d (the one that was dropped)", got.id, dropped.id)
	}
	if len(k.loose) != 0 {
		t.Errorf("%d kits still loose after the only one was taken", len(k.loose))
	}
}

// This is the distinction the whole table rests on: gaining a kit at spawn
// with nothing on the ground nearby is a BUY, and inventing a pickup for it
// would delete a kit that is still lying somewhere else on the map.
func TestKitTrackerGainFarFromAnyKitIsABuy(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 0, 0)
	k.observe(1, false, 0, 0) // a kit is now loose at the origin

	got := k.observe(2, true, 5000, 5000)
	if got.event != "" {
		t.Errorf("event = %q, want none: a buy is not a pickup", got.event)
	}
	if len(k.loose) != 1 {
		t.Errorf("the loose kit was consumed by a buy; %d left, want 1", len(k.loose))
	}
}

// A gain with nothing ever dropped in the round is the common case - 145 of
// the 155 gains across the reference demos.
func TestKitTrackerFirstGainIsNeverAPickup(t *testing.T) {
	k := newKitTracker()
	if got := k.observe(1, true, 100, 100); got.event != "" {
		t.Errorf("event = %q, want none", got.event)
	}
}

// With two kits down, the one actually walked over must be the one claimed -
// not simply the first one recorded.
func TestKitTrackerClaimsTheNearestKit(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 0, 0)
	far := k.observe(1, false, 0, 0)
	k.observe(2, true, 200, 0)
	near := k.observe(2, false, 200, 0)

	got := k.observe(3, true, 190, 10)
	if got.id != near.id {
		t.Errorf("claimed kit %d, want the nearer %d (the far one is %d)", got.id, near.id, far.id)
	}
}

// Exactly at the threshold counts; past it does not.
func TestKitTrackerRespectsTheMeasuredRadius(t *testing.T) {
	inside := newKitTracker()
	inside.observe(1, true, 0, 0)
	inside.observe(1, false, 0, 0)
	if got := inside.observe(2, true, KitPickupUnits, 0); got.event != KitTaken {
		t.Errorf("a gain exactly at the radius = %q, want %q", got.event, KitTaken)
	}

	outside := newKitTracker()
	outside.observe(1, true, 0, 0)
	outside.observe(1, false, 0, 0)
	if got := outside.observe(2, true, KitPickupUnits+1, 0); got.event != "" {
		t.Errorf("a gain past the radius = %q, want none", got.event)
	}
}

// Kit ids restart every round, so a stale id can never pair a taken row with a
// drop from a round that has already been flushed.
func TestKitTrackerResetClearsEverything(t *testing.T) {
	k := newKitTracker()
	k.observe(1, true, 0, 0)
	k.observe(1, false, 0, 0)
	k.reset()

	if len(k.loose) != 0 || k.next != 0 || len(k.had) != 0 {
		t.Errorf("reset left state: loose=%d next=%d had=%d", len(k.loose), k.next, len(k.had))
	}
	// A player who still holds a kit into the next round must not read as
	// having just gained one - the first observation only establishes state.
	if got := k.observe(1, true, 0, 0); got.event != "" {
		t.Errorf("first observation after reset = %q, want none", got.event)
	}
}

// The CSV header and the row emitter must stay in lockstep.
func TestKitAppendRowMatchesColumns(t *testing.T) {
	e := KitEvent{Round: 1, Tick: 100, Event: KitDropped}
	if row := e.AppendRow(nil); len(row) != len(KitColumns()) {
		t.Fatalf("AppendRow produced %d values, KitColumns has %d", len(row), len(KitColumns()))
	}
}

func TestKitAppendRowFieldOrder(t *testing.T) {
	e := KitEvent{
		Round: 7, Tick: 4096, Phase: PhaseLive, Event: KitTaken, KitID: 2,
		SteamID: 99, Team: 3, X: 1.5, Y: -2.25, Z: 3,
	}
	want := []string{"7", "4096", "live", "taken", "2", "99", "3", "1.50", "-2.25", "3.00"}
	row := e.AppendRow(nil)
	cols := KitColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}
