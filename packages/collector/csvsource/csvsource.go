// Package csvsource reads a round-collector output folder back into the
// collector's own Rounds - the reverse of csvsink.
//
// Anything written as a collector.Sink can then run over a demo that was
// parsed earlier, without parsing it again: Run feeds the folder's rounds to
// the sink one at a time, exactly as the collector would have. The tick table
// is streamed, never loaded whole, so a match's 1M+ tick rows cost one round's
// worth of memory.
//
// The values are what the CSVs hold, which is not quite what the parser saw:
// csvsink writes every float to 2 decimals. A consumer that must give the same
// answer from a folder as from the .dem quantizes its inputs the same way (see
// the payload package).
package csvsource

import (
	"bufio"
	"compress/gzip"
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

// schemaMajor is the collector output this reader understands. The check is on
// the major part only, so a 1.x that appends columns keeps loading.
const schemaMajor = "1"

// Manifest is the part of manifest.json a reader needs.
type Manifest struct {
	SchemaVersion string  `json:"schema_version"`
	DemoFile      string  `json:"demo_file"`
	Map           string  `json:"map"`
	TickRate      float64 `json:"tick_rate"`
	Complete      bool    `json:"complete"`
}

// Folder is one parsed demo on disk.
type Folder struct {
	Dir      string
	Manifest Manifest
	// Names is players.csv: steamid -> last-seen name.
	Names map[uint64]string
}

// Open reads a folder's manifest and player names, refusing output from a
// collector whose schema this does not understand. Failing here beats a
// plausible-looking number computed from a column that changed meaning.
func Open(dir string) (*Folder, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%s is not a round-collector output folder: %w", dir, err)
	}
	f := &Folder{Dir: dir, Names: make(map[uint64]string)}
	if err := json.Unmarshal(raw, &f.Manifest); err != nil {
		return nil, fmt.Errorf("%s: read manifest: %w", dir, err)
	}
	if major, _, _ := strings.Cut(f.Manifest.SchemaVersion, "."); major != schemaMajor {
		return nil, fmt.Errorf("%s is schema %q; this reads %s.x", dir, f.Manifest.SchemaVersion, schemaMajor)
	}
	err = eachRow(filepath.Join(dir, "players.csv"), func(r *row) {
		f.Names[r.u64("steamid")] = r.str("name")
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Run feeds every round in the folder to sink, in rounds.csv order, and closes
// it. Each Round carries all of its tables, as the collector delivers them.
func (f *Folder) Run(sink collector.Sink) error {
	rounds, err := f.sideTables()
	if err != nil {
		return err
	}

	ticks, err := openTicks(filepath.Join(f.Dir, "ticks.csv.gz"))
	if err != nil {
		return err
	}
	defer ticks.Close()

	for _, rd := range rounds {
		// ticks.csv.gz is written one round at a time, in round order, so a
		// round's rows are one contiguous run: read until the number changes.
		for {
			t, ok, err := ticks.peek()
			if err != nil {
				return err
			}
			if !ok || t.Round != rd.Meta.Number {
				break
			}
			rd.Ticks = append(rd.Ticks, t)
			ticks.next()
		}
		if err := sink.Round(rd); err != nil {
			return err
		}
		rd.Ticks = nil // the sink has had its look; the next round needs the memory
	}
	if t, ok, err := ticks.peek(); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("%s: ticks for round %d, which rounds.csv does not list (or not in round order)", f.Dir, t.Round)
	}
	return sink.Close()
}

// sideTables reads everything but the ticks: the small tables, grouped by
// round. Tens of thousands of rows at most, so holding them is cheap.
func (f *Folder) sideTables() ([]*collector.Round, error) {
	var rounds []*collector.Round
	// By number rather than by position: a -max-rounds run, or a demo recorded
	// from mid-match, need not start at round 1.
	byNumber := make(map[int32]*collector.Round)
	path := func(name string) string { return filepath.Join(f.Dir, name) }

	err := eachRow(path("rounds.csv"), func(r *row) {
		rd := &collector.Round{Meta: collector.RoundMeta{
			Number:          r.i32("number"),
			StartTick:       r.i32("start_tick"),
			FreezeEndTick:   r.i32("freeze_end_tick"),
			EndTick:         r.i32("end_tick"),
			OfficialEndTick: r.i32("official_end_tick"),
			Winner:          common.Team(r.i32("winner")),
			Reason:          events.RoundEndReason(r.i32("reason")),
			TimeoutBefore:   r.flag("timeout_before"),
			TimeoutTeam:     common.Team(r.i32("timeout_team")),
			Complete:        r.flag("complete"),
		}}
		byNumber[rd.Meta.Number] = rd
		rounds = append(rounds, rd)
	})
	if err != nil {
		return nil, err
	}
	round := func(r *row) *collector.Round {
		if rd, ok := byNumber[r.i32("round")]; ok {
			return rd
		}
		r.fail(fmt.Errorf("round %d is not in rounds.csv", r.i32("round")))
		return nil
	}

	tables := []struct {
		name string
		read func(*row, *collector.Round)
	}{
		{"round_players.csv", func(r *row, rd *collector.Round) {
			rd.Meta.Players = append(rd.Meta.Players, collector.PlayerRound{
				SteamID:               r.u64("steamid"),
				Team:                  common.Team(r.i32("team")),
				MoneyAtFreezeEnd:      r.i32("money_at_freeze_end"),
				EquipValueAtFreezeEnd: r.i32("equip_value_at_freeze_end"),
				Survived:              r.flag("survived"),
				Disconnected:          r.flag("disconnected"),
				JoinedLate:            r.flag("joined_late"),
			})
		}},
		{"kills.csv", func(r *row, rd *collector.Round) {
			rd.Kills = append(rd.Kills, collector.Kill{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				KillerSteamID:   r.u64("killer_steamid"),
				KillerTeam:      common.Team(r.i32("killer_team")),
				VictimSteamID:   r.u64("victim_steamid"),
				VictimTeam:      common.Team(r.i32("victim_team")),
				AssisterSteamID: r.u64("assister_steamid"),
				AssistedFlash:   r.flag("assisted_flash"),
				Weapon:          r.str("weapon"),
				Headshot:        r.flag("headshot"),
				Penetrated:      int16(r.i32("penetrated")),
				NoScope:         r.flag("no_scope"),
				ThroughSmoke:    r.flag("through_smoke"),
				AttackerBlind:   r.flag("attacker_blind"),
				Distance:        r.f32("distance"),
			})
		}},
		{"utility.csv", func(r *row, rd *collector.Round) {
			rd.Utility = append(rd.Utility, collector.Utility{
				Round: rd.Meta.Number, StartTick: r.i32("start_tick"), EndTick: r.i32("end_tick"),
				Phase:          r.phase("phase"),
				Kind:           r.str("kind"),
				ThrowerSteamID: r.u64("thrower_steamid"),
				ThrowerTeam:    common.Team(r.i32("thrower_team")),
				X:              r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
				Radius: r.f32("radius"),
			})
		}},
		{"shots.csv", func(r *row, rd *collector.Round) {
			rd.Shots = append(rd.Shots, collector.Shot{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				SteamID: r.u64("steamid"),
				Team:    common.Team(r.i32("team")),
				Weapon:  r.str("weapon"),
				X:       r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
				Yaw: r.f32("yaw"), Pitch: r.f32("pitch"),
			})
		}},
		{"damage.csv", func(r *row, rd *collector.Round) {
			rd.Damage = append(rd.Damage, collector.Damage{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				VictimSteamID:   r.u64("victim_steamid"),
				VictimTeam:      common.Team(r.i32("victim_team")),
				AttackerSteamID: r.u64("attacker_steamid"),
				AttackerTeam:    common.Team(r.i32("attacker_team")),
				Weapon:          r.str("weapon"),
				Kind:            r.str("kind"),
				HealthDamage:    int16(r.i32("health_damage")),
				ArmorDamage:     int16(r.i32("armor_damage")),
				HealthRemaining: int16(r.i32("health_remaining")),
				ArmorRemaining:  int16(r.i32("armor_remaining")),
				HitGroup:        uint8(r.i32("hitgroup")),
				X:               r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
			})
		}},
		{"bomb.csv", func(r *row, rd *collector.Round) {
			rd.Bomb = append(rd.Bomb, collector.BombSample{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				State:          r.str("state"),
				CarrierSteamID: r.u64("carrier_steamid"),
				CarrierTeam:    common.Team(r.i32("carrier_team")),
				Site:           r.str("site"),
				X:              r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
			})
		}},
		{"kits.csv", func(r *row, rd *collector.Round) {
			rd.Kits = append(rd.Kits, collector.KitEvent{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				Event:   r.str("event"),
				KitID:   r.i32("kit_id"),
				SteamID: r.u64("steamid"),
				Team:    common.Team(r.i32("team")),
				X:       r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
			})
		}},
		{"trajectories.csv", func(r *row, rd *collector.Round) {
			rd.Trajectories = append(rd.Trajectories, collector.TrajectoryPoint{
				Round: rd.Meta.Number, Tick: r.i32("tick"), Phase: r.phase("phase"),
				ProjectileID:   r.i32("projectile_id"),
				Seq:            int16(r.i32("seq")),
				Kind:           r.str("kind"),
				ThrowerSteamID: r.u64("thrower_steamid"),
				ThrowerTeam:    common.Team(r.i32("thrower_team")),
				X:              r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
			})
		}},
	}
	for _, t := range tables {
		err := eachRow(path(t.name), func(r *row) {
			if rd := round(r); rd != nil {
				t.read(r, rd)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return rounds, nil
}

// tickReader streams ticks.csv.gz with one row of lookahead, which is what
// lets Run stop at a round boundary without consuming the next round's first
// row.
type tickReader struct {
	file   *os.File
	gz     *gzip.Reader
	rows   *csv.Reader
	cols   map[string]int
	path   string
	line   int
	buf    collector.PlayerTick
	peeked bool
	done   bool
}

func openTicks(path string) (*tickReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bufio.NewReaderSize(file, 1<<20))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rows := csv.NewReader(gz)
	rows.ReuseRecord = true
	header, err := rows.Read()
	if err != nil {
		gz.Close()
		file.Close()
		return nil, fmt.Errorf("%s: read header: %w", path, err)
	}
	cols := make(map[string]int, len(header))
	for i, name := range header {
		cols[name] = i
	}
	return &tickReader{file: file, gz: gz, rows: rows, cols: cols, path: path, line: 1}, nil
}

func (t *tickReader) Close() error {
	t.gz.Close()
	return t.file.Close()
}

func (t *tickReader) next() { t.peeked = false }

func (t *tickReader) peek() (collector.PlayerTick, bool, error) {
	if t.peeked {
		return t.buf, true, nil
	}
	if t.done {
		return collector.PlayerTick{}, false, nil
	}
	rec, err := t.rows.Read()
	if errors.Is(err, io.EOF) {
		t.done = true
		return collector.PlayerTick{}, false, nil
	}
	t.line++
	if err != nil {
		return collector.PlayerTick{}, false, fmt.Errorf("%s: %w", t.path, err)
	}
	r := &row{cols: t.cols, rec: rec}
	t.buf = collector.PlayerTick{
		Round:   r.i32("round"),
		Tick:    r.i32("tick"),
		Phase:   r.phase("phase"),
		SteamID: r.u64("steamid"),
		Team:    common.Team(r.i32("team")),
		X:       r.f32("x"), Y: r.f32("y"), Z: r.f32("z"),
		Yaw: r.f32("yaw"), Pitch: r.f32("pitch"),
		VelX: r.f32("vel_x"), VelY: r.f32("vel_y"), VelZ: r.f32("vel_z"),
		Speed:           r.f32("speed"),
		AccuracyPenalty: r.f32("accuracy_penalty"),
		VelValid:        r.flag("vel_valid"),
		Buttons:         r.u64("buttons"),
		ShotsFired:      int16(r.i32("shots_fired")),
		PunchYaw:        r.f32("punch_yaw"), PunchPitch: r.f32("punch_pitch"),
		IsDucking: r.flag("is_ducking"), IsWalking: r.flag("is_walking"),
		IsAirborne: r.flag("is_airborne"), IsScoped: r.flag("is_scoped"),
		Health:         int16(r.i32("health")),
		Armor:          int16(r.i32("armor")),
		IsAlive:        r.flag("is_alive"),
		FlashRemaining: r.f32("flash_remaining"),
		ActiveWeapon:   r.str("active_weapon"),
		Place:          r.str("place"),
		Money:          r.i32("money"),
		Loadout: collector.Loadout{
			HasHelmet: r.flag("has_helmet"), HasKit: r.flag("has_kit"), HasBomb: r.flag("has_bomb"),
			Primary: r.str("primary"), Secondary: r.str("secondary"), Nades: r.str("nades"),
		},
	}
	if r.err != nil {
		return collector.PlayerTick{}, false, fmt.Errorf("%s line %d: %w", t.path, t.line, r.err)
	}
	t.peeked = true
	return t.buf, true, nil
}

// row is one CSV record addressed by column name. The schema's rule is that
// columns are only ever appended, so reading by name keeps this working across
// minor versions where reading by position would silently shift.
//
// The typed getters record the first failure instead of returning it, so a
// table reads as a struct literal rather than as a ladder of error checks.
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

func (r *row) f32(col string) float32 {
	v, err := strconv.ParseFloat(r.str(col), 32)
	if err != nil {
		r.fail(fmt.Errorf("column %q: %w", col, err))
	}
	return float32(v)
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

	rd := csv.NewReader(bufio.NewReader(f))
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
