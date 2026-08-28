package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

func eq(t common.EquipmentType) *common.Equipment { return &common.Equipment{Type: t} }

// The kind column is the whole reason damage.csv is more useful than a raw
// PlayerHurt dump: a viewer draws a bullet hit, a burn and a fall
// differently, and nothing in the demo says which is which.
func TestDamageKindClassifiesBySource(t *testing.T) {
	victim := &common.Player{SteamID64: 1}
	attacker := &common.Player{SteamID64: 2}

	cases := []struct {
		name     string
		weapon   *common.Equipment
		attacker *common.Player
		want     string
	}{
		{"rifle", eq(common.EqAK47), attacker, DmgBullet},
		{"pistol", eq(common.EqGlock), attacker, DmgBullet},
		{"smg", eq(common.EqMP9), attacker, DmgBullet},
		{"shotgun", eq(common.EqNova), attacker, DmgBullet},
		{"he grenade", eq(common.EqHE), attacker, DmgHE},
		{"molotov", eq(common.EqMolotov), attacker, DmgFire},
		{"incendiary", eq(common.EqIncendiary), attacker, DmgFire},
		{"c4", eq(common.EqBomb), nil, DmgBomb},
		{"smoke to the head", eq(common.EqSmoke), attacker, DmgImpact},
		{"flashbang to the head", eq(common.EqFlash), attacker, DmgImpact},
		{"knife", eq(common.EqKnife), attacker, DmgKnife},
		{"zeus", eq(common.EqZeus), attacker, DmgZeus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := damageKind(c.weapon, c.attacker, victim); got != c.want {
				t.Errorf("damageKind(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// Fall damage is the case with no attacker to name, and CS2 demos report it
// two different ways - no attacker at all, or the victim credited to
// themselves. Both have to land on "fall" or half of every match's fall
// damage would be mislabelled.
func TestDamageKindRecognisesBothShapesOfFallDamage(t *testing.T) {
	victim := &common.Player{SteamID64: 1}

	if got := damageKind(eq(common.EqWorld), nil, victim); got != DmgFall {
		t.Errorf("world damage with no attacker = %q, want %q", got, DmgFall)
	}
	if got := damageKind(eq(common.EqWorld), victim, victim); got != DmgFall {
		t.Errorf("world damage self-credited = %q, want %q", got, DmgFall)
	}
	// A missing weapon entirely is the same situation seen from the other side.
	if got := damageKind(nil, nil, victim); got != DmgFall {
		t.Errorf("no weapon and no attacker = %q, want %q", got, DmgFall)
	}
}

// A grenade that hurts its own thrower is still grenade damage. Routing it to
// "fall" on the self-inflicted check alone would be wrong - the check must
// only apply once the weapon is already known to be world damage.
func TestDamageKindSelfGrenadeIsStillGrenade(t *testing.T) {
	victim := &common.Player{SteamID64: 1}
	if got := damageKind(eq(common.EqHE), victim, victim); got != DmgHE {
		t.Errorf("self-inflicted HE = %q, want %q", got, DmgHE)
	}
}

// World damage WITH an attacker is not a fall - it is something this
// derivation has not been taught. Reporting it as "other" keeps it visible
// instead of quietly filing it under a source it did not come from.
func TestDamageKindUnattributedWorldDamageIsOther(t *testing.T) {
	victim := &common.Player{SteamID64: 1}
	attacker := &common.Player{SteamID64: 2}
	if got := damageKind(eq(common.EqWorld), attacker, victim); got != DmgOther {
		t.Errorf("world damage from another player = %q, want %q", got, DmgOther)
	}
}

// demoinfocs documents WeaponString as wrong for the CZ and the M4A1-S, so
// the resolved Equipment wins whenever there is one.
func TestWeaponNamePrefersResolvedEquipment(t *testing.T) {
	e := events.PlayerHurt{Weapon: eq(common.EqM4A1), WeaponString: "m4a1"}
	if got := weaponName(e); got != "M4A4-S" && got != "M4A1" {
		// The exact display string is demoinfocs', not ours; what matters is
		// that it is not the raw WeaponString.
		if got == "m4a1" {
			t.Errorf("weaponName = %q, want the resolved equipment name", got)
		}
	}
}

// With no equipment at all the row still has to say something, and "World"
// is the token kills.csv already uses for the same situation.
func TestWeaponNameFallsBackToWorld(t *testing.T) {
	if got := weaponName(events.PlayerHurt{}); got != "World" {
		t.Errorf("weaponName(empty) = %q, want World", got)
	}
	if got := weaponName(events.PlayerHurt{WeaponString: "inferno"}); got != "inferno" {
		t.Errorf("weaponName with only a string = %q, want inferno", got)
	}
}

// The CSV header and the row emitter must stay in lockstep.
func TestDamageAppendRowMatchesColumns(t *testing.T) {
	d := Damage{Round: 1, Tick: 100, Kind: DmgBullet}
	row := d.AppendRow(nil)
	if len(row) != len(DamageColumns()) {
		t.Fatalf("AppendRow produced %d values, DamageColumns has %d",
			len(row), len(DamageColumns()))
	}
}

func TestDamageAppendRowFieldOrder(t *testing.T) {
	d := Damage{
		Round: 2, Tick: 640, Phase: PhaseLive,
		VictimSteamID: 11, VictimTeam: 3, AttackerSteamID: 22, AttackerTeam: 2,
		Weapon: "AK-47", Kind: DmgBullet,
		HealthDamage: 98, ArmorDamage: 24, HealthRemaining: 2, ArmorRemaining: 76,
		HitGroup: 2, X: 1.5, Y: -2.25, Z: 3,
	}
	want := []string{
		"2", "640", "live", "11", "3", "22", "2", "AK-47", "bullet",
		"98", "24", "2", "76", "2", "1.50", "-2.25", "3.00",
	}
	row := d.AppendRow(nil)
	cols := DamageColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}

// Over-damage has to stay visible: a 10 HP player hit for 100 records what
// the weapon did, not what it could use.
func TestDamageKeepsRawHealthDamage(t *testing.T) {
	d := Damage{HealthDamage: 100, HealthRemaining: 0}
	if d.HealthDamage <= 0 || d.HealthRemaining != 0 {
		t.Fatal("fixture is wrong")
	}
	row := d.AppendRow(nil)
	i := indexOf(DamageColumns(), "health_damage")
	if row[i] != "100" {
		t.Errorf("health_damage = %q, want the raw 100 rather than a clamped value", row[i])
	}
}

// The CSV header and the row emitter must stay in lockstep.
func TestShotAppendRowMatchesColumns(t *testing.T) {
	s := Shot{Round: 1, Tick: 100, Weapon: "AK-47"}
	row := s.AppendRow(nil)
	if len(row) != len(ShotColumns()) {
		t.Fatalf("AppendRow produced %d values, ShotColumns has %d",
			len(row), len(ShotColumns()))
	}
}

func TestShotAppendRowFieldOrder(t *testing.T) {
	s := Shot{
		Round: 5, Tick: 2048, Phase: PhaseLive,
		SteamID: 33, Team: common.TeamCounterTerrorists, Weapon: "AWP",
		X: 10, Y: -20.5, Z: 64, Yaw: -179.25, Pitch: 3.5,
	}
	want := []string{"5", "2048", "live", "33", "3", "AWP",
		"10.00", "-20.50", "64.00", "-179.25", "3.50"}
	row := s.AppendRow(nil)
	cols := ShotColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}
