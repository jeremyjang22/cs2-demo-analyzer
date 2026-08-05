package collector

import "math"

// velocityTracker derives player velocity by differencing positions between
// ticks. This is not a fallback: CS2 does not network current player velocity
// (it is client-side predicted), so 324 properties on CCSPlayerPawn contain no
// velocity vector. Differencing is the only source.
type velocityTracker struct {
	tickRate float64
	last     map[uint64]sample
}

type sample struct {
	tick    int32
	x, y, z float32
}

func newVelocityTracker(tickRate float64) *velocityTracker {
	if tickRate <= 0 {
		tickRate = 64 // demos with a corrupt header report -1; corrected later via SetTickRate if the real value arrives
	}
	return &velocityTracker{tickRate: tickRate, last: make(map[uint64]sample, 16)}
}

// SetTickRate corrects the tick rate once the real value is known. The
// constructor runs before parsing starts (TickRate() is unavailable until
// CSVCMsg_ServerInfo arrives mid-parse), so it may have installed the
// hardcoded fallback above; this lets the caller fix that up as soon as the
// real rate is observed. Non-positive values are ignored so a not-yet-known
// reading can never downgrade an already-known rate.
func (v *velocityTracker) SetTickRate(tickRate float64) {
	if tickRate <= 0 {
		return
	}
	v.tickRate = tickRate
}

// maxTickGap bounds how many ticks may separate two consecutive samples for a
// player before they are treated as discontinuous rather than differenced.
// death/respawn already calls forget() to invalidate the very next sample,
// but a player can also drop out of Participants().Playing() mid-round for
// reasons that don't - e.g. a temporarily un-spawned pawn while reconnecting
// - and reappear hundreds of ticks later. Differencing across that kind of
// gap divides a real (possibly large) displacement by a correspondingly
// large dt, producing a small, smooth-looking velocity that is actually
// meaningless. 8 ticks is 125ms at 64-tick: generous for a single missed
// sample but well short of any real presence gap.
const maxTickGap = 8

// compute returns velocity in units/sec. valid is false when there is no usable
// predecessor - a player's first tick in a round, after a death gap, or after
// a presence gap wider than maxTickGap - so consumers can filter rather than
// trusting a bogus zero (or a bogus smooth low velocity).
func (v *velocityTracker) compute(steamID uint64, tick int32, x, y, z float32) (vx, vy, vz, speed float32, valid bool) {
	prev, ok := v.last[steamID]
	v.last[steamID] = sample{tick: tick, x: x, y: y, z: z}

	if !ok || tick <= prev.tick || tick-prev.tick > maxTickGap {
		return 0, 0, 0, 0, false
	}

	dt := float32(float64(tick-prev.tick) / v.tickRate)
	vx = (x - prev.x) / dt
	vy = (y - prev.y) / dt
	vz = (z - prev.z) / dt

	// XY only: a jump is vertical, and counting it would make a stationary
	// jumping player look like they are moving.
	speed = float32(math.Hypot(float64(vx), float64(vy)))

	return vx, vy, vz, speed, true
}

// reset drops all history. Called at round boundaries, where players teleport
// to spawn and any differenced velocity would be meaningless.
func (v *velocityTracker) reset() {
	clear(v.last)
}

// forget drops one player's history, used when they die so the respawn does not
// produce a teleport-sized velocity.
func (v *velocityTracker) forget(steamID uint64) {
	delete(v.last, steamID)
}
