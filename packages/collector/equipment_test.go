package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// player builds a common.Player carrying the given equipment. Inventory is
// keyed by entity id, which readLoadout never reads - only the values matter.
func player(types ...common.EquipmentType) *common.Player {
	p := &common.Player{SteamID64: 7, Inventory: make(map[int]*common.Equipment, len(types))}
	for i, t := range types {
		p.Inventory[i] = &common.Equipment{Type: t}
	}
	return p
}

// The whole point of the loadout columns is telling a rifle from a pistol
// from a grenade, so the slotting rules are what these tests protect.
func TestReadLoadoutSlotsWeaponsByClass(t *testing.T) {
	l := readLoadout(player(common.EqAK47, common.EqGlock, common.EqKnife))

	if l.Primary != "AK-47" {
		t.Errorf("Primary = %q, want AK-47", l.Primary)
	}
	if l.Secondary != "Glock-18" {
		t.Errorf("Secondary = %q, want Glock-18", l.Secondary)
	}
	if l.Nades != "" {
		t.Errorf("Nades = %q, want empty", l.Nades)
	}
}

// SMGs and shotguns are primaries too. An MP9 in the pistol slot would read
// as a full buy with no rifle, which is the opposite of what it means.
func TestReadLoadoutTreatsSMGAndHeavyAsPrimary(t *testing.T) {
	for _, c := range []struct {
		eq   common.EquipmentType
		want string
	}{
		{common.EqMP9, "MP9"},
		{common.EqNova, "Nova"},
		{common.EqAWP, "AWP"},
	} {
		if got := readLoadout(player(c.eq)).Primary; got != c.want {
			t.Errorf("Primary for %v = %q, want %q", c.eq, got, c.want)
		}
	}
}

// Weapons() iterates a Go map, whose order is randomised per run. An
// unsorted Nades string would make the same demo produce different CSV bytes
// on every parse.
func TestReadLoadoutNadesAreSortedAndCounted(t *testing.T) {
	l := readLoadout(player(
		common.EqSmoke, common.EqFlash, common.EqHE, common.EqFlash,
	))
	if l.Nades != "ffhs" {
		t.Errorf("Nades = %q, want %q (sorted, two flashes)", l.Nades, "ffhs")
	}
	if got := NadeCounts(l.Nades)[NadeFlash]; got != 2 {
		t.Errorf("flash count = %d, want 2", got)
	}
}

// The C4 and the defuse kit are the two pieces of equipment that decide who
// can end a round, so each gets its own flag rather than hiding in a slot.
func TestReadLoadoutFlagsBombAndKit(t *testing.T) {
	carrier := readLoadout(player(common.EqBomb, common.EqGlock))
	if !carrier.HasBomb {
		t.Error("HasBomb = false for a player holding the C4")
	}
	if carrier.Primary != "" {
		t.Errorf("Primary = %q; the C4 must not fill a weapon slot", carrier.Primary)
	}

	// HasDefuseKit and HasHelmet read pawn properties, which cannot be
	// constructed outside demoinfocs - see collector_test.go's note on the
	// same limitation. What is testable is that an empty player reports
	// neither, rather than defaulting to true.
	empty := readLoadout(player())
	if empty.HasKit || empty.HasHelmet || empty.HasBomb {
		t.Errorf("empty loadout reports kit=%v helmet=%v bomb=%v, want all false",
			empty.HasKit, empty.HasHelmet, empty.HasBomb)
	}
}

// A nil player happens on the paths where demoinfocs could not attribute an
// entity. An empty loadout is a real state (full eco), so this must degrade
// rather than panic.
func TestReadLoadoutNilPlayerIsEmpty(t *testing.T) {
	if got := readLoadout(nil); got != (Loadout{}) {
		t.Errorf("readLoadout(nil) = %v, want the zero Loadout", got)
	}
}

// The loadout columns were APPENDED to the tick row, not inserted into it.
// A consumer reading ticks.csv.gz by position was written against the columns
// that existed before them, so every one of those has to keep its index -
// which holds exactly as long as the new ones all land after the last old one.
//
// Deliberately anchored to "after place" rather than "the last six": money
// arrived after these and pushed them off the end, and a test that breaks on
// a later append is testing the wrong thing.
func TestTickLoadoutColumnsAreAppendedNotInserted(t *testing.T) {
	tick := PlayerTick{
		Loadout: Loadout{
			HasHelmet: true, HasKit: true, HasBomb: false,
			Primary: "AK-47", Secondary: "Glock-18", Nades: "fs",
		},
	}
	row := tick.AppendRow(nil)
	cols := TickColumns()

	// "place" was the last column before the loadout block landed in 1.4.
	lastLegacy := indexOf(cols, "place")
	if lastLegacy < 0 {
		t.Fatal("TickColumns has no place column")
	}

	want := map[string]string{
		"has_helmet": "1", "has_kit": "1", "has_bomb": "0",
		"primary": "AK-47", "secondary": "Glock-18", "nades": "fs",
	}
	for name, expect := range want {
		i := indexOf(cols, name)
		if i < 0 {
			t.Fatalf("TickColumns has no %q column", name)
		}
		if i <= lastLegacy {
			t.Errorf("column %q is at index %d, at or before %q (%d) - appending must not "+
				"shift a column an existing consumer reads by position",
				name, i, cols[lastLegacy], lastLegacy)
		}
		if row[i] != expect {
			t.Errorf("column %q = %q, want %q", name, row[i], expect)
		}
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
