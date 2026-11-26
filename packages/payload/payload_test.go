package payload

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// Python's round() is the reference: ties to even, on the true binary value.
func TestRoundingMatchesPython(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{0.125, 0.12}, // exact tie, to even
		{0.375, 0.38}, // exact tie, to even
		{2.675, 2.67}, // really 2.67499999...
		{-0.015625, -0.02},
	} {
		if got := round2(c.in); got != c.want {
			t.Errorf("round2(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		in   float64
		want int
	}{{2.5, 2}, {3.5, 4}, {-89.5, -90}, {-0.4, 0}} {
		if got := whole(c.in); got != c.want {
			t.Errorf("whole(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// q2 reads a float as the CSV holds it, so 12.504 and "12.50" agree.
func TestQ2MatchesTheCSV(t *testing.T) {
	if got := whole(q2(12.504)); got != 12 {
		t.Errorf("whole(q2(12.504)) = %d, want 12 (the CSV says 12.50)", got)
	}
	if got := q2(-1106.507); got != -1106.51 {
		t.Errorf("q2(-1106.507) = %v", got)
	}
}

func TestOrderedKeepsInsertionOrder(t *testing.T) {
	o := newOrdered[int]()
	o.set("0:2", 2)
	o.set("0:10", 10)
	o.set("0:2", 3) // an update does not move it
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"0:2":3,"0:10":10}` {
		t.Errorf("got %s", raw)
	}
}

func tick(at int32, alive bool) collector.PlayerTick {
	return collector.PlayerTick{Tick: at, IsAlive: alive, Team: common.TeamTerrorists, Health: 100}
}

// Samples snap to the first real tick at or after each step, and a gap in the
// ticks repeats the tick after it rather than inventing one.
func TestSampleIndexesSnapForward(t *testing.T) {
	alive := []collector.PlayerTick{tick(100, true), tick(110, true), tick(140, true), tick(150, true)}
	got := sampleIndexes(alive, 16) // offsets 0,10,40,50; samples at 0,16,32,48
	want := []int{0, 2, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestTrackMarksTheDeathAndKeepsOnlyChanges(t *testing.T) {
	rows := []collector.PlayerTick{tick(0, true), tick(16, true), tick(32, true), tick(48, false)}
	rows[2].Health = 40
	tr := sampleTrack(rows, 16, func(float64) int { return 0 })
	if tr.D != 2 {
		t.Errorf("d = %d, want 2 (the last living sample)", tr.D)
	}
	if len(tr.Eq) != 2 || tr.Eq[1][0] != 2 || tr.Eq[1][1] != 40 {
		t.Errorf("eq = %v, want a change at sample 2 to 40 health", tr.Eq)
	}

	survivor := sampleTrack(rows[:3], 16, func(float64) int { return 0 })
	if survivor.D != -1 {
		t.Errorf("survivor d = %d, want -1", survivor.D)
	}
	if sampleTrack(rows[3:], 16, func(float64) int { return 0 }) != nil {
		t.Error("a player never alive this round has no track")
	}
}

var flat = events{base: 1000, end: 1000 + 64*60, tickRate: 64, span: 67, level: func(float64) int { return 0 }}

func TestDedupeKeepsTheEarliestDeath(t *testing.T) {
	got := dedupeKills([]collector.Kill{
		{Tick: 500, VictimSteamID: 1, Weapon: "second"},
		{Tick: 400, VictimSteamID: 1, Weapon: "first"},
		{Tick: 400, VictimSteamID: 2},
	})
	if len(got) != 2 || got[0].Weapon != "first" {
		t.Errorf("got %+v", got)
	}
}

// A shotgun blast is one hit with n pellets; hpt is the health actually lost.
func TestDamageMergesPelletsAndCountsHealthLost(t *testing.T) {
	pellet := func(hp, remaining int16, group uint8) collector.Damage {
		return collector.Damage{Tick: 1064, VictimSteamID: 7, AttackerSteamID: 9, Kind: "bullet",
			HealthDamage: hp, HealthRemaining: remaining, HitGroup: group}
	}
	got := flat.damage([]collector.Damage{pellet(20, 80, 3), pellet(90, 0, 1), pellet(20, 0, 2)})
	if len(got) != 1 {
		t.Fatalf("got %d hits, want 1", len(got))
	}
	h := got[0]
	if h.N != 3 || h.Hp != 130 || h.Hpt != 100 || h.Hg != 1 {
		t.Errorf("got n=%d hp=%d hpt=%d hg=%d, want 3/130/100/1 (hg of the biggest pellet)", h.N, h.Hp, h.Hpt, h.Hg)
	}
}

func TestShotEndsOnTheNearestVictim(t *testing.T) {
	shot := collector.Shot{Tick: 1064, Phase: collector.PhaseLive, SteamID: 9, Weapon: "XM1014"}
	got := flat.shots([]collector.Shot{shot, {Tick: 1100, Phase: collector.PhaseLive, SteamID: 9, Weapon: "AK-47"}},
		[]collector.Damage{
			{Tick: 1064, AttackerSteamID: 9, X: 300, Y: 0},
			{Tick: 1064, AttackerSteamID: 9, X: 100, Y: 0},
		})
	if len(got) != 2 || got[0].Hx == nil || *got[0].Hx != 100 {
		t.Fatalf("got %+v, want the first shot to end at x=100", got)
	}
	if got[1].Hx != nil {
		t.Error("a shot that hit nobody has no endpoint")
	}
}

func TestBombKeepsTheSpawnCarrierAndThinsTheArc(t *testing.T) {
	carried := func(at int32, by uint64) collector.BombSample {
		return collector.BombSample{Tick: at, State: "carried", CarrierSteamID: by}
	}
	got := flat.bomb([]collector.BombSample{
		carried(900, 1), carried(950, 2), // freezetime: the last one is who left spawn with it
		{Tick: 1064, State: "dropped"},
		{Tick: 1066, State: "dropped"}, // 0.03s later: thinned
		{Tick: 1080, State: "dropped"},
	})
	if len(got) != 3 {
		t.Fatalf("got %d rows: %+v", len(got), got)
	}
	if got[0].T != 0 || got[0].By == nil || *got[0].By != "2" {
		t.Errorf("first row %+v, want carrier 2 pinned to t=0", got[0])
	}
}

func TestSideIsAMajorityVote(t *testing.T) {
	b := &Builder{rosterA: map[uint64]bool{1: true, 2: true, 3: true}}
	players := []collector.PlayerRound{
		{SteamID: 1, Team: common.TeamCounterTerrorists},
		{SteamID: 2, Team: common.TeamCounterTerrorists},
		{SteamID: 3, Team: common.TeamTerrorists}, // a disconnect, or a misread
		{SteamID: 4, Team: common.TeamTerrorists},
	}
	if got := b.sideOf(players); got != teamCT {
		t.Errorf("side = %d, want CT", got)
	}
	if got := b.sideOf(nil); got != teamT {
		t.Errorf("nobody present: side = %d, want T", got)
	}
}

func TestRadarLevelsFollowTheFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("de_test.txt", `// comment
"de_test" {
	"pos_x" "-3453" "pos_y" "2887" "scale" "7"
	"verticalsections" {
		"default" { "AltitudeMax" "10000" "AltitudeMin" "-495" }
		"lower" { "AltitudeMax" "-495" "AltitudeMin" "-10000" }
	}
}`)
	// The smallest valid 2048x1 PNG is enough for the width check.
	png := mustPNG(t, 2048)
	write("de_test.png", png)
	write("de_test_lower.png", png)

	r, err := LoadRadar(dir, "de_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sections) != 2 || r.Sections[1] != "lower" {
		t.Errorf("sections = %v", r.Sections)
	}
	if r.Scale != 3.5 {
		t.Errorf("scale = %v, want 3.5 (7 at 1024 wide, normalised for 2048)", r.Scale)
	}
	if r.Level(0) != 0 || r.Level(-600) != 1 {
		t.Errorf("levels: z=0 -> %d, z=-600 -> %d", r.Level(0), r.Level(-600))
	}
}

func mustPNG(t *testing.T, width int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
