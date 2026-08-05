package collector

import "testing"

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
