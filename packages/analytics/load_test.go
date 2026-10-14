package analytics

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
)

// LoadDir is the inverse of csvsink for the tables analytics reads, so the
// honest test is a round trip through the real writer: a report built from
// what was written must match one built from the rounds in memory.
func TestLoadDirReadsBackWhatTheCollectorWrote(t *testing.T) {
	late := kill(400, 12, ctSide, 2, tSide)
	late.Phase = collector.PhasePostRound
	late.Headshot = true
	first := round(7, ctSide, []uint64{1, 2}, []uint64{11, 12}, []uint64{2, 11, 12},
		kill(100, 11, ctSide, 1, tSide), late)
	first.Bomb = []collector.BombSample{
		{Round: 7, Tick: 20, Phase: collector.PhaseLive, State: collector.BombCarried, CarrierSteamID: 1},
		{Round: 7, Tick: 90, Phase: collector.PhaseLive, State: collector.BombPlanted, Site: "A"},
	}
	second := round(8, tSide, []uint64{11, 12}, []uint64{1, 2}, []uint64{11},
		kill(500, 1, ctSide, 12, tSide), kill(600, 11, tSide, 1, ctSide), kill(700, 11, tSide, 2, ctSide))
	want := &Match{Name: "demo", Map: "de_test", Names: map[uint64]string{1: "one", 11: "eleven"},
		Rounds: []Round{first, second}}

	dir := filepath.Join(t.TempDir(), "demo")
	sink, err := csvsink.New(dir, csvsink.Meta{Map: "de_test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range want.Rounds {
		if err := sink.Round(&collector.Round{Meta: r.Meta, Kills: r.Kills, Bomb: r.Bomb}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Players(want.Names); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	if !IsDemoDir(dir) {
		t.Fatalf("IsDemoDir(%s) = false for real collector output", dir)
	}
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.Map != "de_test" || !reflect.DeepEqual(got.Names, want.Names) {
		t.Errorf("loaded %q on %q with names %v", got.Name, got.Map, got.Names)
	}

	fromDisk, fromMemory := NewReport(), NewReport()
	fromDisk.Add(got, nil)
	fromMemory.Add(want, nil)
	if a, b := fromDisk.Tables(), fromMemory.Tables(); !reflect.DeepEqual(a, b) {
		for i := range a {
			if !reflect.DeepEqual(a[i], b[i]) {
				t.Errorf("%s differs:\n disk   %v\n memory %v", a[i].Name, a[i].Rows, b[i].Rows)
			}
		}
	}
	if len(fromDisk.Clutches) == 0 || len(fromDisk.Openings) != 2 {
		t.Errorf("round trip produced %d clutches and %d openings; the fixture should yield both",
			len(fromDisk.Clutches), len(fromDisk.Openings))
	}
}

// The analytics output folder has a manifest too, and by default lives inside
// the folder of demos it was built from. It must not be read back as a demo.
func TestIsDemoDirRejectsAnalyticsOutput(t *testing.T) {
	dir := t.TempDir()
	if err := report(round(1, tSide, []uint64{1}, []uint64{11}, []uint64{1, 11})).Write(dir); err != nil {
		t.Fatal(err)
	}
	if IsDemoDir(dir) {
		t.Error("IsDemoDir accepted an analytics output folder")
	}
}
