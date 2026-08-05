package collector

import (
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Round is one fully materialized round: metadata plus every sampled tick for
// every player, in tick order.
//
// Memory: unsafe.Sizeof(PlayerTick) is 144 bytes. A typical ~60k-row round is
// therefore roughly 8.6 MB; the largest round observed against
// mega_ot_mirage.dem (round 7, 122,872 rows) is roughly 17.7 MB. Both figures
// are for the Ticks slice alone and can run transiently ~2x higher while the
// slice grows and reallocates (append does not shrink the old backing array
// until it's garbage collected).
type Round struct {
	Meta  RoundMeta
	Ticks []PlayerTick
}

// RoundMeta is one round's metadata: its boundary ticks, outcome, and the
// per-player economy/survival roster (Players). Everything here is available
// without walking Ticks, so consumers that only need round-level facts (e.g.
// building rounds.csv or round_players.csv) never have to touch the
// potentially-large Ticks slice.
type RoundMeta struct {
	Number          int32
	StartTick       int32
	FreezeEndTick   int32
	EndTick         int32 // RoundEnd - win condition met
	OfficialEndTick int32 // RoundEndOfficial - players frozen for next round

	Winner common.Team
	Reason events.RoundEndReason

	// TimeoutBefore records whether a tactical timeout preceded this round.
	// demoinfocs has no timeout support (see datatables.go:1279); these come
	// from raw properties on the game rules entity.
	TimeoutBefore bool
	TimeoutTeam   common.Team

	// Complete is false when the demo ended before RoundEndOfficial fired.
	// Consumers should treat incomplete rounds as truncated, not as losses.
	Complete bool

	Players []PlayerRound
}

// PlayerRound is one player's per-round summary, snapshotted at freezetime end
// so it reflects what they actually took into the round.
//
// Survived is read from Participants().Playing() at RoundEnd: true when the
// player was present and alive, false when present and dead OR absent
// entirely. Those two false cases are NOT the same thing - a player who
// disconnects mid-round also reads false, indistinguishable from a kill,
// unless Disconnected is checked alongside it. Disconnected is the
// disambiguator: true means this player was not a connected participant at
// RoundEnd, so Survived's false here is "unknown" rather than "died"; false
// means Survived reflects an actual observation. (A player who is alive at
// RoundEnd but then dies during postround, before RoundEndOfficial, is
// correctly recorded Survived=true - that is intentional, not a bug: they
// did survive the round itself.)
//
// JoinedLate is true for a player who was not part of the freezetime economy
// snapshot (snapshotEconomy runs once, at RoundFreezetimeEnd) but who
// nonetheless appears in this round's tick stream - i.e. they connected
// after freezetime had already ended. Their MoneyAtFreezeEnd and
// EquivValueAtFreezeEnd are zeroed rather than sampled, since there is no
// freezetime-end snapshot for them to read.
type PlayerRound struct {
	SteamID               uint64
	Team                  common.Team
	MoneyAtFreezeEnd      int32
	EquipValueAtFreezeEnd int32
	Survived              bool
	Disconnected          bool
	JoinedLate            bool
}

// RoundColumns is the CSV header for rounds.csv.
func RoundColumns() []string {
	return []string{
		"number", "start_tick", "freeze_end_tick", "end_tick", "official_end_tick",
		"winner", "reason", "timeout_before", "timeout_team", "complete", "tick_rows",
	}
}

// RoundPlayerColumns is the CSV header for round_players.csv.
func RoundPlayerColumns() []string {
	return []string{
		"round", "steamid", "team",
		"money_at_freeze_end", "equip_value_at_freeze_end", "survived",
		"disconnected", "joined_late",
	}
}
