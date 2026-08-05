package collector

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// includeParticipant guards both sampleFrame and snapshotEconomy against the
// phantom SteamID-0 "Crew" participant demoinfocs' Participants().Playing()
// can return, and against nil defensively.
func TestIncludeParticipant(t *testing.T) {
	cases := []struct {
		name string
		p    *common.Player
		want bool
	}{
		{"nil", nil, false},
		{"steamid zero (phantom participant)", &common.Player{SteamID64: 0}, false},
		{"real player", &common.Player{SteamID64: 76561198000000123}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := includeParticipant(c.p); got != c.want {
				t.Errorf("includeParticipant(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// isAlive and isUnspawnedPawn both read m_lifeState off PlayerPawnEntity(),
// which requires a real demoinfocs entity graph (demoInfoProvider is
// unexported on common.Player, so a working PlayerPawnEntity() cannot be
// constructed from outside the demoinfocs module without pulling in testify,
// which this project's dependency constraint forbids). The branch that
// actually reads m_lifeState is therefore verified against the real demo in
// packages/round-collector's regeneration step (see C2's verification in the
// fix report), not here. What IS unit-testable, and what these two guard, is
// the "no pawn at all" fallback both functions must take safely.

// isAlive must fall back to p.IsAlive() - not silently report alive - when
// there is no pawn entity to read m_lifeState from.
func TestIsAliveFallsBackWithNoPawnEntity(t *testing.T) {
	p := &common.Player{SteamID64: 7}
	if got, want := isAlive(p), p.IsAlive(); got != want {
		t.Errorf("isAlive() = %v, want %v (must fall back to p.IsAlive() with no pawn entity)", got, want)
	}
}

// isUnspawnedPawn must never flag a participant with no pawn entity at all
// as an unspawned-pawn phantom - there's nothing to sample-gate on.
func TestIsUnspawnedPawnFalseWithNoPawnEntity(t *testing.T) {
	p := &common.Player{SteamID64: 7}
	if isUnspawnedPawn(p) {
		t.Error("isUnspawnedPawn = true with no pawn entity, want false")
	}
}

// demoinfocs dispatches a FrameDone for DEM_FullPacket as well as DEM_Packet;
// both can carry the same ingame tick, and without suppression that duplicates
// every (round, tick, steamid) row sampled on such a tick.
func TestSampleGuardSuppressesRepeatedTick(t *testing.T) {
	var g sampleGuard
	if !g.shouldSample(100) {
		t.Fatal("first call for a new tick returned false, want true")
	}
	if g.shouldSample(100) {
		t.Error("second call for the same tick returned true, want false (duplicate FrameDone dispatch must be suppressed)")
	}
	if !g.shouldSample(101) {
		t.Error("call for a genuinely new tick returned false, want true")
	}
}

// reset() runs at round boundaries. Ticks are globally monotonic across a
// demo so this is defensive, but it must not leave the guard permanently
// stuck refusing every future tick.
func TestSampleGuardResetAllowsSamplingAgain(t *testing.T) {
	var g sampleGuard
	g.shouldSample(100)
	g.reset()
	if !g.shouldSample(100) {
		t.Error("shouldSample after reset returned false, want true (reset must clear the guard)")
	}
}

// fakeSink is a minimal collector.Sink that just records what it's given,
// used to drive emitRound without a real demoinfocs parser.
type fakeSink struct {
	rounds []*Round
}

func (f *fakeSink) Round(r *Round) error { f.rounds = append(f.rounds, r); return nil }
func (f *fakeSink) Close() error         { return nil }

// OnRound's doc comment promises "registers an extra consumer" (additive),
// so a second registration must not discard the first.
func TestOnRoundAppendsMultipleConsumers(t *testing.T) {
	c := &Collector{sink: &fakeSink{}}

	var calledA, calledB bool
	c.OnRound(func(*Round) { calledA = true })
	c.OnRound(func(*Round) { calledB = true })

	c.emitRound(&Round{})

	if !calledA {
		t.Error("first OnRound consumer was not called")
	}
	if !calledB {
		t.Error("second OnRound consumer was not called (OnRound must append, not replace)")
	}
}

// onTimeoutCalled is the rising-edge handler pollTimeout calls. When a round
// is already open (the case actually observed against mega_ot_mirage.dem -
// demoinfocs fires RoundStart well before that round's freezetime elapses,
// so a timeout's flag typically goes high after the enclosing round has
// already started), the timeout must land on that round immediately rather
// than waiting for a RoundStart that would attribute it one round too late.
func TestOnTimeoutCalledAppliesImmediatelyWhenRoundOpen(t *testing.T) {
	var got []*Round
	c := &Collector{asm: newAssembler(func(r *Round) { got = append(got, r) })}
	c.asm.roundStart(1000, 1)

	c.onTimeoutCalled(common.TeamTerrorists)

	if c.havePendingTimeout {
		t.Error("havePendingTimeout = true, want false (should apply immediately, not queue, when a round is open)")
	}
	c.asm.freezeEnd(2000, nil)
	c.asm.roundEndOfficial(2300)
	if len(got) != 1 {
		t.Fatalf("emitted %d rounds, want 1", len(got))
	}
	if !got[0].Meta.TimeoutBefore || got[0].Meta.TimeoutTeam != common.TeamTerrorists {
		t.Errorf("round meta = %+v, want TimeoutBefore=true TimeoutTeam=T", got[0].Meta)
	}
}

// If the flag rises with no round open yet - the ordering the original
// design doc assumed - the team must be queued rather than dropped, since
// asm.setTimeout is itself a no-op with no round open.
func TestOnTimeoutCalledQueuesWhenNoRoundOpen(t *testing.T) {
	c := &Collector{asm: newAssembler(func(*Round) {})}

	c.onTimeoutCalled(common.TeamCounterTerrorists)

	if !c.havePendingTimeout {
		t.Fatal("havePendingTimeout = false, want true (timeout observed with no round open must be queued)")
	}
	if c.pendingTimeoutTeam != common.TeamCounterTerrorists {
		t.Errorf("pendingTimeoutTeam = %v, want CT", c.pendingTimeoutTeam)
	}
}

// consumePendingTimeout is what the RoundStart handler calls to apply a
// queued timeout to the round that just opened, and it must clear the queue
// so the same timeout is never attributed to a second round.
func TestConsumePendingTimeoutAppliesOnceThenClears(t *testing.T) {
	var got []*Round
	c := &Collector{asm: newAssembler(func(r *Round) { got = append(got, r) })}
	c.onTimeoutCalled(common.TeamTerrorists) // no round open yet: queues

	c.asm.roundStart(1000, 1)
	c.consumePendingTimeout()
	c.asm.freezeEnd(2000, nil)
	c.asm.roundEndOfficial(2300)

	if c.havePendingTimeout {
		t.Error("havePendingTimeout = true after consumePendingTimeout, want false")
	}
	if !got[0].Meta.TimeoutBefore || got[0].Meta.TimeoutTeam != common.TeamTerrorists {
		t.Errorf("round 1 meta = %+v, want TimeoutBefore=true TimeoutTeam=T", got[0].Meta)
	}

	// A second round opening after the queue was cleared must NOT inherit
	// the first round's timeout.
	c.asm.roundStart(3000, 2)
	c.consumePendingTimeout()
	c.asm.freezeEnd(4000, nil)
	c.asm.roundEndOfficial(4300)

	if len(got) != 2 {
		t.Fatalf("emitted %d rounds, want 2", len(got))
	}
	if got[1].Meta.TimeoutBefore {
		t.Error("round 2 TimeoutBefore = true, want false (one timeout must not attribute to multiple rounds)")
	}
}

// consumePendingTimeout with nothing queued must not mark the round.
func TestConsumePendingTimeoutNoOpWhenNothingPending(t *testing.T) {
	var got []*Round
	c := &Collector{asm: newAssembler(func(r *Round) { got = append(got, r) })}
	c.asm.roundStart(1000, 1)

	c.consumePendingTimeout()

	c.asm.freezeEnd(2000, nil)
	c.asm.roundEndOfficial(2300)
	if got[0].Meta.TimeoutBefore {
		t.Error("TimeoutBefore = true, want false when nothing was pending")
	}
}

// SetTickRate must reach the velocity tracker the Collector was constructed
// with, not a copy - otherwise wiring it up from main.go would be a no-op.
func TestCollectorSetTickRateReachesVelocityTracker(t *testing.T) {
	c := &Collector{sink: &fakeSink{}, vel: newVelocityTracker(-1)}

	c.vel.compute(7, 100, 0, 0, 0)
	c.SetTickRate(128)
	vx, _, _, _, valid := c.vel.compute(7, 101, 10, 0, 0)

	if !valid {
		t.Fatal("valid = false, want true")
	}
	if got, want := vx, float32(1280); got < want-0.01 || got > want+0.01 {
		t.Errorf("vx = %v, want %v (Collector.SetTickRate must reach the velocityTracker it was built with)", got, want)
	}
}
