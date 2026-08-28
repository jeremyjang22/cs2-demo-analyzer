package collector

import (
	"math"
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Defuse kit event kinds, as written to the event column.
const (
	// KitDropped is a kit hitting the ground, which in practice only ever
	// happens because the CT carrying it died.
	KitDropped = "dropped"
	// KitTaken is another CT picking that kit up.
	KitTaken = "taken"
)

// KitPickupUnits is how close a CT must be to a dropped kit for gaining one to
// count as picking that kit up rather than buying one.
//
// This threshold is what separates the two, and it is measured rather than
// guessed. Across the three reference demos, every gain that had any dropped
// kit in the same round landed within 213 units of one (0, 10, 33, 46, 67, 81,
// 82, 86, 89, 100, 116, 151, 213), and the other 145 gains had no dropped kit
// in that round at all - they are unambiguous buys. 250 clears the widest
// observed pickup with room to spare while staying far under the distance to a
// kit dropped anywhere but on top of the buyer.
const KitPickupUnits = 250.0

// KitEvent is a defuse kit changing hands with the ground. This struct IS the
// schema: KitColumns and AppendRow below must stay aligned with it.
//
// # This table is DERIVED, not observed
//
// Everything else the collector writes comes from something the demo actually
// says. A dropped kit does not: CS2 never networks one as an entity. Parsing
// the reference Nuke demo end to end yields 56 distinct server classes -
// CAK47, CC4, CPlantedC4, CSmokeGrenade and so on - and not one of them is a
// kit, kevlar or helmet. Those live only as properties on a player pawn
// (m_pItemServices.m_bHasDefuser), so the moment a CT dies, the kit they were
// carrying stops being represented anywhere in the demo at all.
//
// What IS observable is the property flipping. This table reconstructs the
// object from those transitions:
//
//   - has_defuser 1 -> 0 is a drop, at that player's position. In the three
//     reference demos all 135 such transitions land on the exact tick the
//     player died - not one happened to a living player - so "the kit went
//     away" and "its owner was killed" are the same event.
//   - has_defuser 0 -> 1 is a gain, which is either a buy or a pickup.
//     KitPickupUnits decides which; see its comment for the measurement.
//
// The consequences of it being derived are worth stating. A kit dropped by a
// player who DISCONNECTS is missed, because a disconnected player simply
// stops appearing in the tick stream and there is no transition to see. A kit
// picked up in the same instant somebody buys one nearby could in principle be
// mismatched. And a kit lying untouched at the end of a round produces a
// `dropped` row with no matching `taken` row, which is correct and not a gap.
type KitEvent struct {
	Round int32
	Tick  int32
	Phase Phase

	Event string
	// KitID pairs a taken row with the dropped row it refers to. Unique within
	// a round and restarting at 1 in each - a viewer needs to know WHICH kit
	// vanished when several are lying around, and nothing in the demo supplies
	// an identity for an object it does not model.
	KitID int32

	// SteamID is who dropped it, or who took it.
	SteamID uint64
	Team    common.Team

	X, Y, Z float32
}

// KitColumns is the CSV header for kits.csv, in AppendRow's emit order.
func KitColumns() []string {
	return []string{
		"round", "tick", "phase", "event", "kit_id",
		"steamid", "team", "x", "y", "z",
	}
}

// AppendRow appends this event's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (k *KitEvent) AppendRow(dst []string) []string {
	return append(dst,
		i32(k.Round),
		i32(k.Tick),
		k.Phase.String(),
		k.Event,
		i32(k.KitID),
		strconv.FormatUint(k.SteamID, 10),
		strconv.Itoa(int(k.Team)),
		f32(k.X), f32(k.Y), f32(k.Z),
	)
}

// looseKit is a kit lying on the ground, waiting to be claimed.
type looseKit struct {
	id   int32
	x, y float32
}

// kitTracker turns per-player has-defuser transitions into kit events.
//
// It holds the previous frame's state per player rather than reading a
// transition off the parser, because there is no kit event to register a
// handler for - see KitEvent's doc comment.
type kitTracker struct {
	had   map[uint64]bool
	loose []looseKit
	next  int32
}

func newKitTracker() *kitTracker {
	return &kitTracker{had: make(map[uint64]bool, 10)}
}

// reset clears everything at a round boundary. Kits do not survive a round,
// and a kit id from a flushed round would pair a taken row with a drop nobody
// can look up.
func (k *kitTracker) reset() {
	clear(k.had)
	k.loose = k.loose[:0]
	k.next = 0
}

// kitChange is what observe decides about one player on one frame.
type kitChange struct {
	event string
	id    int32
}

// observe records one player's kit state for this frame and reports the event
// it implies, if any. An empty event means nothing happened worth a row -
// including a gain that turned out to be a buy.
//
// Callers must pass every sampled player every frame; a player who stops being
// passed is simply forgotten rather than treated as having dropped anything,
// which is what makes a disconnect a silent miss rather than a phantom kit.
func (k *kitTracker) observe(steamID uint64, hasKit bool, x, y float32) kitChange {
	had := k.had[steamID]
	k.had[steamID] = hasKit

	switch {
	case had && !hasKit:
		k.next++
		k.loose = append(k.loose, looseKit{id: k.next, x: x, y: y})
		return kitChange{event: KitDropped, id: k.next}

	case !had && hasKit:
		if i := k.nearest(x, y); i >= 0 {
			id := k.loose[i].id
			k.loose = append(k.loose[:i], k.loose[i+1:]...)
			return kitChange{event: KitTaken, id: id}
		}
		// No kit within reach: they bought it. Not an event - the kit was
		// never on the ground to begin with.
		return kitChange{}
	}
	return kitChange{}
}

// nearest returns the index of the closest unclaimed kit within
// KitPickupUnits, or -1. Closest rather than first so two kits lying near each
// other resolve to the one actually walked over.
func (k *kitTracker) nearest(x, y float32) int {
	best, bestDist := -1, math.MaxFloat64
	for i, kit := range k.loose {
		d := math.Hypot(float64(kit.x-x), float64(kit.y-y))
		if d <= KitPickupUnits && d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}
