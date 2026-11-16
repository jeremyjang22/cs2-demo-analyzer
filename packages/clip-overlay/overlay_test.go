package main

import (
	"strings"
	"testing"
)

func TestHeldIntervalsReadButtonsOneTickLate(t *testing.T) {
	// W is in the mask on rows 11 and 12, which describe ticks 10 and 11.
	samples := []Sample{
		{Tick: 10, Alive: true}, {Tick: 11, Alive: true, Buttons: btnForward},
		{Tick: 12, Alive: true, Buttons: btnForward}, {Tick: 13, Alive: true},
	}
	at := func(tick int32) float64 { return float64(tick - 10) }
	pressed := func(i int) uint64 {
		if i+1 < len(samples) {
			return samples[i+1].Buttons
		}
		return samples[i].Buttons
	}
	got := heldIntervals(samples, pressed, btnForward, at, 4)
	if len(got) != 1 || got[0] != (interval{0, 2}) {
		t.Errorf("held = %v, want [{0 2}]", got)
	}
}

func TestDeadPlayerHidesOverlay(t *testing.T) {
	samples := []Sample{{Tick: 0, Alive: true}, {Tick: 64, Alive: true}, {Tick: 128, Alive: false, Buttons: btnForward}}
	got := aliveIntervals(samples, func(t int32) float64 { return float64(t) / 64 }, 3)
	if len(got) != 1 || got[0] != (interval{0, 2}) {
		t.Errorf("alive = %v, want [{0 2}]", got)
	}
}

func TestAngleDeltaWraps(t *testing.T) {
	if d := angleDelta(-179, 179); d != 2 {
		t.Errorf("angleDelta(-179, 179) = %v, want 2", d)
	}
	if d := angleDelta(10, 20); d != -10 {
		t.Errorf("angleDelta(10, 20) = %v, want -10", d)
	}
}

func TestDotOffsetCapsAtRing(t *testing.T) {
	dx, dy := dotOffset(-10000, 0)
	if dx >= 0 || dx < -(mouseR-dotR)-mouseStep || dy != 0 {
		t.Errorf("flick left = (%d, %d), want a negative x at the ring", dx, dy)
	}
	if dx, dy := dotOffset(0, 0); dx != 0 || dy != 0 {
		t.Errorf("still = (%d, %d), want centre", dx, dy)
	}
}

func TestASSTimes(t *testing.T) {
	if got := assTime(3723.456); got != "1:02:03.46" {
		t.Errorf("assTime = %q", got)
	}
	s := ASS([]Sample{{Tick: 100, Alive: true}, {Tick: 101, Alive: true, Buttons: btnJump}, {Tick: 102, Alive: true}},
		Options{TickRate: 64, StartTick: 100})
	if !strings.Contains(s, "}JUMP") || !strings.Contains(s, "Dialogue: 1,0:00:00.00,0:00:00.02") {
		t.Errorf("script lacks the JUMP label or its press:\n%s", s)
	}
}
