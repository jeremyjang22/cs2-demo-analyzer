package payload

import (
	"sort"
	"strconv"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// bombSampleGap is the minimum spacing, in seconds, between samples of a
// loose bomb. bomb.csv records a thrown C4's arc at tick resolution - about
// 50 rows for a one-second throw - which is more than a viewer sampling at
// 4 Hz can show. 0.1s keeps the arc smooth and drops roughly 80% of the rows.
const bombSampleGap = 0.1

// nonBullet is every weapon whose fire event is not a bullet leaving a
// barrel. shots.csv records them on purpose - it is the only table that says
// when a grenade was thrown - but a tracer drawn along the view angle would be
// wrong for every one. Names are demoinfocs' display strings.
var nonBullet = map[string]bool{
	"Knife": true, "Zeus x27": true, "C4": true,
	"Smoke Grenade": true, "Flashbang": true, "HE Grenade": true,
	"Molotov": true, "Incendiary Grenade": true, "Decoy Grenade": true,
}

// events converts one round's event tables into the viewer's lists, timed in
// seconds from the round's freeze end. The viewer's clock runs from 0 to span
// (contested play plus the postround), so anything outside that window has
// nowhere to be drawn - except where a method says otherwise.
//
// level maps a world z to a radar section, so an event lands on the right
// floor of a multi-level map. Without it a tracer fired in Nuke's lower site
// would be drawn over upper - plausible-looking and wrong.
type events struct {
	base, end int32
	tickRate  float64
	span      float64
	level     func(z float64) int
}

func (e events) at(tick int32) float64 { return float64(tick-e.base) / e.tickRate }

// steamid is a JSON steamid, or null for 0 (the world, or nobody).
func steamid(id uint64) *string {
	if id == 0 {
		return nil
	}
	s := strconv.FormatUint(id, 10)
	return &s
}

func ptr[T any](v T) *T { return &v }

// dedupeKills keeps one death per victim, the earliest. demoinfocs
// occasionally fires a second Kill for an already-dead player at the exact
// tick the round ends - once in 613 live kills across the reference demos.
// Sorted by tick first; the sort is stable, so equal ticks keep event order.
func dedupeKills(kills []collector.Kill) []collector.Kill {
	sorted := append([]collector.Kill(nil), kills...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Tick < sorted[j].Tick })
	seen := map[uint64]bool{}
	out := sorted[:0]
	for _, k := range sorted {
		if seen[k.VictimSteamID] {
			continue
		}
		seen[k.VictimSteamID] = true
		out = append(out, k)
	}
	return out
}

// kills is every death in every phase. A player who stays alive to save a
// rifle and is hunted down after the round is decided produces a postround
// kill, and dropping it would show a save that succeeded when it did not.
// Freeze-phase deaths fall away on the time window: those are disconnects
// and suicides.
func (e events) kills(kills []collector.Kill) []KillJSON {
	out := []KillJSON{}
	for _, k := range dedupeKills(kills) {
		t := e.at(k.Tick)
		if t < 0 || t > e.span {
			continue
		}
		row := KillJSON{
			T: round2(t), K: steamid(k.KillerSteamID), V: strconv.FormatUint(k.VictimSteamID, 10),
			A: steamid(k.AssisterSteamID), W: k.Weapon,
			Hs: boolInt(k.Headshot), Wb: boolInt(k.Penetrated > 0),
		}
		if k.Phase == collector.PhasePostRound {
			// Seconds after the win condition: 2.5s reads very differently
			// from 6.9s when the question is whether somebody got out with
			// their gun. Off the raw end tick, not the rounded dur.
			row.Post = ptr(round1(float64(k.Tick-e.end) / e.tickRate))
		}
		out = append(out, row)
	}
	return out
}

// utility is every grenade effect deployed in live play. An effect that never
// expired (end tick 0) runs to the round's end.
func (e events) utility(util []collector.Utility) []UtilJSON {
	out := []UtilJSON{}
	for _, u := range util {
		if u.Phase != collector.PhaseLive {
			continue
		}
		end := u.EndTick
		if end == 0 {
			end = e.end
		}
		row := UtilJSON{
			T: round2(max(0, e.at(u.StartTick))), T1: round2(max(0, e.at(end))),
			Kind: u.Kind, By: steamid(u.ThrowerSteamID), Team: int(u.ThrowerTeam),
			X: whole(q2(u.X)), Y: whole(q2(u.Y)),
		}
		// Measured spread, not a per-kind constant: a molotov thrown into a
		// corner covers less ground than one on open floor.
		if r := whole(q2(u.Radius)); r != 0 {
			row.R = &r
		}
		out = append(out, row)
	}
	return out
}

// shots is one entry per bullet fired, live and postround, with an endpoint
// where one is knowable. A demo never traces a bullet; what it does record is
// that a weapon fired at tick t and that somebody took damage at tick t from
// the same attacker, which is enough to say where that bullet stopped. A shot
// that hit a wall has no such match and carries only its yaw - that is all
// the demo saw, and drawing it to a made-up endpoint would invent geometry.
//
// A shotgun blast that damages two players gives one shot two candidates; the
// nearer wins, being where the visible tracer stopped. Ties go to the first.
func (e events) shots(shots []collector.Shot, damage []collector.Damage) []ShotJSON {
	type hitKey struct {
		tick int32
		by   uint64
	}
	hits := map[hitKey][]collector.Damage{}
	for _, d := range damage {
		if d.AttackerSteamID != 0 {
			k := hitKey{d.Tick, d.AttackerSteamID}
			hits[k] = append(hits[k], d)
		}
	}

	out := []ShotJSON{}
	for _, s := range shots {
		if nonBullet[s.Weapon] || (s.Phase != collector.PhaseLive && s.Phase != collector.PhasePostRound) {
			continue
		}
		t := e.at(s.Tick)
		if t < 0 || t > e.span {
			continue
		}
		x, y := q2(s.X), q2(s.Y)
		row := ShotJSON{
			T: round2(t), By: strconv.FormatUint(s.SteamID, 10),
			X: whole(x), Y: whole(y),
			// Whole degrees, matching the yaw on every track sample.
			A: whole(q2(s.Yaw)), Lv: e.level(q2(s.Z)),
		}
		var best *collector.Damage
		bestD := 0.0
		for i, d := range hits[hitKey{s.Tick, s.SteamID}] {
			dx, dy := q2(d.X)-x, q2(d.Y)-y
			if dist := dx*dx + dy*dy; best == nil || dist < bestD {
				best, bestD = &hits[hitKey{s.Tick, s.SteamID}][i], dist
			}
		}
		if best != nil {
			row.Hx, row.Hy = ptr(whole(q2(best.X))), ptr(whole(q2(best.Y)))
		}
		out = append(out, row)
	}
	return out
}

// damage is one entry per hit taken, positioned on the victim.
//
// Simultaneous hits from one attacker are merged. A shotgun blast is nine
// pellets and damage.csv records all nine, but a viewer stacking nine markers
// on one spot shows a smear, and nobody experienced nine hits. They are
// summed into the one hit the victim felt, with n recording how many went in.
//
// hp is the raw damage, which can exceed what the victim had left: a 5 HP
// player hit for 502 by the bomb took a 502-damage hit. hpt is what the
// victim actually LOST, which is what any average has to be built from - it
// is derived from each victim's running health rather than read from the
// parser, whose HealthDamageTaken reports a shotgun's whole health drop on
// more than one pellet. Raw damage puts the reference Anubis demo's mean ADR
// at 103.6, a figure no player has ever posted; this puts it at 80.9.
func (e events) damage(damage []collector.Damage) []HitJSON {
	sorted := append([]collector.Damage(nil), damage...)
	// A running health reading is meaningless out of order.
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Tick < sorted[j].Tick })

	type mergeKey struct {
		tick           int32
		victim, attack uint64
		kind           string
	}
	merged := map[mergeKey]int{}
	remaining := map[uint64]int{} // everyone starts a round at full health
	out := []HitJSON{}
	for _, d := range sorted {
		t := e.at(d.Tick)
		if t < 0 || t > e.span {
			continue
		}
		before, ok := remaining[d.VictimSteamID]
		if !ok {
			before = 100
		}
		// max(0, ...) because a health shot can raise it.
		actual := max(0, before-int(d.HealthRemaining))
		remaining[d.VictimSteamID] = int(d.HealthRemaining)

		// Keyed on the raw tick, not the rounded time.
		key := mergeKey{d.Tick, d.VictimSteamID, d.AttackerSteamID, d.Kind}
		if i, ok := merged[key]; ok {
			hit := &out[i]
			// The hitgroup of the biggest component: a blast that clips a leg
			// and a head is a headshot as far as a reader is concerned.
			if int(d.HealthDamage) > hit.Hp {
				hit.Hg = int(d.HitGroup)
			}
			hit.Hp += int(d.HealthDamage)
			hit.Hpt += actual
			hit.Ar += int(d.ArmorDamage)
			hit.N++
			continue
		}
		merged[key] = len(out)
		out = append(out, HitJSON{
			T: round2(t), V: strconv.FormatUint(d.VictimSteamID, 10),
			By: steamid(d.AttackerSteamID), K: d.Kind,
			Hp: int(d.HealthDamage), Hpt: actual, Ar: int(d.ArmorDamage),
			Hg: int(d.HitGroup), N: 1,
			X: whole(q2(d.X)), Y: whole(q2(d.Y)), Lv: e.level(q2(d.Z)),
		})
	}
	return out
}

// bomb is the C4's state over the round, thinned to what a viewer can draw.
//
// Unlike every other list, rows before the round went live are not dropped:
// the bomb is picked up in freezetime, and discarding those rows would show
// nobody carrying it until the first handoff. The last of them is kept and
// pinned to t = 0 - "who walked out of spawn with it". The far end runs to
// the close of the postround, so a bomb that detonates after the round is
// decided stays on screen as long as the viewer can scrub to it.
func (e events) bomb(bomb []collector.BombSample) []BombJSON {
	out := []BombJSON{}
	var start *BombJSON
	for _, b := range bomb {
		t := e.at(b.Tick)
		row := BombJSON{
			T: round2(max(0, min(t, e.span))), St: b.State, By: steamid(b.CarrierSteamID),
			X: whole(q2(b.X)), Y: whole(q2(b.Y)), Lv: e.level(q2(b.Z)), Site: b.Site,
		}
		if t < 0 {
			start = &row // keep only the latest pre-live state
			continue
		}
		if t > e.span {
			continue
		}
		// Thin a thrown bomb's arc, never a change of state or carrier: those
		// are what tell the viewer to draw something different.
		if n := len(out); n > 0 {
			last := out[n-1]
			if row.St == last.St && sameID(row.By, last.By) && row.T-last.T < bombSampleGap {
				continue
			}
		}
		out = append(out, row)
	}
	if start != nil {
		out = append([]BombJSON{*start}, out...)
	}
	return out
}

func sameID(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// kits is defuse kits appearing on and leaving the ground. Early times are
// pinned to 0 rather than dropped, as for the bomb. A drop with no matching
// take is the normal case - most kits are never picked up - and the viewer
// draws it for the rest of the round. The id pairs a take with its drop and
// is unique within the round only.
func (e events) kits(kits []collector.KitEvent) []KitJSON {
	out := []KitJSON{}
	for _, k := range kits {
		t := e.at(k.Tick)
		if t > e.span {
			continue
		}
		out = append(out, KitJSON{
			T: round2(max(0, t)), Ev: k.Event, ID: int(k.KitID),
			By: strconv.FormatUint(k.SteamID, 10),
			X:  whole(q2(k.X)), Y: whole(q2(k.Y)), Lv: e.level(q2(k.Z)),
		})
	}
	return out
}

// throws is one entry per grenade thrown, as a polyline with its own clock.
//
// ts is centiseconds from t for each point. Points are sampled by DISTANCE,
// and a grenade decelerates, so interpolating time evenly along the path
// would run the arc fast at the start and slow at the end. lvl is one char per
// point: a throw from Nuke's upper level to lower crosses floors mid-flight.
func (e events) throws(points []collector.TrajectoryPoint) []ThrowJSON {
	paths := map[int32][]collector.TrajectoryPoint{}
	var ids []int32
	for _, p := range points {
		if _, ok := paths[p.ProjectileID]; !ok {
			ids = append(ids, p.ProjectileID)
		}
		paths[p.ProjectileID] = append(paths[p.ProjectileID], p)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := []ThrowJSON{}
	for _, id := range ids {
		path := paths[id]
		sort.SliceStable(path, func(i, j int) bool { return path[i].Seq < path[j].Seq })
		first, last := path[0], path[len(path)-1]
		t0, t1 := e.at(first.Tick), e.at(last.Tick)
		// Thrown before the round went live, or after the clock runs out:
		// nowhere to draw it.
		if t1 < 0 || t0 > e.span {
			continue
		}
		row := ThrowJSON{
			T: round2(max(0, t0)), T1: round2(min(t1, e.span)),
			Kind: first.Kind, By: steamid(first.ThrowerSteamID), Team: int(first.ThrowerTeam),
			X: make([]int, len(path)), Y: make([]int, len(path)), Ts: make([]int, len(path)),
		}
		lvl := make([]byte, len(path))
		for i, p := range path {
			row.X[i], row.Y[i] = whole(q2(p.X)), whole(q2(p.Y))
			row.Ts[i] = whole(float64(p.Tick-first.Tick) / e.tickRate * 100)
			lvl[i] = byte('0' + e.level(q2(p.Z)))
		}
		row.Lvl = string(lvl)
		out = append(out, row)
	}
	// By throw time, which every other list here is in by construction. This
	// one is grouped by projectile, and a consumer that walks it and stops at
	// the first future event would lose everything after an out-of-order one.
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}
