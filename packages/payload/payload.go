// Package payload builds the web viewer's movement.json from a demo's rounds.
//
// It is a collector.Sink: hand it to collector.New and it builds the payload
// as the parser delivers each round, so a .dem becomes a payload in one pass
// with no CSVs in between and one round of ticks in memory at a time. Fed by
// csvsource instead, it rebuilds a payload from a folder parsed earlier. The
// two give byte-for-byte the same numbers: every float is read at the two
// decimals the CSVs carry (see q2).
//
// This is a port of playground/.../movement_payload.py, and its output was
// checked against that script's on every demo to hand before it replaced it
// (golden_test.go). The comments that explain WHY a field is shaped the way
// it is came across with the code; the format is the contract types.ts in the
// web app reads.
//
// Positions are resampled from each round's first live tick. Straight lines are
// drawn between samples, so the rate is a geometry setting as much as a size one:
// at 1 Hz a sprinting player moves ~250 units between samples and the chord cuts
// visibly through corners. 4 Hz keeps 97% of the true path length with a
// worst-case 65-unit gap, where a player is 32 units wide.
//
// Rounds run past their own win condition. CS2 gives survivors a flat seven
// seconds before the next freeze time, and they are not idle seconds: people run
// rifles into corners to save them, pick up what the dead dropped, and get hunted
// down doing it. `dur` remains the length of contested play - the round table and
// every "was this decided yet" question depends on that - and `post` carries the
// tail, so a viewer can play through it while an analysis can still ignore it.
package payload

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// DefaultHz is the resample rate - see the package comment.
const DefaultHz = 4.0

const (
	teamT  = int(common.TeamTerrorists)
	teamCT = int(common.TeamCounterTerrorists)
)

// reasons names the demoinfocs RoundEndReason values seen in this data.
var reasons = map[int]string{1: "bomb detonated", 7: "bomb defused", 8: "CT elimination",
	9: "T elimination", 11: "T surrender", 12: "CT surrender"}

// Payload is movement.json.
type Payload struct {
	Map      string     `json:"map"`
	Radar    RadarJSON  `json:"radar"`
	TickRate float64    `json:"tick_rate"`
	SampleHz float64    `json:"sample_hz"`
	Sections []string   `json:"sections"`
	MaxSec   float64    `json:"max_sec"`
	Demos    []DemoJSON `json:"demos"`

	Rounds *ordered[*RoundJSON]  `json:"rounds"`
	Kills  *ordered[[]KillJSON]  `json:"kills"`
	Util   *ordered[[]UtilJSON]  `json:"util"`
	Shots  *ordered[[]ShotJSON]  `json:"shots"`
	Damage *ordered[[]HitJSON]   `json:"damage"`
	Bomb   *ordered[[]BombJSON]  `json:"bomb"`
	Kits   *ordered[[]KitJSON]   `json:"kits"`
	Traj   *ordered[[]ThrowJSON] `json:"traj"`

	Teams   []TeamJSON          `json:"teams"`
	Stats   *ordered[*StatLine] `json:"stats"`
	Players []*PlayerJSON       `json:"players"`
}

type RadarJSON struct {
	PosX  float64 `json:"pos_x"`
	PosY  float64 `json:"pos_y"`
	Scale float64 `json:"scale"`
	Size  int     `json:"size"`
}

type DemoJSON struct {
	I      int    `json:"i"`
	Label  string `json:"label"`
	Rounds int    `json:"rounds"`
}

// RoundJSON is one round's facts. Sa/Sb are the running score of the two
// rosters (A is round 1's T side), which survives the halftime side swap where
// counting T and CT wins would not.
type RoundJSON struct {
	N    int     `json:"n"`
	Dur  float64 `json:"dur"`
	W    int     `json:"w"`
	Why  string  `json:"why"`
	Sa   int     `json:"sa"`
	Sb   int     `json:"sb"`
	Aw   int     `json:"aw"`
	Ok   int     `json:"ok"`
	Post float64 `json:"post"`
	Dm   int     `json:"dm"`
	T0   float64 `json:"t0"`
}

type KillJSON struct {
	T  float64 `json:"t"`
	K  *string `json:"k"`
	V  string  `json:"v"`
	A  *string `json:"a"`
	W  string  `json:"w"`
	Hs int     `json:"hs"`
	Wb int     `json:"wb"`
	// Post is seconds after the win condition, on a postround kill only.
	Post *float64 `json:"post,omitempty"`
}

type UtilJSON struct {
	T    float64 `json:"t"`
	T1   float64 `json:"t1"`
	Kind string  `json:"kind"`
	By   *string `json:"by"`
	Team int     `json:"team"`
	X    int     `json:"x"`
	Y    int     `json:"y"`
	R    *int    `json:"r"`
}

type ShotJSON struct {
	T  float64 `json:"t"`
	By string  `json:"by"`
	X  int     `json:"x"`
	Y  int     `json:"y"`
	A  int     `json:"a"`
	Lv int     `json:"lv"`
	Hx *int    `json:"hx,omitempty"`
	Hy *int    `json:"hy,omitempty"`
}

type HitJSON struct {
	T   float64 `json:"t"`
	V   string  `json:"v"`
	By  *string `json:"by"`
	K   string  `json:"k"`
	Hp  int     `json:"hp"`
	Hpt int     `json:"hpt"`
	Ar  int     `json:"ar"`
	Hg  int     `json:"hg"`
	N   int     `json:"n"`
	X   int     `json:"x"`
	Y   int     `json:"y"`
	Lv  int     `json:"lv"`
}

type BombJSON struct {
	T    float64 `json:"t"`
	St   string  `json:"st"`
	By   *string `json:"by"`
	X    int     `json:"x"`
	Y    int     `json:"y"`
	Lv   int     `json:"lv"`
	Site string  `json:"site,omitempty"`
}

type KitJSON struct {
	T  float64 `json:"t"`
	Ev string  `json:"ev"`
	ID int     `json:"id"`
	By string  `json:"by"`
	X  int     `json:"x"`
	Y  int     `json:"y"`
	Lv int     `json:"lv"`
}

type ThrowJSON struct {
	T    float64 `json:"t"`
	T1   float64 `json:"t1"`
	Kind string  `json:"kind"`
	By   *string `json:"by"`
	Team int     `json:"team"`
	X    []int   `json:"x"`
	Y    []int   `json:"y"`
	Ts   []int   `json:"ts"`
	Lvl  string  `json:"lvl"`
}

type TeamJSON struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
}

// StatLine is a player's kills, deaths and assists over the demo.
type StatLine struct {
	K int `json:"k"`
	D int `json:"d"`
	A int `json:"a"`
}

type PlayerJSON struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Rounds []*TrackJSON `json:"rounds"`
	// Col and Side0 are set for everyone whose first round was on T or CT.
	Col   *int `json:"col,omitempty"`
	Side0 *int `json:"side0,omitempty"`
	Team  int  `json:"team"`
}

// TrackJSON is one player's movement through one round.
type TrackJSON struct {
	R   int     `json:"r"`
	S   int     `json:"s"`
	D   int     `json:"d"`
	Sv  int     `json:"sv"`
	X   []int   `json:"x"`
	Y   []int   `json:"y"`
	Air string  `json:"air"`
	Sc  string  `json:"sc"`
	Yaw []int   `json:"yaw"`
	Lvl string  `json:"lvl"`
	Eq  [][]any `json:"eq"`
	Dm  int     `json:"dm"`
	K   string  `json:"k"`
}

// Builder accumulates a payload round by round. It implements collector.Sink.
type Builder struct {
	// Label names the demo in the payload's demo list: its folder name.
	Label    string
	SampleHz float64
	// RadarDir holds the overview .txt/.png pairs (assets/radar).
	RadarDir string

	mapName  string
	tickRate float64
	radar    *Radar

	rosterA   map[uint64]bool
	rosterSet bool
	scoreA    int
	scoreB    int

	p          *Payload
	freezeEnds map[string]int32
	players    map[uint64]*PlayerJSON
	maxSamples int
}

// NewBuilder starts a payload for one demo.
func NewBuilder(label, radarDir string, sampleHz float64) *Builder {
	return &Builder{
		Label: label, RadarDir: radarDir, SampleHz: sampleHz,
		freezeEnds: map[string]int32{},
		players:    map[uint64]*PlayerJSON{},
		p: &Payload{
			Rounds: newOrdered[*RoundJSON](),
			Kills:  newOrdered[[]KillJSON](),
			Util:   newOrdered[[]UtilJSON](),
			Shots:  newOrdered[[]ShotJSON](),
			Damage: newOrdered[[]HitJSON](),
			Bomb:   newOrdered[[]BombJSON](),
			Kits:   newOrdered[[]KitJSON](),
			Traj:   newOrdered[[]ThrowJSON](),
			Stats:  newOrdered[*StatLine](),
		},
	}
}

// SetMap and SetTickRate must be called before the first round arrives. The
// parser reports both from net messages that precede any round.
func (b *Builder) SetMap(name string)       { b.mapName = name }
func (b *Builder) SetTickRate(rate float64) { b.tickRate = rate }

// Round implements collector.Sink.
func (b *Builder) Round(r *collector.Round) error {
	if b.tickRate <= 0 {
		return errors.New("payload: no tick rate before the first round")
	}
	if b.radar == nil {
		if b.mapName == "" {
			return errors.New("payload: no map name before the first round")
		}
		radar, err := LoadRadar(b.RadarDir, b.mapName)
		if err != nil {
			return err
		}
		b.radar = radar
	}

	m := r.Meta
	// Sides switch at halftime, so counting T wins against CT wins would
	// describe two groups that each contain everybody. Anchor on the first
	// round's rosters instead and ask, each round, which side that roster is
	// on now.
	if !b.rosterSet {
		b.rosterA = map[uint64]bool{}
		for _, p := range m.Players {
			if int(p.Team) == teamT {
				b.rosterA[p.SteamID] = true
			}
		}
		b.rosterSet = true
	}
	sideA := b.sideOf(m.Players)
	winner := int(m.Winner)
	// A round with no winner - the recording stopped before anyone won it -
	// scores for nobody. The Python gave it to roster B; no demo to hand has
	// one (recordings stop in the seven-second tail, after the winner is
	// known), so this differs only where the Python was wrong.
	switch {
	case winner == sideA:
		b.scoreA++
	case winner == teamT || winner == teamCT:
		b.scoreB++
	}

	why, ok := reasons[int(m.Reason)]
	if !ok {
		why = fmt.Sprintf("reason %d", int(m.Reason))
	}
	tr := b.tickRate
	key := fmt.Sprintf("0:%d", m.Number)
	meta := &RoundJSON{
		N:   int(m.Number),
		Dur: round1(float64(m.EndTick-m.FreezeEndTick) / tr),
		W:   winner, Why: why,
		Sa: b.scoreA, Sb: b.scoreB,
		Aw: boolInt(winner == sideA),
		Ok: boolInt(m.Complete),
	}

	// The tracks cover live play and the postround save window. Freeze time
	// is dropped: position is constant there and the clock starts at freeze
	// end. Dead rows stay - they are what says a player died this round.
	var ticks []collector.PlayerTick
	lastTick, seen := int32(0), false
	for _, t := range r.Ticks {
		if t.Phase == collector.PhaseLive || t.Phase == collector.PhasePostRound {
			ticks = append(ticks, t)
			if !seen || t.Tick > lastTick {
				lastTick, seen = t.Tick, true
			}
		}
	}

	// The tail after the win condition. Normally RoundEndOfficial minus
	// RoundEnd, a flat 7.00s. On a truncated round official_end_tick is 0:
	// "unknown", not "zero", so the tail runs to the last tick actually
	// recorded - a viewer can only play what was recorded, and assuming the
	// usual seven seconds would invent a window the demo never captured.
	official := m.OfficialEndTick
	if official == 0 {
		official = m.EndTick
		if seen {
			official = lastTick
		}
	}
	meta.Post = round1(max(0, float64(official-m.EndTick)/tr))
	b.p.Rounds.set(key, meta)
	b.freezeEnds[key] = m.FreezeEndTick

	span := meta.Dur + meta.Post
	ev := events{base: m.FreezeEndTick, end: m.EndTick, tickRate: tr, span: span, level: b.level}
	b.p.Kills.set(key, ev.kills(r.Kills))
	b.p.Util.set(key, ev.utility(r.Utility))
	b.p.Shots.set(key, ev.shots(r.Shots, r.Damage))
	b.p.Damage.set(key, ev.damage(r.Damage))
	b.p.Bomb.set(key, ev.bomb(r.Bomb))
	b.p.Kits.set(key, ev.kits(r.Kits))
	b.p.Traj.set(key, ev.throws(r.Trajectories))
	b.addStats(r.Kills)

	survived := map[uint64]bool{}
	for _, p := range m.Players {
		survived[p.SteamID] = p.Survived
	}
	step := tr / b.SampleHz
	for id, rows := range byPlayer(ticks) {
		track := sampleTrack(rows, step, b.level)
		if track == nil {
			continue // joined late, or never alive this round
		}
		track.R = int(m.Number)
		track.Sv = boolInt(survived[id])
		track.Dm = 0
		track.K = key
		pl := b.players[id]
		if pl == nil {
			pl = &PlayerJSON{ID: strconv.FormatUint(id, 10)}
			b.players[id] = pl
		}
		pl.Rounds = append(pl.Rounds, track)
		b.maxSamples = max(b.maxSamples, len(track.X))
	}
	return nil
}

// Close implements collector.Sink. The payload is read with Finish.
func (b *Builder) Close() error { return nil }

// sideOf is the side roster A is on this round, by majority vote so that a
// disconnect or a late join does not flip the whole scoreboard. A tie goes to
// the lower team number, as pandas' mode() did; nobody from roster A present
// reads as T.
func (b *Builder) sideOf(players []collector.PlayerRound) int {
	count := map[int]int{}
	for _, p := range players {
		if b.rosterA[p.SteamID] {
			count[int(p.Team)]++
		}
	}
	if len(count) == 0 {
		return teamT
	}
	best, bestN := 0, -1
	for team, n := range count {
		if n > bestN || (n == bestN && team < best) {
			best, bestN = team, n
		}
	}
	return best
}

func (b *Builder) level(z float64) int { return b.radar.Level(z) }

// addStats counts kills, deaths and assists, live and postround, matching
// the game's scoreboard: a player hunted down while saving is a death, and
// the kill is whoever got them. Freeze-phase deaths are disconnects and
// suicides, which no scoreboard counts.
//
// A suicide or a teamkill is a death for the victim but never a kill for the
// "killer". Crediting it put a post-round bomb death into m0NESY's kills on
// one FACEIT pug (32, where the kill feed shows 31 enemies). FACEIT and HLTV
// leave it out, and so does this.
func (b *Builder) addStats(kills []collector.Kill) {
	var counted []collector.Kill
	for _, k := range kills {
		if k.Phase == collector.PhaseLive || k.Phase == collector.PhasePostRound {
			counted = append(counted, k)
		}
	}
	bump := func(id uint64, field byte) {
		if id == 0 {
			return
		}
		key := strconv.FormatUint(id, 10)
		line, ok := b.p.Stats.get(key)
		if !ok {
			line = &StatLine{}
			b.p.Stats.set(key, line)
		}
		switch field {
		case 'k':
			line.K++
		case 'd':
			line.D++
		case 'a':
			line.A++
		}
	}
	for _, k := range dedupeKills(counted) {
		if k.KillerSteamID != k.VictimSteamID && k.KillerTeam != k.VictimTeam {
			bump(k.KillerSteamID, 'k')
			bump(k.AssisterSteamID, 'a')
		}
		bump(k.VictimSteamID, 'd')
	}
}

// Finish completes the payload once every round is in. names is the
// collector's steamid -> name map; anyone missing from it is labelled by
// steamid.
func (b *Builder) Finish(names map[uint64]string) (*Payload, error) {
	if len(b.p.Rounds.keys) == 0 {
		return nil, errors.New("payload: the demo has no rounds")
	}
	p := b.p
	p.Map = b.mapName
	p.Radar = RadarJSON{PosX: b.radar.PosX, PosY: b.radar.PosY, Scale: b.radar.Scale, Size: b.radar.Width}
	p.TickRate = b.tickRate
	p.SampleHz = b.SampleHz
	p.Sections = b.radar.Sections
	p.MaxSec = round1(float64(b.maxSamples-1) / b.SampleHz)
	p.Demos = []DemoJSON{{I: 0, Label: b.Label, Rounds: len(p.Rounds.keys)}}

	// Seconds from the first live round, so the viewer can run the match as
	// one timeline with rounds as chapters.
	origin := int32(0)
	for i, k := range p.Rounds.keys {
		if fe := b.freezeEnds[k]; i == 0 || fe < origin {
			origin = fe
		}
	}
	for _, k := range p.Rounds.keys {
		p.Rounds.values[k].T0 = round1(float64(b.freezeEnds[k]-origin) / b.tickRate)
	}

	// Most rounds first, then by name. Ties on both fall back to steamid
	// order, which is the order the Python met players in.
	ids := make([]uint64, 0, len(b.players))
	for id := range b.players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ordered := make([]*PlayerJSON, 0, len(ids))
	for _, id := range ids {
		pl := b.players[id]
		if name, ok := names[id]; ok {
			pl.Name = name
		} else {
			pl.Name = pl.ID
		}
		ordered = append(ordered, pl)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, c := ordered[i], ordered[j]
		if len(a.Rounds) != len(c.Rounds) {
			return len(a.Rounds) > len(c.Rounds)
		}
		return strings.ToLower(a.Name) < strings.ToLower(c.Name)
	})
	assignColors(ordered)

	// Teams by roster, not side, so a player stays on one team across the
	// halftime swap.
	teams := []TeamJSON{{ID: 0, Name: "Team 1", Players: []string{}}, {ID: 1, Name: "Team 2", Players: []string{}}}
	for _, pl := range ordered {
		id, _ := strconv.ParseUint(pl.ID, 10, 64)
		if b.rosterA[id] {
			pl.Team = 0
		} else {
			pl.Team = 1
		}
		teams[pl.Team].Players = append(teams[pl.Team].Players, pl.ID)
	}
	p.Teams = teams
	p.Players = ordered
	return p, nil
}

// assignColors gives each player one of the five minimap colour slots within
// their starting side. The real value is in the demo (m_iCompTeammateColor)
// but the collector does not record it yet; this stands in with the same
// shape, so reading the real one later needs no change in the viewer.
func assignColors(players []*PlayerJSON) {
	for _, side := range []int{teamT, teamCT} {
		var members []*PlayerJSON
		for _, p := range players {
			if len(p.Rounds) > 0 && p.Rounds[0].S == side {
				members = append(members, p)
			}
		}
		sort.SliceStable(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		for slot, p := range members {
			col, s := slot%5, side
			p.Col, p.Side0 = &col, &s
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
