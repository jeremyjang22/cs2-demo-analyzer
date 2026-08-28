package collector

import (
	"strconv"

	"github.com/golang/geo/r3"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// TrajectoryPoint is one sampled position on a thrown grenade's flight path.
// This struct IS the schema: TrajectoryColumns and AppendRow below must stay
// aligned with it.
//
// Rows group by (Round, ProjectileID) and are ordered by Seq: a whole throw is
// a polyline, not a single row. Storing it as points rather than an encoded
// path keeps the file readable by the same pandas/DuckDB one-liner as every
// other table here, at the cost of repeating the thrower on each row — which
// compresses to nothing.
//
// Points are sampled by this collector once per frame, NOT taken from
// demoinfocs' own GrenadeProjectile.Trajectory - see flightPath for why that
// field is unsuitable.
//
// A trajectory is where the grenade FLEW, which is not the same as where its
// effect ended up. utility.csv records the latter — the smoke's resting place,
// the inferno's origin. Both matter and neither substitutes for the other: a
// smoke that clips a doorframe and drops short has a revealing arc and an
// unremarkable landing spot.
type TrajectoryPoint struct {
	Round int32
	Tick  int32
	Phase Phase

	// ProjectileID groups the points of one throw. Sequential within a round
	// and restarting at 1 in each, like KitEvent.KitID - demoinfocs' own
	// UniqueID is a random int64, which is stable but miserable to read in a
	// CSV and meaningless across runs.
	ProjectileID int32
	// Seq orders the points within one throw, from 0 at the thrower's hand.
	Seq int16

	// Kind reuses utility.csv's vocabulary: smoke, flash, he, molotov,
	// incendiary, decoy. Taken from the projectile's own weapon, so a molotov
	// is a molotov here even though the inferno it becomes cannot say so.
	Kind           string
	ThrowerSteamID uint64
	ThrowerTeam    common.Team

	X, Y, Z float32
}

// TrajectoryColumns is the CSV header for trajectories.csv, in AppendRow's
// emit order.
func TrajectoryColumns() []string {
	return []string{
		"round", "tick", "phase", "projectile_id", "seq",
		"kind", "thrower_steamid", "thrower_team", "x", "y", "z",
	}
}

// AppendRow appends this point's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (p *TrajectoryPoint) AppendRow(dst []string) []string {
	return append(dst,
		i32(p.Round),
		i32(p.Tick),
		p.Phase.String(),
		i32(p.ProjectileID),
		strconv.Itoa(int(p.Seq)),
		p.Kind,
		strconv.FormatUint(p.ThrowerSteamID, 10),
		strconv.Itoa(int(p.ThrowerTeam)),
		f32(p.X), f32(p.Y), f32(p.Z),
	)
}

// trajectoryStep is the minimum distance between kept points, in Hammer units.
//
// A grenade in flight covers roughly ten to fifteen units a tick, so sampling
// every frame and keeping one point per 32 units gives a smooth arc at about a
// third of the rows. 32 units is under a player's width and well under a pixel
// or two on a radar, so the shape loses nothing visible. It also collapses the
// long tail where a landed smoke sits still for eighteen seconds.
const trajectoryStep = 32.0

// trajectorySettleTicks is how long a grenade must hold still before its path
// is considered over, in ticks.
//
// This is what separates "landed" from "entity destroyed", and they are not
// remotely the same moment: a smoke grenade's projectile lives for as long as
// the cloud does. Measured on the reference Anubis demo, a smoke thrown 0.33s
// into round 5 had its entity destroyed at 29.55s - so a path ended at
// destruction reports a 29-second flight for a two-second throw, and anything
// drawing it treats the grenade as still in the air for half the round.
//
// 24 ticks is 0.375s at 64 tick. A grenade in flight covers several hundred
// units in that time, far past trajectoryStep, so no real flight can settle by
// accident - while a grenade that has come to rest trips it immediately.

// grenadeKind maps a thrown projectile to utility.csv's kind vocabulary.
// Returns "" for anything that is not a grenade, which the caller drops.
func grenadeKind(eq common.EquipmentType) string {
	switch eq {
	case common.EqSmoke:
		return UtilSmoke
	case common.EqFlash:
		return UtilFlash
	case common.EqHE:
		return UtilHE
	case common.EqMolotov:
		return UtilMolotov
	case common.EqIncendiary:
		return UtilIncendiary
	case common.EqDecoy:
		return UtilDecoy
	}
	return ""
}

// flightPath accumulates one grenade's positions while it is in the air.
//
// It exists because demoinfocs' own GrenadeProjectile.Trajectory is not a
// flight path: reading datatables.go, it appends a point on exactly three
// occasions - the throw, each bounce, and the entity's destruction. Those are
// waypoints. Measured against the reference Nuke demo it produces a median of
// four points per throw, with consecutive points up to 900 units and 70 ticks
// apart, which draws as a couple of straight chords through walls rather than
// as an arc.
//
// So the position is sampled per frame instead, the same way pollInfernos
// samples a fire's spread and pollBomb samples a loose C4 - the established
// answer in this collector to a library that reports entity lifetime when what
// is wanted is entity motion.
type flightPath struct {
	// pr is the projectile being followed, held so the per-frame poll can read
	// its position without a second map keyed by the same id.
	pr *common.GrenadeProjectile
	// id is assigned at throw time so paths are numbered in throw order.
	id int32

	kind    string
	thrower uint64
	team    common.Team

	points []TrajectoryPoint
	// last is the position of the most recently kept point, which new samples
	// are measured against rather than against the previous frame - otherwise
	// a slow roll never accumulates enough movement to record anything.
	last r3.Vector
	have bool

	// lastMoveTick is when the grenade last actually went somewhere. settled
	// latches once it has been still for trajectorySettleTicks, after which
	// the caller stops sampling: everything past that point is a projectile
	// entity sitting on the floor waiting to be cleaned up.
	lastMoveTick int32
	settled      bool
}

// trajectorySettleTicks - see the constant block above.
const trajectorySettleTicks = 24

// done reports whether the grenade has come to rest and needs no more samples.
func (f *flightPath) done() bool { return f.settled }

// sample records a position if the projectile has moved far enough since the
// last kept one. force bypasses the distance check, for the first and last
// points of a path: the first is the thrower's hand and the last is where it
// came to rest, and a line missing either starts or ends in the wrong place.
func (f *flightPath) sample(tick int32, phase Phase, pos r3.Vector, force bool) {
	if f.settled {
		return
	}
	if f.have && !force {
		dx, dy, dz := pos.X-f.last.X, pos.Y-f.last.Y, pos.Z-f.last.Z
		if dx*dx+dy*dy+dz*dz < trajectoryStep*trajectoryStep {
			// Still where it was. Once it has been for long enough, the flight
			// is over and the path ends at the last point that meant anything.
			if tick-f.lastMoveTick >= trajectorySettleTicks {
				f.settled = true
			}
			return
		}
	}
	f.points = append(f.points, TrajectoryPoint{
		Tick: tick, Phase: phase, Seq: int16(len(f.points)),
		Kind: f.kind, ThrowerSteamID: f.thrower, ThrowerTeam: f.team,
		X: float32(pos.X), Y: float32(pos.Y), Z: float32(pos.Z),
	})
	f.last, f.have, f.lastMoveTick = pos, true, tick
}

// finish stamps the round and projectile id onto every point and hands the
// path back. The round is only knowable at flush time; the id was taken at
// throw and is passed back in so every point carries it.
func (f *flightPath) finish(round, id int32) []TrajectoryPoint {
	for i := range f.points {
		f.points[i].Round = round
		f.points[i].ProjectileID = id
	}
	return f.points
}
