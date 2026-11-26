package payload

import (
	"math"
	"sort"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// Loadout flag bits, packed into one slot of an equipment entry. A bitmask
// rather than three booleans because this repeats once per change per player
// per round, and three JSON fields of 0 cost more than one digit.
const (
	eqHelmet = 1
	eqKit    = 2
	eqBomb   = 4
)

// byPlayer groups a round's ticks by player.
func byPlayer(ticks []collector.PlayerTick) map[uint64][]collector.PlayerTick {
	out := map[uint64][]collector.PlayerTick{}
	for _, t := range ticks {
		out[t.SteamID] = append(out[t.SteamID], t)
	}
	return out
}

// sampleTrack resamples one player's round to one sample every `step` ticks,
// from their first living tick. Nil when they were never alive this round.
//
// Each sample is SNAPPED to the first real tick at or after its time rather
// than interpolated: an interpolated sample can sit inside a wall. Dead rows
// are not sampled, but their existence is what says the player died: d is
// then the index of their last sample, and -1 for a survivor.
func sampleTrack(rows []collector.PlayerTick, step float64, level func(float64) int) *TrackJSON {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Tick < rows[j].Tick })
	var alive []collector.PlayerTick
	died := false
	for _, r := range rows {
		if r.IsAlive {
			alive = append(alive, r)
		} else {
			died = true
		}
	}
	if len(alive) == 0 {
		return nil
	}

	picked := alive[:0:0]
	for _, i := range sampleIndexes(alive, step) {
		picked = append(picked, alive[i])
	}

	tr := &TrackJSON{
		S: int(rows[0].Team),
		D: -1,
		X: make([]int, len(picked)), Y: make([]int, len(picked)), Yaw: make([]int, len(picked)),
	}
	if died {
		tr.D = len(picked) - 1
	}
	air := make([]byte, len(picked))
	scoped := make([]byte, len(picked))
	lvl := make([]byte, len(picked))
	for i, p := range picked {
		tr.X[i], tr.Y[i] = whole(q2(p.X)), whole(q2(p.Y))
		// Whole degrees is finer than a view cone can show, and keeps this to
		// ~4 bytes a sample instead of ~8.
		tr.Yaw[i] = whole(q2(p.Yaw))
		// One char per sample: ~1 byte where a JSON array of ints costs two,
		// and it gzips just as well.
		air[i], scoped[i] = flagChar(p.IsAirborne), flagChar(p.IsScoped)
		// Which radar image the sample is drawn on. Always 0 on single-level
		// maps; on Nuke, Vertigo and Train it flips mid-round.
		lvl[i] = byte('0' + level(q2(p.Z)))
	}
	tr.Air, tr.Sc, tr.Lvl = string(air), string(scoped), string(lvl)
	tr.Eq = equipmentChanges(picked)
	return tr
}

// sampleIndexes picks, for each multiple of step from 0 up to the last living
// tick's offset, the first row at or after it - numpy's searchsorted over
// arange(0, last+1, step), clipped to the last row, which is what the Python
// did.
func sampleIndexes(alive []collector.PlayerTick, step float64) []int {
	first := alive[0].Tick
	last := float64(alive[len(alive)-1].Tick - first)
	n := int(math.Ceil((last + 1) / step))
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		v := float64(i) * step
		idx := sort.Search(len(alive), func(j int) bool { return float64(alive[j].Tick-first) >= v })
		out = append(out, min(idx, len(alive)-1))
	}
	return out
}

// equipmentChanges is the loadout per sample, stored as changes rather than
// per sample. Equipment is nearly constant within a round, so storing it per
// sample would repeat the same values a hundred times. Each entry is
//
//	[sample_index, health, armor, money, flags, primary, secondary, nades]
//
// and holds until the next. Health and money ride along because both change
// a handful of times a round: a change list says the same thing as a
// per-sample array for a fraction of the size.
func equipmentChanges(picked []collector.PlayerTick) [][]any {
	out := [][]any{}
	var prev []any
	for i, p := range picked {
		flags := 0
		if p.HasHelmet {
			flags |= eqHelmet
		}
		if p.HasKit {
			flags |= eqKit
		}
		if p.HasBomb {
			flags |= eqBomb
		}
		entry := []any{int(p.Health), int(p.Armor), int(p.Money), flags, p.Primary, p.Secondary, p.Nades}
		if !sameEntry(entry, prev) {
			out = append(out, append([]any{i}, entry...))
			prev = entry
		}
	}
	return out
}

func sameEntry(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func flagChar(v bool) byte {
	if v {
		return '1'
	}
	return '0'
}
