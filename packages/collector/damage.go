package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Damage kinds, as written to the kind column. Derived from the weapon rather
// than networked, so the set is closed and a consumer can switch on it
// exhaustively.
const (
	DmgBullet = "bullet"
	DmgHE     = "he"
	// DmgFire covers molotov and incendiary alike. Unlike utility.csv, which
	// distinguishes them because they burn for visibly different lengths,
	// damage from either is the same tick of the same burn - and the weapon
	// column still carries which grenade it was for anyone who cares.
	DmgFire = "fire"
	// DmgImpact is a thrown grenade physically striking someone - a smoke or
	// a flashbang to the head, which does exactly 1 damage. It is grenade
	// damage that has nothing to do with the grenade going off, so folding it
	// into he/fire would put a burn marker where nothing burned.
	DmgImpact = "impact"
	DmgBomb   = "bomb"
	// DmgFall is self-inflicted world damage from a drop. It has no attacker.
	DmgFall  = "fall"
	DmgKnife = "knife"
	DmgZeus  = "zeus"
	// DmgOther is anything the derivation above did not recognise - a new
	// weapon, or world damage that is not a fall. Kept as a real value rather
	// than silently folded into "bullet", so an unfamiliar source shows up as
	// unfamiliar instead of as a gunshot that never happened.
	DmgOther = "other"
)

// Damage is one PlayerHurt event: someone lost health or armour, and this is
// who, how much, and from what. This struct IS the schema: DamageColumns and
// AppendRow below must stay aligned with it.
//
// Every kill in kills.csv has a damage row behind it, but the reverse is very
// much not true - most damage does not kill, which is the point of recording
// it. Expect roughly five to ten rows here per kill row.
//
// HealthDamage is the raw amount, HealthRemaining the victim's health after
// it landed. Over-damage is therefore visible as HealthDamage exceeding the
// health the victim had: a 10 HP player hit for 100 records 100 and 0, not
// 10 and 0. demoinfocs also exposes a clamped figure; the raw one is kept
// because it is what the weapon did, and the clamped one is recoverable from
// the pair while the raw one would not be.
//
// AttackerSteamID is 0 for world damage - falling, and the bomb going off
// with nobody to credit. That matches kills.csv's convention for the same
// situation.
type Damage struct {
	Round int32
	Tick  int32
	Phase Phase

	VictimSteamID   uint64
	VictimTeam      common.Team
	AttackerSteamID uint64 // 0 for world damage
	AttackerTeam    common.Team

	Weapon string
	Kind   string

	HealthDamage    int16
	ArmorDamage     int16
	HealthRemaining int16
	ArmorRemaining  int16

	// HitGroup is demoinfocs' HitGroup: 1 head, 2 chest, 3 stomach, 4/5 arms,
	// 6/7 legs, 8 neck, 0 generic (which is what every non-bullet source
	// reports, since a molotov does not hit a limb).
	HitGroup uint8

	// Position is where the VICTIM was standing, not the attacker. A damage
	// marker belongs on the player who took it; the attacker's position at
	// the same tick is in ticks.csv.gz and, for gunfire, in shots.csv.
	X, Y, Z float32
}

// DamageColumns is the CSV header for damage.csv, in AppendRow's emit order.
func DamageColumns() []string {
	return []string{
		"round", "tick", "phase",
		"victim_steamid", "victim_team", "attacker_steamid", "attacker_team",
		"weapon", "kind", "health_damage", "armor_damage",
		"health_remaining", "armor_remaining", "hitgroup", "x", "y", "z",
	}
}

// AppendRow appends this row's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (d *Damage) AppendRow(dst []string) []string {
	return append(dst,
		i32(d.Round),
		i32(d.Tick),
		d.Phase.String(),
		strconv.FormatUint(d.VictimSteamID, 10),
		strconv.Itoa(int(d.VictimTeam)),
		strconv.FormatUint(d.AttackerSteamID, 10),
		strconv.Itoa(int(d.AttackerTeam)),
		d.Weapon,
		d.Kind,
		strconv.Itoa(int(d.HealthDamage)),
		strconv.Itoa(int(d.ArmorDamage)),
		strconv.Itoa(int(d.HealthRemaining)),
		strconv.Itoa(int(d.ArmorRemaining)),
		strconv.Itoa(int(d.HitGroup)),
		f32(d.X), f32(d.Y), f32(d.Z),
	)
}

// damageKind classifies a PlayerHurt by what caused it.
//
// The weapon is the whole signal: CS2 does not network a damage type. World
// damage arrives as EqWorld or EqUnknown with no attacker, and the only world
// damage a player takes in a normal match is a fall - so that combination is
// reported as such rather than as a nondescript "world". A self-inflicted
// grenade still names the grenade, so it lands in he/fire correctly even
// though attacker == victim.
func damageKind(weapon *common.Equipment, attacker, victim *common.Player) string {
	eq := common.EqUnknown
	if weapon != nil {
		eq = weapon.Type
	}

	switch eq {
	case common.EqHE:
		return DmgHE
	case common.EqMolotov, common.EqIncendiary:
		return DmgFire
	case common.EqSmoke, common.EqFlash, common.EqDecoy:
		return DmgImpact
	case common.EqBomb:
		return DmgBomb
	case common.EqKnife, common.EqAxe, common.EqHammer, common.EqWrench, common.EqFists:
		return DmgKnife
	case common.EqZeus:
		return DmgZeus
	case common.EqWorld, common.EqUnknown:
		if isSelfInflicted(attacker, victim) {
			return DmgFall
		}
		return DmgOther
	}

	switch eq.Class() {
	case common.EqClassPistols, common.EqClassSMG, common.EqClassHeavy, common.EqClassRifle:
		return DmgBullet
	}
	return DmgOther
}

// isSelfInflicted reports whether damage had no external attacker. CS2 demos
// are inconsistent about which of the two shapes fall damage takes - some
// report no attacker at all, others credit the victim to themselves - so both
// count.
func isSelfInflicted(attacker, victim *common.Player) bool {
	if attacker == nil {
		return true
	}
	return victim != nil && attacker.SteamID64 == victim.SteamID64
}

// weaponName renders the weapon behind a PlayerHurt. It prefers the resolved
// Equipment over demoinfocs' WeaponString, whose own documentation flags it
// as wrong for the CZ and the M4A1-S. Falls back to that string only when
// there is no Equipment at all, and to "World" when there is neither - the
// same token kills.csv already uses for world damage.
func weaponName(e events.PlayerHurt) string {
	if e.Weapon != nil && e.Weapon.Type != common.EqUnknown {
		return e.Weapon.String()
	}
	if e.WeaponString != "" {
		return e.WeaponString
	}
	return "World"
}
