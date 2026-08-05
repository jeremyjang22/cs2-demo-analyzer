// Package csvsink writes collector Rounds to gzipped CSV plus a JSON manifest.
//
// Layout:
//
//	<dir>/manifest.json        schema version, map, tickrate, column lists, counts
//	<dir>/players.csv          one row per steamid, with their last-seen name
//	<dir>/rounds.csv           one row per round
//	<dir>/round_players.csv    one row per (round, player)
//	<dir>/ticks.csv.gz         one row per (round, tick, player)
package csvsink

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// schemaVersion follows the rule in the design doc: appending a trailing column
// bumps the minor part and stays readable by header-aware consumers; removing
// or reordering columns bumps the major part.
const schemaVersion = "1.0"

// Meta is the run-level information the manifest records.
type Meta struct {
	DemoFile string
	Map      string
	TickRate float64
}

type manifest struct {
	SchemaVersion  string              `json:"schema_version"`
	DemoFile       string              `json:"demo_file"`
	Map            string              `json:"map"`
	TickRate       float64             `json:"tick_rate"`
	Rounds         int                 `json:"rounds"`
	TickRows       int64               `json:"tick_rows"`
	Complete       bool                `json:"complete"`
	VelocitySource string              `json:"velocity_source"`
	Columns        map[string][]string `json:"columns"`
}

// Sink implements collector.Sink.
type Sink struct {
	dir  string
	meta Meta

	ticksFile *os.File
	gz        *gzip.Writer
	ticks     *csv.Writer

	roundsFile   *os.File
	rounds       *csv.Writer
	rpFile       *os.File
	roundPlayers *csv.Writer

	buf []string // reused across every tick row to avoid millions of allocations

	nRounds  int
	nTicks   int64
	complete bool
}

var _ collector.Sink = (*Sink)(nil)

func New(dir string, meta Meta) (*Sink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	s := &Sink{
		dir:      dir,
		meta:     meta,
		buf:      make([]string, 0, len(collector.TickColumns())),
		complete: true,
	}

	var err error
	if s.ticksFile, err = os.Create(filepath.Join(dir, "ticks.csv.gz")); err != nil {
		return nil, fmt.Errorf("create ticks.csv.gz: %w", err)
	}
	s.gz = gzip.NewWriter(s.ticksFile)
	s.ticks = csv.NewWriter(s.gz)
	if err := s.ticks.Write(collector.TickColumns()); err != nil {
		return nil, fmt.Errorf("write ticks header: %w", err)
	}

	if s.roundsFile, err = os.Create(filepath.Join(dir, "rounds.csv")); err != nil {
		return nil, fmt.Errorf("create rounds.csv: %w", err)
	}
	s.rounds = csv.NewWriter(s.roundsFile)
	if err := s.rounds.Write(collector.RoundColumns()); err != nil {
		return nil, fmt.Errorf("write rounds header: %w", err)
	}

	if s.rpFile, err = os.Create(filepath.Join(dir, "round_players.csv")); err != nil {
		return nil, fmt.Errorf("create round_players.csv: %w", err)
	}
	s.roundPlayers = csv.NewWriter(s.rpFile)
	if err := s.roundPlayers.Write(collector.RoundPlayerColumns()); err != nil {
		return nil, fmt.Errorf("write round_players header: %w", err)
	}

	return s, nil
}

func (s *Sink) Round(r *collector.Round) error {
	m := r.Meta

	if err := s.rounds.Write([]string{
		strconv.Itoa(int(m.Number)),
		strconv.Itoa(int(m.StartTick)),
		strconv.Itoa(int(m.FreezeEndTick)),
		strconv.Itoa(int(m.EndTick)),
		strconv.Itoa(int(m.OfficialEndTick)),
		strconv.Itoa(int(m.Winner)),
		strconv.Itoa(int(m.Reason)),
		boolStr(m.TimeoutBefore),
		strconv.Itoa(int(m.TimeoutTeam)),
		boolStr(m.Complete),
		strconv.Itoa(len(r.Ticks)),
	}); err != nil {
		return fmt.Errorf("write round %d: %w", m.Number, err)
	}

	for _, p := range m.Players {
		if err := s.roundPlayers.Write([]string{
			strconv.Itoa(int(m.Number)),
			strconv.FormatUint(p.SteamID, 10),
			strconv.Itoa(int(p.Team)),
			strconv.Itoa(int(p.MoneyAtFreezeEnd)),
			strconv.Itoa(int(p.EquipValueAtFreezeEnd)),
			boolStr(p.Survived),
		}); err != nil {
			return fmt.Errorf("write round_player r%d p%d: %w", m.Number, p.SteamID, err)
		}
	}

	for i := range r.Ticks {
		s.buf = r.Ticks[i].AppendRow(s.buf[:0])
		if err := s.ticks.Write(s.buf); err != nil {
			return fmt.Errorf("write tick r%d: %w", m.Number, err)
		}
	}

	s.nRounds++
	s.nTicks += int64(len(r.Ticks))
	if !m.Complete {
		s.complete = false
	}
	return nil
}

// Close flushes every writer and writes the manifest. The manifest is written
// last because it records counts that are only final once all rounds are in.
func (s *Sink) Close() error {
	s.ticks.Flush()
	if err := s.ticks.Error(); err != nil {
		return fmt.Errorf("flush ticks: %w", err)
	}
	if err := s.gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	if err := s.ticksFile.Close(); err != nil {
		return fmt.Errorf("close ticks file: %w", err)
	}

	s.rounds.Flush()
	if err := s.rounds.Error(); err != nil {
		return fmt.Errorf("flush rounds: %w", err)
	}
	if err := s.roundsFile.Close(); err != nil {
		return fmt.Errorf("close rounds file: %w", err)
	}

	s.roundPlayers.Flush()
	if err := s.roundPlayers.Error(); err != nil {
		return fmt.Errorf("flush round_players: %w", err)
	}
	if err := s.rpFile.Close(); err != nil {
		return fmt.Errorf("close round_players file: %w", err)
	}

	return s.writeManifest()
}

func (s *Sink) writeManifest() error {
	m := manifest{
		SchemaVersion:  schemaVersion,
		DemoFile:       s.meta.DemoFile,
		Map:            s.meta.Map,
		TickRate:       s.meta.TickRate,
		Rounds:         s.nRounds,
		TickRows:       s.nTicks,
		Complete:       s.complete,
		VelocitySource: "position_diff",
		Columns: map[string][]string{
			"ticks":         collector.TickColumns(),
			"rounds":        collector.RoundColumns(),
			"round_players": collector.RoundPlayerColumns(),
		},
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "manifest.json"), raw, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
