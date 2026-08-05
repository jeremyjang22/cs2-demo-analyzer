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
}

func newAssembler(emit func(*Round)) *assembler {
	return &assembler{emit: emit}
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
	a.emit(r)
}

func (a *assembler) appendTick(t PlayerTick) {
	if a.cur == nil {
		return
	}
	a.cur.Ticks = append(a.cur.Ticks, t)
}

func (a *assembler) setTimeout(team common.Team) {
	if a.cur == nil {
		return
	}
	a.cur.Meta.TimeoutBefore = true
	a.cur.Meta.TimeoutTeam = team
}
