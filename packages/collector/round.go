package collector

import (
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Round is one fully materialized round: metadata plus every sampled tick for
// every player, in tick order. Peak memory is roughly 14 MB for a normal round.
type Round struct {
	Meta  RoundMeta
	Ticks []PlayerTick
}

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
type PlayerRound struct {
	SteamID               uint64
	Team                  common.Team
	MoneyAtFreezeEnd      int32
	EquipValueAtFreezeEnd int32
	Survived              bool
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
	}
}
