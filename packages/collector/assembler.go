package collector

import (
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// assembler builds Rounds from an ordered stream of round-lifecycle calls.
//
// It deliberately knows nothing about demoinfocs parsers or entities: Collector
// translates parser events into these calls. That keeps every tricky ordering
// rule - restarts, missing official ends, stray events - testable without mocks.
type assembler struct {
	cur  *Round
	emit func(*Round)

	// phaseNow tracks progression within the open round. It is not derived
	// from tick numbers because RoundEnd can arrive at any tick.
	phaseNow Phase

	// maxRounds and emitted cap how many rounds flush() will ever emit; zero
	// maxRounds means no cap. See setMaxRounds's doc comment for why this
	// lives here rather than being left to the caller.
	maxRounds int
	emitted   int
}

func newAssembler(emit func(*Round)) *assembler {
	return &assembler{emit: emit}
}

// setMaxRounds caps the number of rounds flush() will emit. Needed because
// demoinfocs can fire a round's RoundStart back-to-back with the previous
// round's RoundEndOfficial on the same tick (see Collector.pollTimeout's doc
// comment for the same ordering quirk elsewhere): by the time a
// maxRounds-triggered parser.Cancel() actually stops parsing, round
// maxRounds+1 has often already been opened via roundStart. Collector.Run()
// unconditionally calls finish() when parsing ends, which would otherwise
// flush that extra round as "incomplete" - turning "-max-rounds N" into N+1
// rounds on disk. n <= 0 means no cap.
func (a *assembler) setMaxRounds(n int) { a.maxRounds = n }

// capReached reports whether flush() has already emitted maxRounds rounds
// (always false when no cap is set).
func (a *assembler) capReached() bool {
	return a.maxRounds > 0 && a.emitted >= a.maxRounds
}

func (a *assembler) active() bool { return a.cur != nil }

func (a *assembler) phase() Phase { return a.phaseNow }

// roundStart opens a new round. If a round is already open it was never
// officially ended - a restart - so the partial round is discarded.
func (a *assembler) roundStart(tick, number int32) {
	a.cur = &Round{Meta: RoundMeta{Number: number, StartTick: tick}}
	a.phaseNow = PhaseFreeze
}

func (a *assembler) freezeEnd(tick int32, economy []PlayerRound) {
	if a.cur == nil {
		return
	}
	a.cur.Meta.FreezeEndTick = tick
	a.cur.Meta.Players = economy
	a.phaseNow = PhaseLive
}

func (a *assembler) roundEnd(tick int32, winner common.Team, reason events.RoundEndReason) {
	if a.cur == nil {
		return
	}
	// Guard against a duplicate RoundEnd overwriting the real one.
	if a.phaseNow == PhasePostRound {
		return
	}
	a.cur.Meta.EndTick = tick
	a.cur.Meta.Winner = winner
	a.cur.Meta.Reason = reason
	a.phaseNow = PhasePostRound
}

func (a *assembler) roundEndOfficial(tick int32) {
	if a.cur == nil {
		return
	}
	a.cur.Meta.OfficialEndTick = tick
	a.cur.Meta.Complete = true
	a.flush()
}

// finish emits any round still open when parsing ends, marked incomplete.
func (a *assembler) finish() {
	if a.cur == nil {
		return
	}
	a.cur.Meta.Complete = false
	a.flush()
}

func (a *assembler) flush() {
	r := a.cur
	a.cur = nil // release before emitting so the sink owns the memory

	if a.capReached() {
		// Round maxRounds+1, opened after the cap was already hit (see
		// setMaxRounds) - discard it rather than emit an extra round.
		return
	}
	a.emitted++

	backfillRoster(r)
	a.emit(r)
}

func (a *assembler) appendTick(t PlayerTick) {
	if a.cur == nil {
		return
	}
	a.cur.Ticks = append(a.cur.Ticks, t)
}

// backfillRoster ensures every steamid present in r.Ticks has a matching
// PlayerRound in r.Meta.Players. snapshotEconomy runs once, at
// RoundFreezetimeEnd; a player who connects after that (already mid-round)
// is absent from that snapshot even though their rows are in the tick
// stream, which would otherwise silently break any INNER JOIN between
// ticks.csv.gz and round_players.csv. Backfilled entries get zeroed economy
// (never observed) and JoinedLate=true. Survived is best-effort: taken from
// the player's last sampled tick in this round rather than left at its zero
// value, since flush() runs well after markSurvivors had its one chance to
// observe live parser state. Entries are appended in order of first
// appearance in r.Ticks, so output stays deterministic rather than depending
// on map iteration order.
func backfillRoster(r *Round) {
	if r == nil {
		return
	}
	known := make(map[uint64]bool, len(r.Meta.Players))
	for _, p := range r.Meta.Players {
		known[p.SteamID] = true
	}

	type lastSeen struct {
		team  common.Team
		alive bool
	}
	late := make(map[uint64]*lastSeen)
	var order []uint64
	for i := range r.Ticks {
		t := &r.Ticks[i]
		if known[t.SteamID] {
			continue
		}
		ls, ok := late[t.SteamID]
		if !ok {
			ls = &lastSeen{}
			late[t.SteamID] = ls
			order = append(order, t.SteamID)
		}
		ls.team = t.Team
		ls.alive = t.IsAlive
	}

	for _, id := range order {
		ls := late[id]
		r.Meta.Players = append(r.Meta.Players, PlayerRound{
			SteamID:    id,
			Team:       ls.team,
			Survived:   ls.alive,
			JoinedLate: true,
		})
	}
}

func (a *assembler) setTimeout(team common.Team) {
	if a.cur == nil {
		return
	}
	a.cur.Meta.TimeoutBefore = true
	a.cur.Meta.TimeoutTeam = team
}
