package analytics

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// clipVersion is the shape of a clip list. The HLAE runner on the recording
// machine refuses a version it does not know.
const clipVersion = 1

// steamID64Base turns a steamid64 into the 32-bit account id CS2's spectator
// commands take (spec_lock_to_accountid).
const steamID64Base = 76561197960265728

// Clip kinds. Each is one reason a moment is worth recording from a player's
// point of view.
const (
	ClipOpening   = "opening"   // the player got the round's first kill
	ClipMultiKill = "multikill" // the player killed MinMulti or more in one round
	ClipClutch    = "clutch"    // the player was last alive against at least one enemy
	ClipRound     = "round"     // the player's whole round, freeze end to round end
)

// ClipKinds is every kind, in the order the flag help lists them.
var ClipKinds = []string{ClipOpening, ClipMultiKill, ClipClutch, ClipRound}

// ClipOptions chooses which moments become clips and how much is kept around
// them.
type ClipOptions struct {
	// Players are steamid64s to record. Empty means every player.
	Players map[uint64]bool
	// Kinds to emit; empty means opening, multikill and clutch.
	Kinds []string
	// MinMulti is the kill count that makes a multikill. Zero means 3.
	MinMulti int
	// AllClutches also emits lost clutches. By default only won ones are
	// worth a recording.
	AllClutches bool
	// Before and After pad each clip, in seconds.
	Before, After float64
	// FromRoundStart starts every clip when the round's freezetime ends
	// instead of Before seconds ahead of the moment, so the clip shows how
	// the player got there. Clips of one player in one round then always
	// overlap and, unless NoMerge, become one.
	FromRoundStart bool
	// NoMerge keeps overlapping clips of the same player and round apart. By
	// default they are joined, so an opening kill that starts a 4k is
	// recorded once.
	NoMerge bool
}

// ClipList is what clip-list writes and the HLAE runner reads.
type ClipList struct {
	Version int    `json:"version"`
	Clips   []Clip `json:"clips"`
}

// Clip is one stretch of one demo to record from one player's eyes.
//
// Ticks are demo ticks, the unit demo_gototick takes. StartTick already
// includes the lead-in and EndTick the tail, and both are kept inside the
// round they belong to.
type Clip struct {
	ID         string   `json:"id"`
	Demo       string   `json:"demo"` // the demo's basename; the runner adds .dem
	Map        string   `json:"map"`
	TickRate   float64  `json:"tick_rate"`
	Round      int32    `json:"round"`
	Kinds      []string `json:"kinds"`
	SteamID    string   `json:"steamid"`
	AccountID  uint32   `json:"account_id"`
	PlayerName string   `json:"player_name"`
	StartTick  int32    `json:"start_tick"`
	EndTick    int32    `json:"end_tick"`
	// EventTicks are the moments themselves (each kill, the clutch start),
	// for a reel that wants to mark them.
	EventTicks []int32 `json:"event_ticks"`
	// Sequence numbers the clip within its demo from 1, in recording order.
	// It is the N of the "N-sequence" folder CS Demo Manager records it to.
	Sequence int    `json:"sequence"`
	Label    string `json:"label"`
}

// Clips lists the moments in one match worth recording, in demo order.
func Clips(m *Match, opt ClipOptions) []Clip {
	kinds := opt.Kinds
	if len(kinds) == 0 {
		kinds = []string{ClipOpening, ClipMultiKill, ClipClutch}
	}
	want := func(k string) bool { return slices.Contains(kinds, k) }
	minMulti := cmp.Or(opt.MinMulti, 3)
	rate := m.TickRate
	if rate <= 0 {
		rate = defaultTickRate
	}
	before, after := int32(opt.Before*rate+0.5), int32(opt.After*rate+0.5)
	follow := func(id uint64) bool { return id != 0 && (len(opt.Players) == 0 || opt.Players[id]) }

	r := NewReport()
	r.Add(m, nil)
	rounds := make(map[int32]*Round, len(m.Rounds))
	for i := range m.Rounds {
		rounds[m.Rounds[i].Meta.Number] = &m.Rounds[i]
	}

	var out []Clip
	add := func(round int32, kind string, player uint64, from, to int32, events []int32, label string) {
		rd := rounds[round]
		if rd == nil {
			return
		}
		start, end := from-before, to+after
		if opt.FromRoundStart {
			start = rd.Meta.FreezeEndTick
		}
		// A clip never reaches into the previous round's freezetime or past
		// the moment the next one begins, where the player may not exist.
		start = max(start, rd.Meta.StartTick)
		if last := cmp.Or(rd.Meta.OfficialEndTick, rd.Meta.EndTick); last > 0 {
			end = min(end, last)
		}
		out = append(out, Clip{
			Demo: m.Name, Map: m.Map, TickRate: rate, Round: round, Kinds: []string{kind},
			SteamID: idStr(player), AccountID: uint32(player - steamID64Base), PlayerName: m.Names[player],
			StartTick: start, EndTick: end, EventTicks: events, Label: label,
		})
	}
	name := func(id uint64) string { return cmp.Or(m.Names[id], idStr(id)) }

	if want(ClipOpening) {
		for _, o := range r.Openings {
			if follow(o.Killer) {
				hs := ""
				if o.Headshot {
					hs = " (HS)"
				}
				add(o.Round, ClipOpening, o.Killer, o.Tick, o.Tick, []int32{o.Tick},
					fmt.Sprintf("opening kill on %s with %s%s", name(o.Victim), o.Weapon, hs))
			}
		}
	}

	if want(ClipMultiKill) {
		type key struct {
			round  int32
			player uint64
		}
		var order []key
		ticks := make(map[key][]int32)
		for _, d := range r.Deaths {
			if !follow(d.Killer) {
				continue
			}
			k := key{d.Round, d.Killer}
			if ticks[k] == nil {
				order = append(order, k)
			}
			ticks[k] = append(ticks[k], d.Tick)
		}
		for _, k := range order {
			t := ticks[k]
			if len(t) >= minMulti {
				add(k.round, ClipMultiKill, k.player, t[0], t[len(t)-1], t, fmt.Sprintf("%dk", len(t)))
			}
		}
	}

	if want(ClipClutch) {
		for _, c := range r.Clutches {
			if !follow(c.Player) || (!c.Won && !opt.AllClutches) {
				continue
			}
			rd := rounds[c.Round]
			if rd == nil {
				continue
			}
			result := "lost"
			if c.Won {
				result = "won"
			}
			// A clutch plays out to the end of the round, kill or no kill:
			// the defuse or the hide is the point of it.
			add(c.Round, ClipClutch, c.Player, c.Tick, rd.Meta.EndTick, []int32{c.Tick},
				fmt.Sprintf("1v%d clutch, %d kill(s), %s", len(c.Enemies), c.Kills, result))
		}
	}

	if want(ClipRound) {
		for _, rd := range m.Rounds {
			if _, decided := sideIndex(rd.Meta.Winner); !decided {
				continue
			}
			for _, p := range rd.Meta.Players {
				if _, ok := sideIndex(p.Team); ok && follow(p.SteamID) {
					// No lead-in: before freeze end is the buy, not the round.
					add(rd.Meta.Number, ClipRound, p.SteamID, rd.Meta.FreezeEndTick+before,
						rd.Meta.EndTick, nil, "full round")
				}
			}
		}
	}

	slices.SortStableFunc(out, func(a, b Clip) int {
		return cmp.Or(cmp.Compare(a.StartTick, b.StartTick), cmp.Compare(a.SteamID, b.SteamID))
	})
	if !opt.NoMerge {
		out = mergeClips(out)
	}
	for i := range out {
		c := &out[i]
		c.Sequence = i + 1
		c.ID = fmt.Sprintf("%s_r%02d_%d_%s", m.Name, c.Round, c.AccountID, strings.Join(c.Kinds, "+"))
		if c.EventTicks == nil {
			c.EventTicks = []int32{}
		}
	}
	return out
}

// mergeClips joins clips of the same player in the same round whose ranges
// overlap. The input is sorted by start tick.
func mergeClips(in []Clip) []Clip {
	var out []Clip
	for _, c := range in {
		joined := false
		for i := len(out) - 1; i >= 0; i-- {
			o := &out[i]
			if o.Round != c.Round {
				break
			}
			if o.SteamID != c.SteamID || c.StartTick > o.EndTick {
				continue
			}
			o.EndTick = max(o.EndTick, c.EndTick)
			for _, k := range c.Kinds {
				if !slices.Contains(o.Kinds, k) {
					o.Kinds = append(o.Kinds, k)
				}
			}
			for _, t := range c.EventTicks {
				if !slices.Contains(o.EventTicks, t) {
					o.EventTicks = append(o.EventTicks, t)
				}
			}
			slices.Sort(o.EventTicks)
			o.Label += "; " + c.Label
			joined = true
			break
		}
		if !joined {
			out = append(out, c)
		}
	}
	return out
}

// NewClipList wraps clips for writing.
func NewClipList(clips []Clip) ClipList {
	if clips == nil {
		clips = []Clip{}
	}
	return ClipList{Version: clipVersion, Clips: clips}
}
