// track-player follows a single player through a CS2 demo and reports
// their behavior round by round: combat, utility usage, economy,
// movement, and positioning.
//
// Usage:
//
//	track-player <path-to-dem> <player-name-or-steamid64>
//
// The player argument is a case-insensitive name substring (first match
// wins) or an exact SteamID64.
//
// Live-round gating reuses the hello-demo strategy: events only count
// once AnnouncementMatchStarted has fired and IsWarmupPeriod() is false,
// which filters both warmup rounds and the false-positive RoundStart at
// tick 0.
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/geo/r3"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// metersPerUnit converts Hammer units to meters (1 unit = 0.75 inch).
const metersPerUnit = 0.01905

type roundStats struct {
	num          int
	kills        int
	deaths       int
	assists      int
	flashAssists int
	headshots    int
	dmgDealt     int
	dmgTaken     int
	shots        int
	hits         int
	nades        int
	moneyFreeze  int
	equipFreeze  int
	distance     float64 // Hammer units, XY plane
	survived     bool
	won          bool
	ended        bool
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: track-player <path-to-dem> <player-name-or-steamid64>")
		os.Exit(1)
	}
	demoPath, query := os.Args[1], os.Args[2]

	f, err := os.Open(demoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	var (
		trackedID   uint64
		trackedName string

		announcementSeen bool

		rounds []*roundStats
		cur    *roundStats

		// aggregates that don't fit the per-round table
		killsByWeapon  = map[string]int{}
		nadesByType    = map[string]int{}
		hitsByGroup    = map[events.HitGroup]int{}
		placeSamples   = map[string]int{}
		blindInflicted time.Duration
		blindSuffered  time.Duration
		enemiesFlashed int
		teamFlashes    int
		jumps          int
		reloads        int
		plants         int
		defuses        int
		mvps           int
		wallbangKills  int
		noscopeKills   int
		smokeKills     int
		blindKills     int

		eventLog []string

		lastPos    r3.Vector
		hasLastPos bool
		frameCount int
	)

	wantID, wantIDErr := strconv.ParseUint(query, 10, 64)
	queryLower := strings.ToLower(query)

	tracked := func(p *common.Player) bool {
		return p != nil && trackedID != 0 && p.SteamID64 == trackedID
	}

	// resolve locks onto a SteamID64 the first time a participant matches
	// the query, so name changes mid-game don't lose the player.
	resolve := func() {
		if trackedID != 0 {
			return
		}
		for _, p := range parser.GameState().Participants().All() {
			if (wantIDErr == nil && p.SteamID64 == wantID) ||
				(wantIDErr != nil && strings.Contains(strings.ToLower(p.Name), queryLower)) {
				trackedID = p.SteamID64
				trackedName = p.Name
				return
			}
		}
	}

	trackedPlayer := func() *common.Player {
		if trackedID == 0 {
			return nil
		}
		for _, p := range parser.GameState().Participants().All() {
			if p.SteamID64 == trackedID {
				return p
			}
		}
		return nil
	}

	live := func() bool {
		return announcementSeen && !parser.GameState().IsWarmupPeriod()
	}

	logf := func(format string, args ...any) {
		round := 0
		if cur != nil {
			round = cur.num
		}
		stamp := parser.CurrentTime().Truncate(time.Second)
		eventLog = append(eventLog, fmt.Sprintf("[R%02d %7s] %s", round, stamp, fmt.Sprintf(format, args...)))
	}

	isGun := func(w *common.Equipment) bool {
		if w == nil {
			return false
		}
		switch w.Class() {
		case common.EqClassPistols, common.EqClassSMG, common.EqClassHeavy, common.EqClassRifle:
			return true
		}
		return false
	}

	parser.RegisterEventHandler(func(e events.AnnouncementMatchStarted) {
		announcementSeen = true
		resolve()
	})

	parser.RegisterEventHandler(func(e events.RoundStart) {
		if !live() {
			return
		}
		resolve()
		cur = &roundStats{num: parser.GameState().TotalRoundsPlayed() + 1}
		rounds = append(rounds, cur)
		hasLastPos = false
	})

	parser.RegisterEventHandler(func(e events.RoundFreezetimeEnd) {
		if cur == nil {
			return
		}
		if p := trackedPlayer(); p != nil {
			cur.moneyFreeze = p.Money()
			cur.equipFreeze = p.EquipmentValueFreezeTimeEnd()
		}
	})

	parser.RegisterEventHandler(func(e events.RoundEnd) {
		if cur == nil || cur.ended {
			return
		}
		cur.ended = true
		if p := trackedPlayer(); p != nil {
			cur.survived = p.IsAlive()
			cur.won = e.Winner == p.Team
		}
	})

	parser.RegisterEventHandler(func(e events.Kill) {
		if cur == nil {
			return
		}
		if tracked(e.Killer) {
			cur.kills++
			if e.IsHeadshot {
				cur.headshots++
			}
			weapon := "?"
			if e.Weapon != nil {
				weapon = e.Weapon.String()
			}
			killsByWeapon[weapon]++
			var tags []string
			if e.IsHeadshot {
				tags = append(tags, "HS")
			}
			if e.IsWallBang() {
				wallbangKills++
				tags = append(tags, "wallbang")
			}
			if e.NoScope {
				noscopeKills++
				tags = append(tags, "noscope")
			}
			if e.ThroughSmoke {
				smokeKills++
				tags = append(tags, "through smoke")
			}
			if e.AttackerBlind {
				blindKills++
				tags = append(tags, "while blind")
			}
			tag := ""
			if len(tags) > 0 {
				tag = " [" + strings.Join(tags, ", ") + "]"
			}
			victim := "?"
			if e.Victim != nil {
				victim = e.Victim.Name
			}
			logf("KILL   %s (%s, %.0fm)%s", victim, weapon, e.Distance, tag)
		}
		if tracked(e.Victim) {
			cur.deaths++
			hasLastPos = false
			killer, weapon := "world", "?"
			if e.Killer != nil {
				killer = e.Killer.Name
			}
			if e.Weapon != nil {
				weapon = e.Weapon.String()
			}
			logf("DEATH  by %s (%s)", killer, weapon)
		}
		if tracked(e.Assister) {
			cur.assists++
			if e.AssistedFlash {
				cur.flashAssists++
			}
		}
	})

	parser.RegisterEventHandler(func(e events.PlayerHurt) {
		if cur == nil {
			return
		}
		if tracked(e.Attacker) && e.Player != nil && e.Player.Team != e.Attacker.Team {
			cur.dmgDealt += e.HealthDamageTaken
			if isGun(e.Weapon) {
				cur.hits++
				hitsByGroup[e.HitGroup]++
			}
		}
		if tracked(e.Player) {
			cur.dmgTaken += e.HealthDamageTaken
		}
	})

	parser.RegisterEventHandler(func(e events.WeaponFire) {
		if cur == nil || !tracked(e.Shooter) {
			return
		}
		if isGun(e.Weapon) {
			cur.shots++
		}
	})

	parser.RegisterEventHandler(func(e events.WeaponReload) {
		if cur != nil && tracked(e.Player) {
			reloads++
		}
	})

	parser.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		if cur == nil || e.Projectile == nil || !tracked(e.Projectile.Thrower) {
			return
		}
		cur.nades++
		nadesByType[e.Projectile.WeaponInstance.String()]++
	})

	parser.RegisterEventHandler(func(e events.PlayerFlashed) {
		if cur == nil || e.Player == nil {
			return
		}
		if tracked(e.Attacker) && e.Player.SteamID64 != trackedID {
			if e.Player.Team == e.Attacker.Team {
				teamFlashes++
			} else {
				enemiesFlashed++
				blindInflicted += e.FlashDuration()
			}
		}
		if tracked(e.Player) {
			blindSuffered += e.FlashDuration()
		}
	})

	parser.RegisterEventHandler(func(e events.PlayerJump) {
		if cur != nil && tracked(e.Player) {
			jumps++
		}
	})

	parser.RegisterEventHandler(func(e events.BombPlanted) {
		if cur != nil && tracked(e.Player) {
			plants++
			logf("PLANT  bombsite %c", e.Site)
		}
	})

	parser.RegisterEventHandler(func(e events.BombDefused) {
		if cur != nil && tracked(e.Player) {
			defuses++
			logf("DEFUSE bombsite %c", e.Site)
		}
	})

	parser.RegisterEventHandler(func(e events.RoundMVPAnnouncement) {
		if cur != nil && tracked(e.Player) {
			mvps++
			logf("MVP    (reason %d)", e.Reason)
		}
	})

	// Movement + position sampling. Distance accumulates every frame while
	// alive; place names sample every 32nd frame to keep counts meaningful.
	parser.RegisterEventHandler(func(e events.FrameDone) {
		frameCount++
		if cur == nil {
			return
		}
		p := trackedPlayer()
		if p == nil || !p.IsAlive() {
			hasLastPos = false
			return
		}
		pos := p.Position()
		if hasLastPos {
			d := pos.Sub(lastPos)
			d.Z = 0
			step := d.Norm()
			// Skip teleport-sized jumps (round resets, spawns).
			if step < 100 {
				cur.distance += step
			}
		}
		lastPos, hasLastPos = pos, true

		if frameCount%32 == 0 {
			if place := p.LastPlaceName(); place != "" {
				placeSamples[place]++
			}
		}
	})

	if err := parser.ParseToEnd(); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	if trackedID == 0 {
		fmt.Fprintf(os.Stderr, "no player matching %q found in demo\n", query)
		os.Exit(1)
	}

	// --- Report ---
	fmt.Printf("== Tracking: %s (%d) ==\n", trackedName, trackedID)

	fmt.Println("\n== Rounds ==")
	fmt.Println("Rnd    K  D  A   HS   Dmg  Taken  Shots  Hits  Nades  Money  Equip  Dist(m)  Result")
	var (
		tK, tD, tA, tHS, tDmg, tTaken, tShots, tHits, tNades int
		tDist                                                float64
		roundsWon                                            int
	)
	for _, r := range rounds {
		result := "loss"
		if r.won {
			result = "WIN"
			roundsWon++
		}
		if r.survived {
			result += " (alive)"
		}
		fmt.Printf("%3d   %2d %2d %2d   %2d  %4d   %4d   %4d  %4d   %4d  %5d  %5d  %7.0f  %s\n",
			r.num, r.kills, r.deaths, r.assists, r.headshots, r.dmgDealt, r.dmgTaken,
			r.shots, r.hits, r.nades, r.moneyFreeze, r.equipFreeze,
			r.distance*metersPerUnit, result)
		tK += r.kills
		tD += r.deaths
		tA += r.assists
		tHS += r.headshots
		tDmg += r.dmgDealt
		tTaken += r.dmgTaken
		tShots += r.shots
		tHits += r.hits
		tNades += r.nades
		tDist += r.distance
	}

	n := len(rounds)
	if n == 0 {
		fmt.Println("  (no live rounds found)")
		return
	}

	fmt.Println("\n== Totals ==")
	fmt.Printf("K/D/A:            %d/%d/%d  (%.2f KD)\n", tK, tD, tA, safeDiv(float64(tK), float64(tD)))
	fmt.Printf("Headshot kills:   %d (%.0f%%)\n", tHS, 100*safeDiv(float64(tHS), float64(tK)))
	fmt.Printf("Special kills:    %d wallbang, %d noscope, %d through smoke, %d while blind\n",
		wallbangKills, noscopeKills, smokeKills, blindKills)
	fmt.Printf("ADR:              %.1f  (%d damage over %d rounds)\n", safeDiv(float64(tDmg), float64(n)), tDmg, n)
	fmt.Printf("Damage taken:     %d (%.1f per round)\n", tTaken, safeDiv(float64(tTaken), float64(n)))
	fmt.Printf("Accuracy:         %.1f%%  (%d hits / %d shots)\n", 100*safeDiv(float64(tHits), float64(tShots)), tHits, tShots)
	fmt.Printf("Reloads:          %d\n", reloads)
	fmt.Printf("Grenades thrown:  %d\n", tNades)
	fmt.Printf("Enemies flashed:  %d for %s total (team flashes: %d)\n", enemiesFlashed, blindInflicted.Truncate(time.Millisecond*100), teamFlashes)
	fmt.Printf("Time blinded:     %s\n", blindSuffered.Truncate(time.Millisecond*100))
	fmt.Printf("Flash assists:    %d\n", sumFlashAssists(rounds))
	fmt.Printf("Bomb:             %d plants, %d defuses\n", plants, defuses)
	fmt.Printf("MVPs:             %d\n", mvps)
	fmt.Printf("Jumps:            %d\n", jumps)
	fmt.Printf("Distance moved:   %.0f m (%.0f m per round)\n", tDist*metersPerUnit, safeDiv(tDist*metersPerUnit, float64(n)))
	fmt.Printf("Rounds won:       %d/%d\n", roundsWon, n)

	fmt.Println("\n== Kills by weapon ==")
	for _, kv := range sortedByCount(killsByWeapon) {
		fmt.Printf("  %-20s %d\n", kv.k, kv.v)
	}

	fmt.Println("\n== Grenades by type ==")
	for _, kv := range sortedByCount(nadesByType) {
		fmt.Printf("  %-20s %d\n", kv.k, kv.v)
	}

	fmt.Println("\n== Hit locations ==")
	for _, kv := range sortedByCount(hitGroupNames(hitsByGroup)) {
		fmt.Printf("  %-20s %d\n", kv.k, kv.v)
	}

	fmt.Println("\n== Most visited map areas ==")
	top := sortedByCount(placeSamples)
	if len(top) > 8 {
		top = top[:8]
	}
	for _, kv := range top {
		fmt.Printf("  %-20s %5.1f%% of samples\n", kv.k, 100*safeDiv(float64(kv.v), float64(totalCount(placeSamples))))
	}

	fmt.Println("\n== Event log ==")
	for _, line := range eventLog {
		fmt.Println("  " + line)
	}
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func sumFlashAssists(rounds []*roundStats) int {
	total := 0
	for _, r := range rounds {
		total += r.flashAssists
	}
	return total
}

type kv struct {
	k string
	v int
}

func sortedByCount(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].v != out[j].v {
			return out[i].v > out[j].v
		}
		return out[i].k < out[j].k
	})
	return out
}

func totalCount(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func hitGroupNames(m map[events.HitGroup]int) map[string]int {
	names := map[events.HitGroup]string{
		events.HitGroupGeneric:  "generic",
		events.HitGroupHead:     "head",
		events.HitGroupChest:    "chest",
		events.HitGroupStomach:  "stomach",
		events.HitGroupLeftArm:  "left arm",
		events.HitGroupRightArm: "right arm",
		events.HitGroupLeftLeg:  "left leg",
		events.HitGroupRightLeg: "right leg",
		events.HitGroupNeck:     "neck",
		events.HitGroupGear:     "gear",
	}
	out := map[string]int{}
	for g, v := range m {
		name, ok := names[g]
		if !ok {
			name = fmt.Sprintf("group %d", g)
		}
		out[name] += v
	}
	return out
}
