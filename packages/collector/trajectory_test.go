package collector

import (
	"testing"

	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// A grenade spends most of its recorded life not moving - a smoke sits where
// it landed for eighteen seconds. Those frames are the bulk of the samples and
// none of the information.
func TestFlightPathIgnoresAStationaryGrenade(t *testing.T) {
	f := &flightPath{kind: UtilSmoke}
	f.sample(1, PhaseLive, r3.Vector{X: 0, Y: 0}, true)
	for i := 0; i < 200; i++ {
		f.sample(int32(2+i), PhaseLive, r3.Vector{X: 1, Y: 1}, false)
	}
	if len(f.points) != 1 {
		t.Errorf("kept %d points for a grenade that never moved, want 1", len(f.points))
	}
}

// In flight it covers far more than the step per tick, so the arc survives.
func TestFlightPathKeepsAMovingGrenade(t *testing.T) {
	f := &flightPath{kind: UtilHE}
	for i := 0; i < 10; i++ {
		f.sample(int32(i), PhaseLive, r3.Vector{X: float64(i) * 60}, false)
	}
	if len(f.points) != 10 {
		t.Errorf("kept %d of 10 points on a fast arc", len(f.points))
	}
	for i, p := range f.points {
		if int(p.Seq) != i {
			t.Errorf("point %d has Seq %d; order is the polyline", i, p.Seq)
		}
	}
}

// Distance is measured against the last KEPT point, not the previous frame.
// Against the previous frame a slow roll would never accumulate enough
// movement to record anything and the grenade would appear to teleport.
func TestFlightPathAccumulatesSlowMovement(t *testing.T) {
	f := &flightPath{kind: UtilDecoy}
	f.sample(1, PhaseLive, r3.Vector{}, true)
	step := trajectoryStep / 4
	for i := 1; i <= 8; i++ {
		f.sample(int32(1+i), PhaseLive, r3.Vector{X: step * float64(i)}, false)
	}
	if len(f.points) < 3 {
		t.Errorf("a slow roll produced %d points; drift must accumulate", len(f.points))
	}
}

// force is what guarantees the throw and the resting place are both on the
// path, however little the grenade moved at either end.
func TestFlightPathForceKeepsBothEnds(t *testing.T) {
	f := &flightPath{kind: UtilFlash}
	f.sample(1, PhaseLive, r3.Vector{X: 100}, true)
	f.sample(2, PhaseLive, r3.Vector{X: 101}, true) // barely moved, but forced

	if len(f.points) != 2 {
		t.Fatalf("forced samples produced %d points, want 2", len(f.points))
	}
	if f.points[1].X != 101 {
		t.Errorf("last point X = %v, want the forced 101", f.points[1].X)
	}
}

// Round and id are only knowable at flush time, so finish stamps them onto
// every point - a path whose points disagree cannot be grouped.
func TestFlightPathFinishStampsEveryPoint(t *testing.T) {
	f := &flightPath{kind: UtilSmoke}
	for i := 0; i < 4; i++ {
		f.sample(int32(i), PhaseLive, r3.Vector{X: float64(i) * 100}, false)
	}
	points := f.finish(7, 3)
	for i, p := range points {
		if p.Round != 7 || p.ProjectileID != 3 {
			t.Errorf("point %d: round %d id %d, want 7 and 3", i, p.Round, p.ProjectileID)
		}
	}
}

// The kind vocabulary is shared with utility.csv on purpose, so a consumer can
// join a flight path to the effect it produced without a translation table.
func TestGrenadeKindMatchesUtilityVocabulary(t *testing.T) {
	cases := map[common.EquipmentType]string{
		common.EqSmoke:      UtilSmoke,
		common.EqFlash:      UtilFlash,
		common.EqHE:         UtilHE,
		common.EqMolotov:    UtilMolotov,
		common.EqIncendiary: UtilIncendiary,
		common.EqDecoy:      UtilDecoy,
	}
	for eq, want := range cases {
		if got := grenadeKind(eq); got != want {
			t.Errorf("grenadeKind(%v) = %q, want %q", eq, got, want)
		}
	}
	// Not a grenade: the caller drops these rather than writing a path for a
	// bullet.
	if got := grenadeKind(common.EqAK47); got != "" {
		t.Errorf("grenadeKind(AK-47) = %q, want empty", got)
	}
}

func TestTrajectoryAppendRowMatchesColumns(t *testing.T) {
	p := TrajectoryPoint{Round: 1, Kind: UtilSmoke}
	if row := p.AppendRow(nil); len(row) != len(TrajectoryColumns()) {
		t.Fatalf("AppendRow produced %d values, TrajectoryColumns has %d",
			len(row), len(TrajectoryColumns()))
	}
}

func TestTrajectoryAppendRowFieldOrder(t *testing.T) {
	p := TrajectoryPoint{
		Round: 3, Tick: 900, Phase: PhaseLive, ProjectileID: 4, Seq: 7,
		Kind: UtilMolotov, ThrowerSteamID: 55, ThrowerTeam: 2,
		X: 1.5, Y: -2.25, Z: 3,
	}
	want := []string{"3", "900", "live", "4", "7", "molotov", "55", "2", "1.50", "-2.25", "3.00"}
	row := p.AppendRow(nil)
	cols := TrajectoryColumns()
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %q (index %d) = %q, want %q", cols[i], i, row[i], want[i])
		}
	}
}
