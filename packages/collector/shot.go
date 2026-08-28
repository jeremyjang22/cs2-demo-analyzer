package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Shot is one weapon-fire event: who pulled the trigger, from where, and
// facing which way. This struct IS the schema: ShotColumns and AppendRow
// below must stay aligned with it.
//
// One row per trigger pull, not per projectile. A shotgun blast is one row
// even though nine pellets leave the barrel, and a burst of ten rifle rounds
// is ten rows. That is the granularity the demo actually networks.
//
// There is no endpoint column, and adding one would mean inventing data. A
// demo records that a weapon fired and, separately, that someone took damage;
// it does not trace the bullet. Consumers wanting a tracer that terminates on
// a victim join this against damage.csv on (round, tick, attacker) - see
// docs/round-collector-schema.md. Shots that hit a wall have no join partner
// and no knowable endpoint, only a direction.
//
// Grenade throws and knife swings produce weapon-fire events too and are kept
// rather than filtered: the Weapon column identifies them, and dropping them
// here would mean this table could not answer "when did they throw it"
// without going back to the demo.
type Shot struct {
	Round int32
	Tick  int32
	Phase Phase

	SteamID uint64
	Team    common.Team
	Weapon  string

	// Position is the shooter's feet, matching ticks.csv.gz's convention so
	// the two tables plot on the same coordinates. Eye height is not recorded:
	// a top-down consumer does not use it, and a consumer that does can read
	// z plus a standing/crouching offset.
	X, Y, Z float32

	// Yaw and Pitch are the shooter's view angles at the fire tick, in
	// degrees. Yaw wraps at +/-180 like every other angle in this schema.
	// Together with Position this is the ray the bullet left along.
	Yaw, Pitch float32
}

// ShotColumns is the CSV header for shots.csv, in AppendRow's emit order.
func ShotColumns() []string {
	return []string{
		"round", "tick", "phase", "steamid", "team", "weapon",
		"x", "y", "z", "yaw", "pitch",
	}
}

// AppendRow appends this shot's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (s *Shot) AppendRow(dst []string) []string {
	return append(dst,
		i32(s.Round),
		i32(s.Tick),
		s.Phase.String(),
		strconv.FormatUint(s.SteamID, 10),
		strconv.Itoa(int(s.Team)),
		s.Weapon,
		f32(s.X), f32(s.Y), f32(s.Z),
		f32(s.Yaw), f32(s.Pitch),
	)
}
