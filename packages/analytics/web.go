package analytics

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// webVersion is the shape of analytics.json. The web app refuses a version it
// does not know rather than rendering wrong numbers from a renamed field.
// 2 replaced the per-player K/D/A totals and the weapons table with the kills
// themselves, so the page can re-sum them for one side.
const webVersion = 2

// defaultTickRate stands in when a demo never reported one. It only places
// the "watch this moment" links, so a wrong guess lands a replay a little
// early or late - it changes no statistic.
const defaultTickRate = 64

// GamePayload is one demo's analytics as the web app reads it.
//
// It is facts rather than finished tables: the page filters by side, by
// player and by plant, and every one of those is a different grouping of the
// same rows. Sending the rows keeps that choice in the browser.
//
// Teams are indexed 0 and 1 everywhere, 0 being whoever started on T - the
// same order movement.json uses, so the two payloads agree on which team is
// which. Steamids are strings because they exceed what a JSON number holds.
type GamePayload struct {
	Version         int    `json:"version"`
	Demo            string `json:"demo"`
	Map             string `json:"map"`
	RoundsUncertain int    `json:"rounds_uncertain"`

	Teams      [2]string       `json:"teams"`
	Players    []GamePlayer    `json:"players"`
	Rounds     []GameRound     `json:"rounds"`
	Kills      []GameKill      `json:"kills"`
	Openings   []GameOpening   `json:"openings"`
	Clutches   []GameClutch    `json:"clutches"`
	Situations []GameSituation `json:"situations"`
}

type GamePlayer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Team   int    `json:"team"`
	Rounds int    `json:"rounds"`
}

// GameRound is a decided round. T is the team index that played T side and
// Winner the team index that won, so a fact's side and outcome are both one
// lookup away and are not repeated on every row.
type GameRound struct {
	N      int32 `json:"n"`
	T      int   `json:"t"`
	Winner int   `json:"winner"`
}

// GameKill is one death. Team is the VICTIM's; a credited killer or assister
// is always on the other one. Killer is empty for a death nobody is credited
// with (the world, a suicide, a team kill) and Assister when nobody helped, so
// K, D and A are each a count of rows naming the player.
type GameKill struct {
	Round    int32   `json:"round"`
	At       float64 `json:"at"`
	Team     int     `json:"team"`
	Victim   string  `json:"victim"`
	Killer   string  `json:"killer"`
	Assister string  `json:"assister"`
	Weapon   string  `json:"weapon"`
	Headshot bool    `json:"headshot"`
	Planted  bool    `json:"planted"`
	// Post marks a death after the round was won. Added without a version
	// bump: an older file simply has none marked.
	Post bool `json:"post"`
}

// At is seconds into the round's live play on every fact - the clock the
// viewer's round-relative seek takes.
type GameOpening struct {
	Round    int32   `json:"round"`
	At       float64 `json:"at"`
	Team     int     `json:"team"` // the killer's
	Killer   string  `json:"killer"`
	Victim   string  `json:"victim"`
	Weapon   string  `json:"weapon"`
	Headshot bool    `json:"headshot"`
}

type GameClutch struct {
	Round    int32    `json:"round"`
	At       float64  `json:"at"`
	Team     int      `json:"team"`
	Player   string   `json:"player"`
	Enemies  []string `json:"enemies"`
	Planted  bool     `json:"planted"`
	Kills    int      `json:"kills"`
	Survived bool     `json:"survived"`
}

// GameSituation is one player count, once - not once per team as in
// situations.csv. Alive[i] is who team i had up.
type GameSituation struct {
	Round   int32       `json:"round"`
	At      float64     `json:"at"`
	Alive   [2][]string `json:"alive"`
	Planted bool        `json:"planted"`
}

// Game builds one demo's web payload. defs name its teams the way they do in a
// report; a team nobody named is "Team 1" or "Team 2", matching the replay.
func Game(m *Match, defs []TeamDef) GamePayload {
	r := NewReport()
	r.Add(m, defs)
	keys := r.Demos[0].Teams
	index := map[string]int{keys[0]: 0, keys[1]: 1}

	rate := m.TickRate
	if rate <= 0 {
		rate = defaultTickRate
	}
	freezeEnd := make(map[int32]int32, len(m.Rounds))
	for _, rd := range m.Rounds {
		freezeEnd[rd.Meta.Number] = rd.Meta.FreezeEndTick
	}
	at := func(round, tick int32) float64 {
		sec := float64(tick-freezeEnd[round]) / rate
		return float64(int(sec*10+0.5)) / 10
	}
	ids := func(in []uint64) []string {
		out := make([]string, len(in))
		for i, id := range in {
			out[i] = idStr(id)
		}
		return out
	}

	p := GamePayload{
		Version: webVersion, Demo: m.Name, Map: m.Map,
		RoundsUncertain: r.Demos[0].RoundsUncertain,
		Teams:           [2]string{"Team 1", "Team 2"},
		// Empty rather than null, so the page can iterate without checking.
		Players: []GamePlayer{}, Rounds: []GameRound{}, Kills: []GameKill{},
		Openings: []GameOpening{}, Clutches: []GameClutch{}, Situations: []GameSituation{},
	}
	for _, def := range defs {
		if i, ok := index[def.Name]; ok {
			p.Teams[i] = def.Name
		}
	}

	split := splitTeams(m)
	for i, rd := range m.Rounds {
		if winner, ok := sideIndex(rd.Meta.Winner); ok {
			p.Rounds = append(p.Rounds, GameRound{
				N: rd.Meta.Number, T: split.teamOn(i, sideT), Winner: split.teamOn(i, winner),
			})
		}
	}

	for _, key := range sortedKeys(r.players, func(a, b playerKey) int {
		return cmp.Or(cmp.Compare(index[a.team], index[b.team]),
			cmp.Compare(r.players[b].kills, r.players[a].kills), cmp.Compare(a.id, b.id))
	}) {
		t := r.players[key]
		p.Players = append(p.Players, GamePlayer{
			ID: idStr(key.id), Name: r.name(key.id), Team: index[key.team], Rounds: t.rounds,
		})
	}
	plant := make(map[int32]int32, len(m.Rounds))
	for _, rd := range m.Rounds {
		plant[rd.Meta.Number] = plantTick(rd.Bomb)
	}
	optional := func(id uint64) string {
		if id == 0 {
			return ""
		}
		return idStr(id)
	}
	for _, d := range r.Deaths {
		p.Kills = append(p.Kills, GameKill{
			Round: d.Round, At: at(d.Round, d.Tick), Team: index[d.VictimTeam],
			Victim: idStr(d.Victim), Killer: optional(d.Killer), Assister: optional(d.Assister),
			Weapon: d.Weapon, Headshot: d.Headshot, Post: d.Postround,
			Planted: plant[d.Round] > 0 && plant[d.Round] <= d.Tick,
		})
	}

	for _, o := range r.Openings {
		p.Openings = append(p.Openings, GameOpening{
			Round: o.Round, At: at(o.Round, o.Tick), Team: index[o.KillerTeam],
			Killer: idStr(o.Killer), Victim: idStr(o.Victim), Weapon: o.Weapon, Headshot: o.Headshot,
		})
	}
	for _, c := range r.Clutches {
		p.Clutches = append(p.Clutches, GameClutch{
			Round: c.Round, At: at(c.Round, c.Tick), Team: index[c.Team], Player: idStr(c.Player),
			Enemies: ids(c.Enemies), Planted: c.Planted, Kills: c.Kills, Survived: c.Survived,
		})
	}
	for _, s := range r.Situations {
		// The report holds each count from both teams' side; one is enough
		// when the row names both rosters.
		if index[s.Team] != 0 {
			continue
		}
		p.Situations = append(p.Situations, GameSituation{
			Round: s.Round, At: at(s.Round, s.Tick),
			Alive: [2][]string{ids(s.Own), ids(s.Enemy)}, Planted: s.Planted,
		})
	}
	return p
}

// WriteGame writes <root>/<demo>/analytics.json, beside the movement.json the
// viewer already fetches from that folder.
func WriteGame(root string, m *Match, defs []TeamDef) error {
	dir := filepath.Join(root, m.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	raw, err := json.Marshal(Game(m, defs))
	if err != nil {
		return fmt.Errorf("marshal analytics for %s: %w", m.Name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "analytics.json"), raw, 0o644); err != nil {
		return fmt.Errorf("write analytics for %s: %w", m.Name, err)
	}
	return nil
}
