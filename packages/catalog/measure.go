package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
)

// Summary is the MEASURED half of an index entry, read out of a demo's
// movement.json when it is built and stored as demos.summary. The JSON is
// what the database holds; Entry copies it into the index.
//
// It is a port of index-demos.mjs and demo-info.mjs's playSummary, and has to
// stay one: the index Go generates replaces the one Node did, and the web app
// reads the same fields. testdata/ holds a payload and the summary both
// languages must produce from it (TestSummaryMatchesTheJavaScript here,
// "summary matches the Go catalogue" in demo-info.test.mjs).
type Summary struct {
	// More than one means a merged payload; the Pros pages leave those out.
	Games   int    `json:"games"`
	Map     string `json:"map"`
	Rounds  int    `json:"rounds"`
	Players int    `json:"players"`
	// movement.json's size, so someone can see what a demo costs before
	// opening it on a phone.
	KB int `json:"kb"`
	// Live play, every game in the payload added up.
	Seconds int `json:"seconds"`
	// Final score [roster A, roster B] and each player's
	// [kills, deaths, assists, roster, adr]. Absent for a merged payload,
	// where one score means nothing.
	Score *[2]int               `json:"score,omitempty"`
	Lines map[string][5]float64 `json:"lines,omitempty"`
	// Every steamid in the roster. Not in the index: it is what pros are
	// matched against when the index is generated, and a merged payload has
	// no lines to read them from.
	IDs []string `json:"ids"`
}

// Measured is everything read from a payload: the Summary, plus what goes in
// columns of its own.
type Measured struct {
	Summary
	TickRate float64
	Roster   []Player
}

// Player is one roster line, for demo_players.
type Player struct {
	ID      string
	Name    string
	Team    int
	K, D, A int
}

// What Measure reads of a payload. Lenient where the JavaScript is (a missing
// field reads as zero), because payloads from the Python exporter are still
// published.
type payloadFields struct {
	Map      *string           `json:"map"`
	TickRate float64           `json:"tick_rate"`
	Demos    []json.RawMessage `json:"demos"`
	Rounds   map[string]struct {
		N   float64 `json:"n"`
		Dur float64 `json:"dur"`
		T0  float64 `json:"t0"`
		Dm  float64 `json:"dm"`
		Sa  int     `json:"sa"`
		Sb  int     `json:"sb"`
	} `json:"rounds"`
	Damage map[string][]struct {
		V   json.RawMessage `json:"v"`
		By  json.RawMessage `json:"by"`
		Hpt float64         `json:"hpt"`
	} `json:"damage"`
	Stats map[string]*struct {
		K int `json:"k"`
		D int `json:"d"`
		A int `json:"a"`
	} `json:"stats"`
	Players []struct {
		ID   json.RawMessage `json:"id"`
		Name string          `json:"name"`
		Team int             `json:"team"`
	} `json:"players"`
}

// Measure reads a movement.json.
func Measure(movement []byte) (Measured, error) {
	var p payloadFields
	if err := json.Unmarshal(movement, &p); err != nil {
		return Measured{}, fmt.Errorf("movement.json: %w", err)
	}
	m := Measured{TickRate: p.TickRate}
	s := &m.Summary
	s.Games = len(p.Demos)
	if s.Games == 0 {
		s.Games = 1
	}
	s.Map = "unknown"
	if p.Map != nil && *p.Map != "" {
		s.Map = *p.Map
	}
	s.Rounds = len(p.Rounds)
	s.Players = len(p.Players)
	s.KB = int(jsRound(float64(len(movement)) / 1024))

	// Live play: each game's last round end (rounds are timed from their own
	// start), summed over games. Map order in the JavaScript is the order a
	// game's dm is first met walking the rounds by number, which is the order
	// the sum runs in here too.
	keys := roundKeys(p.Rounds)
	perGame := map[float64]float64{}
	var games []float64
	for _, k := range keys {
		r := p.Rounds[k]
		if _, seen := perGame[r.Dm]; !seen {
			games = append(games, r.Dm)
		}
		perGame[r.Dm] = math.Max(perGame[r.Dm], r.T0+r.Dur)
	}
	total := 0.0
	for _, dm := range games {
		total += perGame[dm]
	}
	s.Seconds = int(jsRound(total))

	team := map[string]int{}
	for _, pl := range p.Players {
		id := idString(pl.ID)
		team[id] = pl.Team
		s.IDs = append(s.IDs, id)
		st := p.Stats[id]
		if st == nil {
			st = &struct {
				K int `json:"k"`
				D int `json:"d"`
				A int `json:"a"`
			}{}
		}
		m.Roster = append(m.Roster, Player{ID: id, Name: pl.Name, Team: pl.Team, K: st.K, D: st.D, A: st.A})
	}
	if s.IDs == nil {
		s.IDs = []string{}
	}

	if len(p.Demos) > 1 || len(p.Rounds) == 0 {
		return m, nil
	}
	// Score after the round with the highest number.
	last := p.Rounds[keys[0]]
	for _, k := range keys {
		if r := p.Rounds[k]; r.N > last.N {
			last = r
		}
	}
	s.Score = &[2]int{last.Sa, last.Sb}

	// ADR: health actually taken off enemies (hpt, not raw damage, which runs
	// about a third high), per round. Not self damage, not a teammate's.
	damage := map[string]float64{}
	for _, hits := range p.Damage {
		for _, h := range hits {
			by, v := idString(h.By), idString(h.V)
			if by == "" || by == v {
				continue
			}
			a, okA := team[by]
			if b, okB := team[v]; okA && okB && a == b {
				continue
			}
			damage[by] += h.Hpt
		}
	}
	s.Lines = map[string][5]float64{}
	for _, pl := range m.Roster {
		adr := jsRound(damage[pl.ID]/float64(s.Rounds)*10) / 10
		s.Lines[pl.ID] = [5]float64{float64(pl.K), float64(pl.D), float64(pl.A), float64(pl.Team), adr}
	}
	return m, nil
}

// jsRound is JavaScript's Math.round for the non-negative numbers it is used
// on: halves go up. Not math.Round's away-from-zero on the floor-plus-half
// edge, which differs from Math.round for 0.49999999999999994.
func jsRound(x float64) float64 {
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

// roundKeys orders rounds as Object.values does for integer-like keys:
// numerically. Anything else follows, by text.
func roundKeys[V any](rounds map[string]V) []string {
	keys := make([]string, 0, len(rounds))
	for k := range rounds {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		na, ea := strconv.ParseUint(a, 10, 32)
		nb, eb := strconv.ParseUint(b, 10, 32)
		switch {
		case ea == nil && eb == nil:
			return int(int64(na) - int64(nb))
		case ea == nil:
			return -1
		case eb == nil:
			return 1
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})
	return keys
}

// idString is JavaScript's String(x) for a steamid that a payload may carry
// as a string or a number; "" for null or absent.
func idString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
