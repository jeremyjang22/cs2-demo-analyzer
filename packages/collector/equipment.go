package collector

import (
	"sort"
	"strings"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Grenade codes used by Loadout.Nades, one character each. Chosen so the
// string is readable at a glance ("hffs" = HE, two flashes, smoke) and stable
// across schema versions - a consumer that maps these characters to icons
// must not have to change when a new column lands next to them.
const (
	NadeHE         = 'h'
	NadeFlash      = 'f'
	NadeSmoke      = 's'
	NadeMolotov    = 'm'
	NadeIncendiary = 'i'
	NadeDecoy      = 'd'
)

// nadeCode maps a grenade equipment type to its Loadout.Nades character.
// Molotov and incendiary stay distinct here for the same reason they do in
// utility.go: they are different buys with different burn times, and a viewer
// showing a T holding an "incendiary" would be showing a lie.
var nadeCode = map[common.EquipmentType]byte{
	common.EqHE:         NadeHE,
	common.EqFlash:      NadeFlash,
	common.EqSmoke:      NadeSmoke,
	common.EqMolotov:    NadeMolotov,
	common.EqIncendiary: NadeIncendiary,
	common.EqDecoy:      NadeDecoy,
}

// Loadout is what a player is carrying at one tick, beyond the active weapon
// that ticks.csv already records. Armor is NOT here - it predates this struct
// and stays a plain PlayerTick field so the column order does not shift.
//
// Primary and Secondary hold weapon names ("AK-47", "Glock-18") rather than
// ids, matching active_weapon's existing convention: the strings repeat
// across millions of rows but compress to almost nothing, and a reader
// querying the CSV should not need a lookup table to ask "who had an AWP".
// Both are empty when the player has nothing in that slot, which is a real
// state - a T who dropped their rifle to plant, or anyone on a full eco.
//
// Nades is one character per grenade carried, sorted, so two flashes read
// "ff" and the whole loadout fits one short field. Sorting matters: Weapons()
// iterates a Go map, so an unsorted string would differ run to run for the
// same demo and make output non-reproducible.
//
// Knives are deliberately excluded. Everyone always has one, so a column that
// is constant carries no information - the same reasoning that removed
// max_speed in schema 1.1.
type Loadout struct {
	HasHelmet bool
	HasKit    bool // defuse kit; CT-only in practice
	HasBomb   bool // carrying the C4 right now

	Primary   string // rifle, SMG, or heavy
	Secondary string // pistol
	Nades     string
}

// readLoadout reads what p is carrying. A nil player, or one whose inventory
// has not been networked yet, yields the zero Loadout rather than an error:
// an empty loadout is indistinguishable from a real eco and both are drawn
// the same way, so failing the run over it would be theatre.
//
// Two weapons in the same slot cannot happen in normal play, but a demo mid
// pick-up can briefly report it. The lower EquipmentType id wins, purely so
// the choice is deterministic across runs - see Nades for why that matters.
func readLoadout(p *common.Player) Loadout {
	var l Loadout
	if p == nil {
		return l
	}

	l.HasHelmet = p.HasHelmet()
	l.HasKit = p.HasDefuseKit()

	var nades []byte
	primary, secondary := common.EqUnknown, common.EqUnknown

	for _, w := range p.Weapons() {
		if w == nil {
			continue
		}
		switch w.Type {
		case common.EqBomb:
			l.HasBomb = true
			continue
		case common.EqKnife:
			continue
		}
		if code, ok := nadeCode[w.Type]; ok {
			nades = append(nades, code)
			continue
		}
		switch w.Class() {
		case common.EqClassRifle, common.EqClassSMG, common.EqClassHeavy:
			if primary == common.EqUnknown || w.Type < primary {
				primary = w.Type
			}
		case common.EqClassPistols:
			if secondary == common.EqUnknown || w.Type < secondary {
				secondary = w.Type
			}
		}
	}

	sort.Slice(nades, func(i, j int) bool { return nades[i] < nades[j] })
	l.Nades = string(nades)
	if primary != common.EqUnknown {
		l.Primary = primary.String()
	}
	if secondary != common.EqUnknown {
		l.Secondary = secondary.String()
	}
	return l
}

// NadeCounts breaks a Nades string back into per-kind counts. Provided for
// consumers in Go; the CSV readers do this themselves.
func NadeCounts(nades string) map[byte]int {
	out := make(map[byte]int, len(nades))
	for i := 0; i < len(nades); i++ {
		out[nades[i]]++
	}
	return out
}

// String renders a loadout the way a reader would say it out loud. Used in
// test failure messages, not in the CSV.
func (l Loadout) String() string {
	parts := make([]string, 0, 4)
	if l.Primary != "" {
		parts = append(parts, l.Primary)
	}
	if l.Secondary != "" {
		parts = append(parts, l.Secondary)
	}
	if l.Nades != "" {
		parts = append(parts, l.Nades)
	}
	if l.HasKit {
		parts = append(parts, "kit")
	}
	if l.HasHelmet {
		parts = append(parts, "helmet")
	}
	if l.HasBomb {
		parts = append(parts, "c4")
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, " ")
}
