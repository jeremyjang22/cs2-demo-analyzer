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
		tickRate = 64 // demos with a corrupt header report -1
	}
	return &velocityTracker{tickRate: tickRate, last: make(map[uint64]sample, 16)}
}

// compute returns velocity in units/sec. valid is false when there is no usable
// predecessor - a player's first tick in a round, or after a death gap - so
// consumers can filter rather than trusting a bogus zero.
func (v *velocityTracker) compute(steamID uint64, tick int32, x, y, z float32) (vx, vy, vz, speed float32, valid bool) {
	prev, ok := v.last[steamID]
	v.last[steamID] = sample{tick: tick, x: x, y: y, z: z}

	if !ok || tick <= prev.tick {
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
