package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Utility kinds, as written to the kind column.
const (
	UtilSmoke   = "smoke"
	UtilMolotov = "molotov"
	// UtilIncendiary is the CT grenade. It burns visibly shorter than a
	// molotov - measured medians of 5.50s against 7.02s across the reference
	// demos - so the two are worth distinguishing rather than lumping.
	UtilIncendiary = "incendiary"
	UtilHE         = "he"
	UtilFlash      = "flash"
	UtilDecoy      = "decoy"
)

// Utility is one deployed grenade effect: where it landed, who threw it, and
// how long it lasted. This struct IS the schema: UtilityColumns and AppendRow
// below must stay aligned with it.
//
// Two shapes share this row. Smokes, molotovs and decoys occupy space over
// time, so StartTick and EndTick bracket a real interval. HE and flash are
// instantaneous, and record EndTick == StartTick rather than a zero that would
// read as "never ended".
//
// EndTick == 0 means the effect was still active when the round was flushed —
// its expiry event never arrived, which is normal for utility thrown late in a
// round. Treat it as "burning at round end", not as a zero-length effect.
//
// ThrowerSteamID is 0 when demoinfocs could not attribute the grenade. That is
// expected on POV demos and on partially corrupt ones; the effect is still
// worth drawing, so the row is kept rather than dropped.
type Utility struct {
	Round     int32
	StartTick int32
	EndTick   int32
	Phase     Phase

	Kind           string
	ThrowerSteamID uint64
	ThrowerTeam    common.Team

	// Position is where the effect sits, in Hammer units. For an inferno this
	// is the entity origin rather than the centroid of its individual fires,
	// which spread outward from it over time.
	X, Y, Z float32

	// Radius is how far the effect actually reached from Position, in Hammer
	// units - the peak distance to any of its fires, sampled while it burned.
	// Molotovs and incendiaries do not cover the same ground, so a viewer that
	// draws one fixed circle for both is wrong for at least one of them.
	//
	// Zero for smokes, decoys, HE and flash: those have fixed radii the game
	// defines, and nothing in the demo measures them.
	Radius float32
}

// UtilityColumns is the CSV header for utility.csv, in AppendRow's emit order.
func UtilityColumns() []string {
	return []string{
		"round", "start_tick", "end_tick", "phase", "kind",
		"thrower_steamid", "thrower_team", "x", "y", "z", "radius",
	}
}

// AppendRow appends this row's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (u *Utility) AppendRow(dst []string) []string {
	return append(dst,
		i32(u.Round),
		i32(u.StartTick),
		i32(u.EndTick),
		u.Phase.String(),
		u.Kind,
		strconv.FormatUint(u.ThrowerSteamID, 10),
		strconv.Itoa(int(u.ThrowerTeam)),
		f32(u.X), f32(u.Y), f32(u.Z), f32(u.Radius),
	)
}
