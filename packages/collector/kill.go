package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Kill is one death, recorded at the tick the kill event fired. This struct IS
// the schema: KillColumns and AppendRow below must stay aligned with it.
//
// Phase is recorded because kills genuinely happen outside live play - a
// player shot after the win condition is met still produces a Kill event, and
// those deaths carry no analytical weight. Filter on phase = 'live' for
// anything that is meant to describe the round being contested.
//
// KillerSteamID is 0 for world damage: falling, the bomb, or a demo corrupt
// enough that demoinfocs reports an unconnected attacker. AssisterSteamID is 0
// when nobody assisted. Both are distinguishable from a real steamid, which is
// never zero. VictimSteamID is never 0 - a kill with no victim is dropped at
// the collector rather than written as a row that joins to nothing.
//
// Teams are stored for both sides rather than a derived is_teamkill flag, so
// consumers can compute team kills, trades, and side-relative stats without
// re-joining round_players.
//
// Duplicate-death caveat: a (round, victim) pair is USUALLY unique but is not
// guaranteed to be. demoinfocs can fire a second Kill for a player who is
// already dead, at the exact tick RoundEnd fires. Observed once in 613 live
// kills across three demos (mega-OT Mirage round 11: an AWP death at tick
// 69439, then a second event at tick 72349 = end_tick exactly), and the tick
// stream confirms only one is_alive transition, so the first event is the real
// death and the second is spurious.
//
// These rows are written rather than filtered, because suppressing them would
// mean tracking live/dead state in the collector and silently discarding
// events on a guess. Any consumer counting deaths should either use
// round_players.Survived, or deduplicate on (round, victim) keeping the
// earliest tick.
type Kill struct {
	Round int32
	Tick  int32 // ingame tick, not demo frame
	Phase Phase

	KillerSteamID uint64 // 0 for world damage
	KillerTeam    common.Team
	VictimSteamID uint64
	VictimTeam    common.Team

	AssisterSteamID uint64 // 0 when unassisted
	AssistedFlash   bool   // the assist was a flash, not damage

	Weapon string

	Headshot bool
	// Penetrated counts objects the bullet passed through - a wallbang is
	// Penetrated > 0. demoinfocs reports this as an int; it is stored narrow
	// because the observed range is 0-2.
	Penetrated    int16
	NoScope       bool
	ThroughSmoke  bool
	AttackerBlind bool

	// Distance is in Hammer units, straight-line between the two players.
	Distance float32
}

// KillColumns is the CSV header for kills.csv, in AppendRow's emit order.
func KillColumns() []string {
	return []string{
		"round", "tick", "phase",
		"killer_steamid", "killer_team", "victim_steamid", "victim_team",
		"assister_steamid", "assisted_flash",
		"weapon", "headshot", "penetrated", "no_scope", "through_smoke",
		"attacker_blind", "distance",
	}
}

// AppendRow appends this kill's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (k *Kill) AppendRow(dst []string) []string {
	return append(dst,
		i32(k.Round),
		i32(k.Tick),
		k.Phase.String(),
		strconv.FormatUint(k.KillerSteamID, 10),
		strconv.Itoa(int(k.KillerTeam)),
		strconv.FormatUint(k.VictimSteamID, 10),
		strconv.Itoa(int(k.VictimTeam)),
		strconv.FormatUint(k.AssisterSteamID, 10),
		b(k.AssistedFlash),
		k.Weapon,
		b(k.Headshot),
		strconv.Itoa(int(k.Penetrated)),
		b(k.NoScope),
		b(k.ThroughSmoke),
		b(k.AttackerBlind),
		f32(k.Distance),
	)
}
