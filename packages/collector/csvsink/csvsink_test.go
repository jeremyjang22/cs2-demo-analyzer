package csvsink

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

func testRound() *collector.Round {
	return &collector.Round{
		Meta: collector.RoundMeta{
			Number: 1, StartTick: 1000, FreezeEndTick: 2000,
			EndTick: 3000, OfficialEndTick: 3300, Complete: true,
			Players: []collector.PlayerRound{
				{SteamID: 7, MoneyAtFreezeEnd: 800, EquipValueAtFreezeEnd: 1000, Survived: true},
			},
		},
		Ticks: []collector.PlayerTick{
			{Round: 1, Tick: 2001, SteamID: 7, X: 100, Y: 200, Z: 50, Speed: 240, IsAlive: true},
			{Round: 1, Tick: 2002, SteamID: 7, X: 110, Y: 200, Z: 50, Speed: 250, IsAlive: true},
		},
	}
}

func readGzCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip %s: %v", path, err)
	}
	defer gz.Close()
	rows, err := csv.NewReader(gz).ReadAll()
	if err != nil {
		t.Fatalf("csv %s: %v", path, err)
	}
	return rows
}

func TestWritesAllFourFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Round(testRound()); err != nil {
		t.Fatalf("Round: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, name := range []string{"manifest.json", "rounds.csv", "round_players.csv", "ticks.csv.gz"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing output file %s: %v", name, err)
		}
	}
}

func TestTicksFileHasHeaderAndRows(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(testRound())
	s.Close()

	rows := readGzCSV(t, filepath.Join(dir, "ticks.csv.gz"))
	if len(rows) != 3 { // header + 2 ticks
		t.Fatalf("ticks.csv.gz has %d rows, want 3", len(rows))
	}
	want := collector.TickColumns()
	if len(rows[0]) != len(want) {
		t.Fatalf("header has %d columns, want %d", len(rows[0]), len(want))
	}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, rows[0][i], want[i])
		}
	}
}

func TestManifestRecordsCounts(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(testRound())
	s.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if m.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1", m.Rounds)
	}
	if m.TickRows != 2 {
		t.Errorf("TickRows = %d, want 2", m.TickRows)
	}
	if m.VelocitySource != "position_diff" {
		t.Errorf("VelocitySource = %q, want position_diff", m.VelocitySource)
	}
	if !m.Complete {
		t.Error("Complete = false, want true")
	}
	if m.Map != "de_mirage" {
		t.Errorf("Map = %q, want de_mirage", m.Map)
	}
}

// An incomplete round must poison the manifest's Complete flag, so a truncated
// dump is never mistaken for a whole match.
func TestIncompleteRoundMarksManifestIncomplete(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	r := testRound()
	r.Meta.Complete = false
	s.Round(r)
	s.Close()

	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var m manifest
	json.Unmarshal(raw, &m)
	if m.Complete {
		t.Error("manifest Complete = true, want false when a round is incomplete")
	}
}

func TestRoundPlayersRowsWritten(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(testRound())
	s.Close()

	f, _ := os.Open(filepath.Join(dir, "round_players.csv"))
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	if len(rows) != 2 { // header + 1 player
		t.Fatalf("round_players.csv has %d rows, want 2", len(rows))
	}
}
