package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// PlayerTick is one player's complete state at one tick. This struct IS the
// schema: TickColumns and AppendRow below must stay aligned with it.
type PlayerTick struct {
	Round   int32
	Tick    int32 // ingame tick, not demo frame
	Phase   Phase
	SteamID uint64
	Team    common.Team // on the tick row because it changes at halftime

	X, Y, Z    float32 // world position, Hammer units
	Yaw, Pitch float32 // view angles, degrees

	// Velocity is derived by differencing positions - CS2 does not network it.
	// VelValid is false on a player's first sampled tick in a round and after
	// any gap (death, respawn), where there is no predecessor to difference against.
	VelX, VelY, VelZ float32
	Speed            float32 // XY magnitude, units/sec; Z excluded so jumps don't inflate it
	MaxSpeed         float32 // m_flMaxspeed - varies by weapon; Speed/MaxSpeed is comparable
	VelValid         bool

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
		"vel_x", "vel_y", "vel_z", "speed", "max_speed", "vel_valid",
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
		f32(t.Speed), f32(t.MaxSpeed),
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
// the file size versus full float32 precision.
func f32(v float32) string { return strconv.FormatFloat(float64(v), 'f', 2, 32) }
func i32(v int32) string   { return strconv.FormatInt(int64(v), 10) }

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
