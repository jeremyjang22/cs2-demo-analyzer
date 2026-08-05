package collector

import (
	"math"
	"testing"
)

func TestFirstSampleIsInvalid(t *testing.T) {
	v := newVelocityTracker(64)
	_, _, _, _, valid := v.compute(7, 100, 0, 0, 0)
	if valid {
		t.Error("valid = true on first sample, want false (no predecessor)")
	}
}

func TestVelocityOverOneTick(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	// 10 units in 1 tick at 64 tick/s -> 640 units/sec
	vx, vy, vz, speed, valid := v.compute(7, 101, 10, 0, 0)
	if !valid {
		t.Fatal("valid = false on second sample, want true")
	}
	if math.Abs(float64(vx)-640) > 0.01 {
		t.Errorf("vx = %v, want 640", vx)
	}
	if vy != 0 || vz != 0 {
		t.Errorf("vy, vz = %v, %v, want 0, 0", vy, vz)
	}
	if math.Abs(float64(speed)-640) > 0.01 {
		t.Errorf("speed = %v, want 640", speed)
	}
}

// Speed must exclude Z, or a player jumping straight up reads as "moving fast"
// and any counterstrafe check based on it breaks.
func TestSpeedExcludesVertical(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	_, _, vz, speed, _ := v.compute(7, 101, 0, 0, 5)
	if speed != 0 {
		t.Errorf("speed = %v for purely vertical movement, want 0", speed)
	}
	if math.Abs(float64(vz)-320) > 0.01 {
		t.Errorf("vz = %v, want 320", vz)
	}
}

func TestMultiTickGapScalesCorrectly(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	// 20 units over 2 ticks = same 640 units/sec as 10 units over 1 tick
	vx, _, _, _, _ := v.compute(7, 102, 20, 0, 0)
	if math.Abs(float64(vx)-640) > 0.01 {
		t.Errorf("vx = %v, want 640", vx)
	}
}

func TestPlayersTrackedIndependently(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	v.compute(8, 100, 500, 500, 0)
	vx, _, _, _, valid := v.compute(7, 101, 10, 0, 0)
	if !valid || math.Abs(float64(vx)-640) > 0.01 {
		t.Errorf("player 7 vx = %v (valid=%v), want 640, true", vx, valid)
	}
}

func TestResetInvalidatesEveryone(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	v.reset()
	_, _, _, _, valid := v.compute(7, 101, 10, 0, 0)
	if valid {
		t.Error("valid = true after reset, want false")
	}
}

func TestForgetInvalidatesOnePlayer(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	v.compute(8, 100, 0, 0, 0)
	v.forget(7)

	if _, _, _, _, valid := v.compute(7, 101, 10, 0, 0); valid {
		t.Error("player 7 valid = true after forget, want false")
	}
	if _, _, _, _, valid := v.compute(8, 101, 10, 0, 0); !valid {
		t.Error("player 8 valid = false, want true (forget must not affect others)")
	}
}

// A repeated tick would divide by zero.
func TestSameTickIsInvalid(t *testing.T) {
	v := newVelocityTracker(64)
	v.compute(7, 100, 0, 0, 0)
	_, _, _, _, valid := v.compute(7, 100, 10, 0, 0)
	if valid {
		t.Error("valid = true for a zero-duration sample, want false")
	}
}

// SetTickRate must actually change the dt used in compute(), not just be
// accepted and ignored - otherwise every velocity value computed before the
// real CSVCMsg_ServerInfo tick rate arrives (and after, if the field were
// unused) stays silently wrong for any demo that isn't 64-tick.
func TestSetTickRateCorrectsFallback(t *testing.T) {
	v := newVelocityTracker(-1) // constructor falls back to the hardcoded 64
	v.compute(7, 100, 0, 0, 0)
	v.SetTickRate(128) // real rate arrives mid-parse, corrects the fallback
	// 10 units in 1 tick at 128 tick/s -> 1280 units/sec (would be 640 at
	// the uncorrected fallback of 64).
	vx, _, _, speed, valid := v.compute(7, 101, 10, 0, 0)
	if !valid {
		t.Fatal("valid = false, want true")
	}
	if math.Abs(float64(vx)-1280) > 0.01 {
		t.Errorf("vx = %v, want 1280 (SetTickRate must affect subsequent compute() calls)", vx)
	}
	if math.Abs(float64(speed)-1280) > 0.01 {
		t.Errorf("speed = %v, want 1280", speed)
	}
}

// A not-yet-known reading (<= 0, e.g. a demo whose ServerInfo carries no
// tick_interval) must never downgrade an already-installed rate.
func TestSetTickRateIgnoresNonPositive(t *testing.T) {
	v := newVelocityTracker(64)
	v.SetTickRate(0)
	v.SetTickRate(-1)
	v.compute(7, 100, 0, 0, 0)
	vx, _, _, _, valid := v.compute(7, 101, 10, 0, 0)
	if !valid {
		t.Fatal("valid = false, want true")
	}
	// Still 640 (10 units / 1 tick at 64 tick/s): the rate must be unchanged.
	if math.Abs(float64(vx)-640) > 0.01 {
		t.Errorf("vx = %v, want 640 (non-positive SetTickRate calls must be ignored)", vx)
	}
}
