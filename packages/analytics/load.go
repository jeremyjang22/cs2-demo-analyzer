package analytics

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// schemaMajor is the collector output this loader understands. The check is on
// the major part only, so a 1.x that appends columns keeps loading.
const schemaMajor = "1"

// IsDemoDir reports whether dir looks like one demo's round-collector output.
// A manifest alone is not enough: this package writes one too, and its own
// output folder usually sits right beside the demos it was built from.
func IsDemoDir(dir string) bool {
	for _, name := range []string{"manifest.json", "rounds.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

// LoadDir reads a Match back out of a round-collector output folder. It opens
// the small tables only - never ticks.csv.gz - so a folder loads in
// milliseconds.
func LoadDir(dir string) (*Match, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%s is not a round-collector output folder: %w", dir, err)
	}
	var man struct {
		SchemaVersion string  `json:"schema_version"`
		Map           string  `json:"map"`
		TickRate      float64 `json:"tick_rate"`
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, fmt.Errorf("%s: read manifest: %w", dir, err)
	}
	if major, _, _ := strings.Cut(man.SchemaVersion, "."); major != schemaMajor {
		return nil, fmt.Errorf("%s is schema %q; this reads %s.x", dir, man.SchemaVersion, schemaMajor)
	}

	m := &Match{
		Name: filepath.Base(filepath.Clean(dir)), Map: man.Map, TickRate: man.TickRate,
		Names: make(map[uint64]string),
	}

	err = eachRow(filepath.Join(dir, "players.csv"), func(r *row) {
		m.Names[r.u64("steamid")] = r.str("name")
	})
	if err != nil {
		return nil, err
	}

	// byNumber finds a round by its number rather than assuming row i is round
	// i+1: a -max-rounds run, or a demo recorded from mid-match, need not
	// start at 1.
	byNumber := make(map[int32]int)
	err = eachRow(filepath.Join(dir, "rounds.csv"), func(r *row) {
		byNumber[r.i32("number")] = len(m.Rounds)
		m.Rounds = append(m.Rounds, Round{Meta: collector.RoundMeta{
			Number:          r.i32("number"),
			StartTick:       r.i32("start_tick"),
			FreezeEndTick:   r.i32("freeze_end_tick"),
			EndTick:         r.i32("end_tick"),
			OfficialEndTick: r.i32("official_end_tick"),
			Winner:          common.Team(r.i32("winner")),
			Reason:          events.RoundEndReason(r.i32("reason")),
			Complete:        r.flag("complete"),
		}})
	})
	if err != nil {
		return nil, err
	}
	round := func(r *row) *Round {
		if i, ok := byNumber[r.i32("round")]; ok {
			return &m.Rounds[i]
		}
		r.fail(fmt.Errorf("round %d is not in rounds.csv", r.i32("round")))
		return nil
	}

	err = eachRow(filepath.Join(dir, "round_players.csv"), func(r *row) {
		if rd := round(r); rd != nil {
			rd.Meta.Players = append(rd.Meta.Players, collector.PlayerRound{
				SteamID:      r.u64("steamid"),
				Team:         common.Team(r.i32("team")),
				Survived:     r.flag("survived"),
				Disconnected: r.flag("disconnected"),
				JoinedLate:   r.flag("joined_late"),
			})
		}
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(filepath.Join(dir, "kills.csv"), func(r *row) {
		if rd := round(r); rd != nil {
			rd.Kills = append(rd.Kills, collector.Kill{
				Round:           rd.Meta.Number,
				Tick:            r.i32("tick"),
				Phase:           r.phase("phase"),
				KillerSteamID:   r.u64("killer_steamid"),
				KillerTeam:      common.Team(r.i32("killer_team")),
				VictimSteamID:   r.u64("victim_steamid"),
				VictimTeam:      common.Team(r.i32("victim_team")),
				AssisterSteamID: r.u64("assister_steamid"),
				AssistedFlash:   r.flag("assisted_flash"),
				Weapon:          r.str("weapon"),
				Headshot:        r.flag("headshot"),
			})
		}
	})
	if err != nil {
		return nil, err
	}

	// bomb.csv is mostly position samples of a loose C4. Only the plant
	// matters here, so the rest is skipped before it is parsed.
	err = eachRow(filepath.Join(dir, "bomb.csv"), func(r *row) {
		if r.str("state") != collector.BombPlanted {
			return
		}
		if rd := round(r); rd != nil {
			rd.Bomb = append(rd.Bomb, collector.BombSample{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				State: collector.BombPlanted, Site: r.str("site"),
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// row is one CSV record addressed by column name. The schema's rule is that
// columns are only ever appended, so reading by name keeps this working across
// minor versions where reading by position would silently shift.
//
// The typed getters record the first failure instead of returning it, so a
// loader reads as a struct literal rather than as a ladder of error checks;
// eachRow reports it with the file and line.
type row struct {
	cols map[string]int
	rec  []string
	err  error
}

func (r *row) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *row) str(col string) string {
	i, ok := r.cols[col]
	if !ok {
		r.fail(fmt.Errorf("no %q column", col))
		return ""
	}
	return r.rec[i]
}

func (r *row) i32(col string) int32 {
	v, err := strconv.ParseInt(r.str(col), 10, 32)
	if err != nil {
		r.fail(fmt.Errorf("column %q: %w", col, err))
	}
	return int32(v)
}

func (r *row) u64(col string) uint64 {
	v, err := strconv.ParseUint(r.str(col), 10, 64)
	if err != nil {
		r.fail(fmt.Errorf("column %q: %w", col, err))
	}
	return v
}

func (r *row) flag(col string) bool { return r.str(col) == "1" }

func (r *row) phase(col string) collector.Phase {
	s := r.str(col)
	for _, p := range []collector.Phase{collector.PhaseFreeze, collector.PhaseLive, collector.PhasePostRound} {
		if p.String() == s {
			return p
		}
	}
	r.fail(fmt.Errorf("column %q: unknown phase %q", col, s))
	return collector.PhaseFreeze
}

func eachRow(path string, fn func(*row)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	rd := csv.NewReader(f)
	header, err := rd.Read()
	if err != nil {
		return fmt.Errorf("%s: read header: %w", path, err)
	}
	r := &row{cols: make(map[string]int, len(header))}
	for i, name := range header {
		r.cols[name] = i
	}
	for line := 2; ; line++ {
		r.rec, err = rd.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if fn(r); r.err != nil {
			return fmt.Errorf("%s line %d: %w", path, line, r.err)
		}
	}
}
