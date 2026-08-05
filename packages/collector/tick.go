package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// PlayerTick is one player's complete state at one tick. This struct IS the
// schema: TickColumns and AppendRow below must stay aligned with it.
//
// Dead-row caveat: every column here except Health, Armor, IsAlive and
// VelValid is frozen at the moment of death rather than cleared. The server
// stops networking property updates for a dead pawn, and demoinfocs' cached
// property values simply hold whatever they last were - so Buttons,
// ShotsFired, ActiveWeapon, position, and everything else on a dead row
// silently repeat the player's last living values for the rest of the round.
// Confirmed against mega_ot_mirage.dem: of 836,195 dead rows, 643,327 carry a
// non-zero Buttons mask and 426,681 carry non-zero ShotsFired, because
// ButtonsPressedState latches on its last live value rather than resetting.
// Any aggregation over this data (spray patterns, movement, button-mashing,
// anything) MUST filter IsAlive == true first, or it will silently count a
// corpse as still playing.
type PlayerTick struct {
	Round   int32
	Tick    int32 // ingame tick, not demo frame
	Phase   Phase
	SteamID uint64
	Team    common.Team // on the tick row because it changes at halftime

	X, Y, Z float32 // world position, Hammer units

	// Yaw, Pitch are view angles in degrees. Yaw is read off
	// m_angEyeAngles.Y via demoinfocs' ViewDirectionX, whose own doc comment
	// claims a 0-360 range - that is wrong for the data actually observed
	// here: Yaw's real range is [-180, +180] and it wraps at that boundary
	// (e.g. -179 to +179 is a 2-degree turn, not a 358-degree one). Any
	// angular delta computed from Yaw must handle that wrap, not subtract
	// naively.
	Yaw, Pitch float32

	// Velocity is derived by differencing positions - CS2 does not network it.
	// VelValid is false on a player's first sampled tick in a round and after
	// any gap (death, respawn), where there is no predecessor to difference against.
	VelX, VelY, VelZ float32
	Speed            float32 // XY magnitude, units/sec; Z excluded so jumps don't inflate it

	// AccuracyPenalty is the active weapon's live inaccuracy
	// (m_fAccuracyPenalty): higher means less accurate. It rises while firing
	// or moving and decays toward 0 when still, making it the most direct
	// spray-quality signal available in the data. A spike against
	// mega_ot_mirage.dem proved the alternative - the player pawn's
	// m_pMovementServices.m_flMaxspeed - is a static 260.00 for every weapon
	// (knife, pistols, rifles, grenades), so it carries no information and was
	// dropped rather than kept as a dead column.
	AccuracyPenalty float32

	VelValid bool

	// Buttons is ButtonsPressedState, which demoinfocs binds to
	// m_pMovementServices.m_nButtonDownMaskPrev - the PREVIOUS command's
	// button mask, not the current one. So this column is one tick behind
	// this row's position/velocity. That matters most for counterstrafe
	// scoring specifically, where the button transition and the velocity
	// change need to line up: naively comparing them at the same Tick is off
	// by one.
	Buttons uint64 // raw input bitmask - see common.ButtonBitMask

	ShotsFired           int16   // m_iShotsFired - spray index, resets between bursts
	PunchYaw, PunchPitch float32 // m_vecCsViewPunchAngle - recoil kick on the view

	IsDucking, IsWalking, IsAirborne, IsScoped bool

	Health, Armor  int16
	IsAlive        bool
	FlashRemaining float32 // seconds
	ActiveWeapon   string
	Place          string // LastPlaceName(), e.g. "Palace"
}

// TickColumns is the CSV header for ticks.csv.gz, in AppendRow's emit order.
func TickColumns() []string {
	return []string{
		"round", "tick", "phase", "steamid", "team",
		"x", "y", "z", "yaw", "pitch",
		"vel_x", "vel_y", "vel_z", "speed", "accuracy_penalty", "vel_valid",
		"buttons", "shots_fired", "punch_yaw", "punch_pitch",
		"is_ducking", "is_walking", "is_airborne", "is_scoped",
		"health", "armor", "is_alive", "flash_remaining",
		"active_weapon", "place",
	}
}

// AppendRow appends this tick's CSV fields to dst and returns the extended
// slice. Callers pass buf[:0] to reuse one allocation across millions of rows.
func (t *PlayerTick) AppendRow(dst []string) []string {
	return append(dst,
		i32(t.Round),
		i32(t.Tick),
		t.Phase.String(),
		strconv.FormatUint(t.SteamID, 10),
		strconv.Itoa(int(t.Team)),
		f32(t.X), f32(t.Y), f32(t.Z),
		f32(t.Yaw), f32(t.Pitch),
		f32(t.VelX), f32(t.VelY), f32(t.VelZ),
		f32(t.Speed), accuracyStr(t.AccuracyPenalty),
		b(t.VelValid),
		strconv.FormatUint(t.Buttons, 10),
		strconv.Itoa(int(t.ShotsFired)),
		f32(t.PunchYaw), f32(t.PunchPitch),
		b(t.IsDucking), b(t.IsWalking), b(t.IsAirborne), b(t.IsScoped),
		strconv.Itoa(int(t.Health)), strconv.Itoa(int(t.Armor)),
		b(t.IsAlive),
		f32(t.FlashRemaining),
		t.ActiveWeapon,
		t.Place,
	)
}

// Two decimals is sub-millimeter precision in Hammer units and saves ~30% of
// the file size versus full float32 precision. This is NOT used for
// AccuracyPenalty - see accuracyStr.
func f32(v float32) string { return strconv.FormatFloat(float64(v), 'f', 2, 32) }
func i32(v int32) string   { return strconv.FormatInt(int64(v), 10) }

// accuracyStr formats AccuracyPenalty at full float32 precision rather than
// f32's fixed 2 decimals. The observed range in mega_ot_mirage.dem is
// [0.00, 0.34]: at 2 decimals that quantizes down to only 35 distinct values
// across 3.16M rows (2.34M of them landing on exactly "0.00"), destroying the
// low end where a settling weapon actually lives - the column exists
// specifically to be the spray-quality signal, so that quantization defeats
// its purpose. Position and velocity stay on f32: 2 decimals there is
// genuinely sub-millimeter in Hammer units and the size savings are worth it.
func accuracyStr(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
