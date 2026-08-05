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
