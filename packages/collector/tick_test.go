package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

func TestPhaseString(t *testing.T) {
	cases := []struct {
		phase Phase
		want  string
	}{
		{PhaseFreeze, "freeze"},
		{PhaseLive, "live"},
		{PhasePostRound, "postround"},
	}
	for _, c := range cases {
		if got := c.phase.String(); got != c.want {
			t.Errorf("Phase(%d).String() = %q, want %q", c.phase, got, c.want)
		}
	}
}

// The CSV header and the row emitter must stay in lockstep. If they drift,
// every column after the drift point silently holds the wrong data - the
// nastiest possible bug in a data pipeline, because nothing errors.
func TestAppendRowMatchesColumns(t *testing.T) {
	tick := PlayerTick{Round: 1, Tick: 100, SteamID: 76561198000000000}
	row := tick.AppendRow(nil)
	if len(row) != len(TickColumns()) {
		t.Fatalf("AppendRow produced %d values, TickColumns has %d",
			len(row), len(TickColumns()))
	}
}

// distinctiveTick returns a PlayerTick whose field values are pairwise
// distinct, so a transposition of two same-typed adjacent fields in
// AppendRow - exactly what removing MaxSpeed and inserting AccuracyPenalty
// risked, and TestAppendRowMatchesColumns above cannot catch because it only
// checks row length - shows up as a value landing under the wrong column
// name instead of silently matching by coincidence. Mirrors csvsink_test.go's
// distinctiveRound / rowByHeader pattern.
func distinctiveTick() PlayerTick {
	return PlayerTick{
		Round:   101,
		Tick:    202,
		Phase:   PhaseLive,
		SteamID: 76561198000000042,
		Team:    common.TeamCounterTerrorists, // 3

		X: 11, Y: 22, Z: 33, Yaw: 44, Pitch: 55,

		VelX: 66, VelY: 77, VelZ: 88, Speed: 99,
		AccuracyPenalty: 0.123456,
		VelValid:        true,

		Buttons:    123456789,
		ShotsFired: 17,
		PunchYaw:   1.5, PunchPitch: 2.5,

		IsDucking: true, IsWalking: false, IsAirborne: true, IsScoped: false,

		Health: 91, Armor: 92,
		IsAlive:        true,
		FlashRemaining: 3.75,
		ActiveWeapon:   "ak47",
		Place:          "Palace",
	}
}

// rowByHeader zips a header row and a data row into a map, so a test can
// assert on a column by name rather than by position.
func rowByHeader(header, row []string) map[string]string {
	m := make(map[string]string, len(header))
	for i, name := range header {
		m[name] = row[i]
	}
	return m
}

func TestAppendRowValuesLandInClaimedColumns(t *testing.T) {
	tick := distinctiveTick()
	row := tick.AppendRow(nil)
	header := TickColumns()
	if len(row) != len(header) {
		t.Fatalf("AppendRow produced %d values, TickColumns has %d", len(row), len(header))
	}
	got := rowByHeader(header, row)

	want := map[string]string{
		"round":            "101",
		"tick":             "202",
		"phase":            "live",
		"steamid":          "76561198000000042",
		"team":             "3",
		"x":                "11.00",
		"y":                "22.00",
		"z":                "33.00",
		"yaw":              "44.00",
		"pitch":            "55.00",
		"vel_x":            "66.00",
		"vel_y":            "77.00",
		"vel_z":            "88.00",
		"speed":            "99.00",
		"accuracy_penalty": "0.123456",
		"vel_valid":        "1",
		"buttons":          "123456789",
		"shots_fired":      "17",
		"punch_yaw":        "1.50",
		"punch_pitch":      "2.50",
		"is_ducking":       "1",
		"is_walking":       "0",
		"is_airborne":      "1",
		"is_scoped":        "0",
		"health":           "91",
		"armor":            "92",
		"is_alive":         "1",
		"flash_remaining":  "3.75",
		"active_weapon":    "ak47",
		"place":            "Palace",
	}
	if len(want) != len(header) {
		t.Fatalf("test covers %d columns, TickColumns has %d - every column must be checked", len(want), len(header))
	}
	for col, wantVal := range want {
		if got[col] != wantVal {
			t.Errorf("column %q = %q, want %q", col, got[col], wantVal)
		}
	}
}

func TestAppendRowReusesBuffer(t *testing.T) {
	tick := PlayerTick{Round: 1}
	buf := make([]string, 0, len(TickColumns()))
	for i := 0; i < 3; i++ {
		buf = tick.AppendRow(buf[:0])
		if len(buf) != len(TickColumns()) {
			t.Fatalf("iteration %d: got %d values, want %d",
				i, len(buf), len(TickColumns()))
		}
	}
}
