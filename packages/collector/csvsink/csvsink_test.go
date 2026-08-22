package csvsink

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
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

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("csv %s: %v", path, err)
	}
	return rows
}

// rowByHeader zips a header row and a data row into a map, so a test can
// assert on a column by name rather than by position. This is deliberately
// the opposite of how csvsink itself writes rows (by position): if csvsink's
// positional writes and RoundColumns()/RoundPlayerColumns() ever drift apart,
// values land under the wrong header name and the assertions below catch it.
func rowByHeader(header, row []string) map[string]string {
	m := make(map[string]string, len(header))
	for i, name := range header {
		m[name] = row[i]
	}
	return m
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

// The complete latch must stay poisoned once any round is incomplete, even if
// later rounds in the same run are complete. This guards against a future
// refactor that recomputes the flag per round (e.g. `s.complete = m.Complete`)
// instead of latching it once and never clearing it.
func TestCompleteLatchStaysFalseAcrossRounds(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	incomplete := testRound()
	incomplete.Meta.Number = 1
	incomplete.Meta.Complete = false
	if err := s.Round(incomplete); err != nil {
		t.Fatalf("Round(incomplete): %v", err)
	}

	complete := testRound()
	complete.Meta.Number = 2
	complete.Meta.Complete = true
	if err := s.Round(complete); err != nil {
		t.Fatalf("Round(complete): %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Complete {
		t.Error("manifest Complete = true, want false: an earlier incomplete round must stay latched even after a later complete round")
	}
}

// Close() must attempt every flush and close, in order, even when an early
// one fails, so a late failure doesn't abandon output that was otherwise
// fine. We force the failure by closing s.ticksFile out from under the sink
// before calling Close(): the ticks stream's flush/gzip-close/file-close
// sequence will then error, and Close() must still flush and close
// rounds.csv and round_players.csv afterward.
//
// If the Finding 1 fix were reverted (each error check in Close() returning
// immediately), this test fails because rounds.csv would never be flushed:
// its Flush()/Close() calls live after the ticks section that now errors
// first, so the file would exist but be empty (header only, or not even
// that, depending on buffering).
func TestCloseSalvagesLaterStreamsAfterEarlyFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Round(testRound()); err != nil {
		t.Fatalf("Round: %v", err)
	}

	// Force the ticks stream to fail during Close() by closing its
	// underlying file early. Writes/closes against an already-closed
	// *os.File return an error rather than panicking, so this reliably
	// makes the ticks section of Close() fail without touching the
	// rounds/round_players files at all.
	if err := s.ticksFile.Close(); err != nil {
		t.Fatalf("pre-closing ticksFile: %v", err)
	}

	if err := s.Close(); err == nil {
		t.Fatal("Close() = nil error, want non-nil after forcing a ticks failure")
	}

	rows := readCSV(t, filepath.Join(dir, "rounds.csv"))
	if len(rows) != 2 { // header + 1 round
		t.Fatalf("rounds.csv has %d rows, want 2 (rounds.csv must still be flushed despite the earlier ticks failure)", len(rows))
	}

	rpRows := readCSV(t, filepath.Join(dir, "round_players.csv"))
	if len(rpRows) != 2 { // header + 1 player
		t.Fatalf("round_players.csv has %d rows, want 2 (round_players.csv must still be flushed despite the earlier ticks failure)", len(rpRows))
	}
}

// distinctiveRound returns a round whose field values are all pairwise
// distinct (and non-zero) within each written row, so that a column
// transposition in Round() shows up as a mismatched value rather than
// silently matching by coincidence.
func distinctiveRound() *collector.Round {
	return &collector.Round{
		Meta: collector.RoundMeta{
			Number:          21,
			StartTick:       32,
			FreezeEndTick:   43,
			EndTick:         54,
			OfficialEndTick: 65,
			Winner:          common.TeamCounterTerrorists,     // 3
			Reason:          events.RoundEndReasonBombDefused, // 7
			TimeoutBefore:   false,                            // "0"
			TimeoutTeam:     common.TeamTerrorists,            // 2
			Complete:        true,                             // "1"
			Players: []collector.PlayerRound{
				{
					SteamID:               76561198000000123,
					Team:                  common.TeamTerrorists, // 2
					MoneyAtFreezeEnd:      4800,
					EquipValueAtFreezeEnd: 3350,
					Survived:              true, // "1"
				},
			},
		},
		// 9 ticks so tick_rows ("9") doesn't collide with any other value
		// in the rounds.csv row above.
		Ticks: make([]collector.PlayerTick, 9),
	}
}

func TestRoundsCSVHeaderMatchesRoundColumns(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(distinctiveRound())
	s.Close()

	rows := readCSV(t, filepath.Join(dir, "rounds.csv"))
	if len(rows) < 1 {
		t.Fatalf("rounds.csv has no rows")
	}
	want := collector.RoundColumns()
	if len(rows[0]) != len(want) {
		t.Fatalf("rounds.csv header has %d columns, want %d", len(rows[0]), len(want))
	}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Errorf("rounds.csv column %d = %q, want %q", i, rows[0][i], want[i])
		}
	}
}

func TestRoundsCSVValuesLandInClaimedColumns(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(distinctiveRound())
	s.Close()

	rows := readCSV(t, filepath.Join(dir, "rounds.csv"))
	if len(rows) != 2 { // header + 1 round
		t.Fatalf("rounds.csv has %d rows, want 2", len(rows))
	}
	got := rowByHeader(rows[0], rows[1])

	want := map[string]string{
		"number":            "21",
		"start_tick":        "32",
		"freeze_end_tick":   "43",
		"end_tick":          "54",
		"official_end_tick": "65",
		"winner":            "3",
		"reason":            "7",
		"timeout_before":    "0",
		"timeout_team":      "2",
		"complete":          "1",
		"tick_rows":         "9",
	}
	for col, wantVal := range want {
		if got[col] != wantVal {
			t.Errorf("rounds.csv column %q = %q, want %q", col, got[col], wantVal)
		}
	}
}

func TestRoundPlayersCSVHeaderMatchesRoundPlayerColumns(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(distinctiveRound())
	s.Close()

	rows := readCSV(t, filepath.Join(dir, "round_players.csv"))
	if len(rows) < 1 {
		t.Fatalf("round_players.csv has no rows")
	}
	want := collector.RoundPlayerColumns()
	if len(rows[0]) != len(want) {
		t.Fatalf("round_players.csv header has %d columns, want %d", len(rows[0]), len(want))
	}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Errorf("round_players.csv column %d = %q, want %q", i, rows[0][i], want[i])
		}
	}
}

// Close() must be safe to call twice: main.go calls it via defer even on the
// happy path, where an explicit call may also exist during a transition
// period. Without an idempotency guard the second call double-closes
// s.ticksFile, which returns an error the second time around.
func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Round(testRound()); err != nil {
		t.Fatalf("Round: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v, want nil (Close must be idempotent)", err)
	}
}

// SetTickRate must reach the manifest, mirroring how SetMap already reaches
// it for the map name.
func TestSetTickRateUpdatesManifest(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: -1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.SetTickRate(128)
	if err := s.Round(testRound()); err != nil {
		t.Fatalf("Round: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.TickRate != 128 {
		t.Errorf("TickRate = %v, want 128", m.TickRate)
	}
	if m.TickRateSource != "measured" {
		t.Errorf("TickRateSource = %q, want %q when SetTickRate was called with a positive value", m.TickRateSource, "measured")
	}
}

// If CSVCMsg_ServerInfo never arrives, SetTickRate is never called, and the
// manifest must make that visible rather than silently reporting a default
// as if it were measured.
func TestUnresolvedTickRateIsVisibleInManifest(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: -1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// SetTickRate deliberately never called.
	if err := s.Round(testRound()); err != nil {
		t.Fatalf("Round: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.TickRate > 0 {
		t.Errorf("TickRate = %v, want <= 0 when never resolved (must not silently default to look measured)", m.TickRate)
	}
	if m.TickRateSource != "unknown" {
		t.Errorf("TickRateSource = %q, want %q when the tick rate was never resolved", m.TickRateSource, "unknown")
	}
}

// players.csv rows come from Go map iteration, which is randomized per run.
// Sorting by steamid makes the file byte-reproducible across runs of the
// same demo.
func TestPlayersSortedBySteamID(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// New opens the round/tick/kill files immediately. Without this, they stay
	// open and t.TempDir's cleanup fails on Windows, which cannot unlink a file
	// that is still held. (On Linux the unlink succeeds and the leak is
	// invisible, which is why this went unnoticed.)
	defer s.Close()

	names := map[uint64]string{
		300: "charlie",
		100: "alice",
		200: "bob",
	}
	if err := s.Players(names); err != nil {
		t.Fatalf("Players: %v", err)
	}

	rows := readCSV(t, filepath.Join(dir, "players.csv"))
	if len(rows) != 4 { // header + 3 players
		t.Fatalf("players.csv has %d rows, want 4", len(rows))
	}
	wantOrder := []string{"100", "200", "300"}
	for i, want := range wantOrder {
		if got := rows[i+1][0]; got != want {
			t.Errorf("row %d steamid = %q, want %q (players.csv must be sorted by steamid)", i+1, got, want)
		}
	}
}

func TestRoundPlayersCSVValuesLandInClaimedColumns(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, Meta{DemoFile: "test.dem", Map: "de_mirage", TickRate: 64})
	s.Round(distinctiveRound())
	s.Close()

	rows := readCSV(t, filepath.Join(dir, "round_players.csv"))
	if len(rows) != 2 { // header + 1 player
		t.Fatalf("round_players.csv has %d rows, want 2", len(rows))
	}
	got := rowByHeader(rows[0], rows[1])

	want := map[string]string{
		"round":                     "21",
		"steamid":                   "76561198000000123",
		"team":                      "2",
		"money_at_freeze_end":       "4800",
		"equip_value_at_freeze_end": "3350",
		"survived":                  "1",
	}
	for col, wantVal := range want {
		if got[col] != wantVal {
			t.Errorf("round_players.csv column %q = %q, want %q", col, got[col], wantVal)
		}
	}
}
