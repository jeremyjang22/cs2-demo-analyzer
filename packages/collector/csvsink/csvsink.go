// Package csvsink writes collector Rounds to gzipped CSV plus a JSON manifest.
//
// Layout:
//
//	<dir>/manifest.json        schema version, map, tickrate, column lists, counts
//	<dir>/players.csv          one row per steamid, with their last-seen name
//	<dir>/rounds.csv           one row per round
//	<dir>/round_players.csv    one row per (round, player)
//	<dir>/kills.csv            one row per death
//	<dir>/utility.csv          one row per grenade effect
//	<dir>/ticks.csv.gz         one row per (round, tick, player)
package csvsink

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// schemaVersion follows the rule in the design doc: appending a trailing column
// bumps the minor part and stays readable by header-aware consumers; removing
// or reordering columns bumps the major part.
// 1.1 added kills.csv and kill_rows; 1.2 added utility.csv and utility_rows;
// 1.3 added the utility radius column and split the molotov kind into
// molotov/incendiary. All additive at the file level - a header-aware consumer
// keeps working - but note 1.3 changes a VALUE, not just a column: anything
// filtering kind == 'molotov' now misses roughly 60% of fire.
const schemaVersion = "1.3"

// Meta is the run-level information the manifest records.
type Meta struct {
	DemoFile string
	Map      string
	TickRate float64
}

type manifest struct {
	SchemaVersion string  `json:"schema_version"`
	DemoFile      string  `json:"demo_file"`
	Map           string  `json:"map"`
	TickRate      float64 `json:"tick_rate"`
	// TickRateSource is "measured" when TickRate came from the demo's
	// CSVCMsg_ServerInfo net-message (via SetTickRate), or "unknown" when
	// that message was never observed - in which case TickRate is whatever
	// was known at construction (typically <= 0) and velocity math fell back
	// to a hardcoded default internally. Never "measured" for a value that
	// was actually guessed, so a wrong number can't be mistaken for one that
	// was verified.
	TickRateSource string              `json:"tick_rate_source"`
	Rounds         int                 `json:"rounds"`
	TickRows       int64               `json:"tick_rows"`
	KillRows       int64               `json:"kill_rows"`
	UtilityRows    int64               `json:"utility_rows"`
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
	killsFile    *os.File
	kills        *csv.Writer
	utilFile     *os.File
	utility      *csv.Writer

	buf []string // reused across every tick row to avoid millions of allocations

	nRounds  int
	nTicks   int64
	nKills   int64
	nUtility int64
	complete bool
	closed   bool // guards Close() against double-closing its files on a second call
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
	// If we return an error below, close whatever s has already opened
	// instead of leaking the handles. ok flips to true only once every
	// file is open and every header is written.
	ok := false
	defer func() {
		if !ok {
			s.closeAll()
		}
	}()

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

	if s.killsFile, err = os.Create(filepath.Join(dir, "kills.csv")); err != nil {
		return nil, fmt.Errorf("create kills.csv: %w", err)
	}
	s.kills = csv.NewWriter(s.killsFile)
	if err := s.kills.Write(collector.KillColumns()); err != nil {
		return nil, fmt.Errorf("write kills header: %w", err)
	}

	if s.utilFile, err = os.Create(filepath.Join(dir, "utility.csv")); err != nil {
		return nil, fmt.Errorf("create utility.csv: %w", err)
	}
	s.utility = csv.NewWriter(s.utilFile)
	if err := s.utility.Write(collector.UtilityColumns()); err != nil {
		return nil, fmt.Errorf("write utility header: %w", err)
	}

	ok = true
	return s, nil
}

// closeAll closes every handle opened so far, ignoring errors. It exists to
// release OS resources when New() fails partway through, not to guarantee
// flushed output — the caller is about to discard this Sink entirely.
func (s *Sink) closeAll() {
	if s.gz != nil {
		s.gz.Close()
	}
	if s.ticksFile != nil {
		s.ticksFile.Close()
	}
	if s.roundsFile != nil {
		s.roundsFile.Close()
	}
	if s.rpFile != nil {
		s.rpFile.Close()
	}
	if s.killsFile != nil {
		s.killsFile.Close()
	}
	if s.utilFile != nil {
		s.utilFile.Close()
	}
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
			boolStr(p.Disconnected),
			boolStr(p.JoinedLate),
		}); err != nil {
			return fmt.Errorf("write round_player r%d p%d: %w", m.Number, p.SteamID, err)
		}
	}

	for i := range r.Kills {
		s.buf = r.Kills[i].AppendRow(s.buf[:0])
		if err := s.kills.Write(s.buf); err != nil {
			return fmt.Errorf("write kill r%d: %w", m.Number, err)
		}
	}

	for i := range r.Utility {
		s.buf = r.Utility[i].AppendRow(s.buf[:0])
		if err := s.utility.Write(s.buf); err != nil {
			return fmt.Errorf("write utility r%d: %w", m.Number, err)
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
	s.nKills += int64(len(r.Kills))
	s.nUtility += int64(len(r.Utility))
	if !m.Complete {
		s.complete = false
	}
	return nil
}

// Close flushes every writer and closes every file, in the order csv flush ->
// gzip close -> file close for each of the three streams, and it does so even
// when an earlier step in that sequence failed. A single early failure (say,
// a full disk hitting the ticks stream first) must not abandon rounds.csv and
// round_players.csv unflushed: a demo run takes minutes to regenerate, so
// whatever output can be salvaged is worth salvaging.
//
// The first error encountered is returned; later errors are discarded. The
// manifest is only written once everything above has succeeded, since it
// records final counts and a "complete" flag that would be misleading to
// write after a failed close.
func (s *Sink) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	var first error
	record := func(step string, err error) {
		if err != nil && first == nil {
			first = fmt.Errorf("%s: %w", step, err)
		}
	}

	s.ticks.Flush()
	record("flush ticks", s.ticks.Error())
	record("close gzip", s.gz.Close())
	record("close ticks file", s.ticksFile.Close())

	s.rounds.Flush()
	record("flush rounds", s.rounds.Error())
	record("close rounds file", s.roundsFile.Close())

	s.roundPlayers.Flush()
	record("flush round_players", s.roundPlayers.Error())
	record("close round_players file", s.rpFile.Close())

	s.kills.Flush()
	record("flush kills", s.kills.Error())
	record("close kills file", s.killsFile.Close())

	s.utility.Flush()
	record("flush utility", s.utility.Error())
	record("close utility file", s.utilFile.Close())

	if first != nil {
		return first
	}
	return s.writeManifest()
}

func (s *Sink) writeManifest() error {
	// TickRate is only ever set to a real value via SetTickRate, called from
	// a CSVCMsg_ServerInfo handler. If that never fired, TickRate is still
	// whatever was passed to New() (typically <= 0, since it's read before
	// parsing starts) - a value visibly distinct from any real tick rate,
	// rather than a hardcoded default masquerading as a measurement.
	tickRateSource := "measured"
	if s.meta.TickRate <= 0 {
		tickRateSource = "unknown"
	}

	m := manifest{
		SchemaVersion:  schemaVersion,
		DemoFile:       s.meta.DemoFile,
		Map:            s.meta.Map,
		TickRate:       s.meta.TickRate,
		TickRateSource: tickRateSource,
		Rounds:         s.nRounds,
		TickRows:       s.nTicks,
		KillRows:       s.nKills,
		UtilityRows:    s.nUtility,
		Complete:       s.complete,
		VelocitySource: "position_diff",
		Columns: map[string][]string{
			"ticks":         collector.TickColumns(),
			"rounds":        collector.RoundColumns(),
			"round_players": collector.RoundPlayerColumns(),
			"kills":         collector.KillColumns(),
			"utility":       collector.UtilityColumns(),
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

// SetMap records the map name. The name arrives from a net-message during
// parsing, after the sink is constructed, so it cannot be passed to New. Safe
// to call any time before Close, which is when the manifest is written.
func (s *Sink) SetMap(name string) { s.meta.Map = name }

// SetTickRate records the demo's real tick rate. Like SetMap, this arrives
// from a net-message (CSVCMsg_ServerInfo) after the sink is constructed -
// TickRate() is unusable before parsing starts, always reporting <= 0. Safe
// to call any time before Close, which is when the manifest is written and
// derives tick_rate_source from whether this was ever called with a
// positive value.
func (s *Sink) SetTickRate(rate float64) { s.meta.TickRate = rate }

// Players writes players.csv: one row per steamid, holding their last-seen
// name. Names live here rather than on tick rows because repeating them across
// millions of rows is waste.
//
// Note this collapses mid-match name changes to the final name. The design doc
// specifies a full (steamid, name, first_seen_tick) observation table; that is
// deliberately deferred until a demo with a rename shows it matters.
func (s *Sink) Players(names map[uint64]string) error {
	f, err := os.Create(filepath.Join(s.dir, "players.csv"))
	if err != nil {
		return fmt.Errorf("create players.csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"steamid", "name"}); err != nil {
		return fmt.Errorf("write players header: %w", err)
	}

	// Go map iteration order is randomized, so writing directly from names
	// would make players.csv non-reproducible across runs of the same demo.
	ids := make([]uint64, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		if err := w.Write([]string{strconv.FormatUint(id, 10), names[id]}); err != nil {
			return fmt.Errorf("write player %d: %w", id, err)
		}
	}
	w.Flush()
	return w.Error()
}
