package main

import (
	"fmt"
	"math"
	"strings"
)

// Input bits of the ticks.csv buttons mask that the overlay draws. See the
// buttons table in docs/round-collector-schema.md.
const (
	btnAttack  = 0x1
	btnJump    = 0x2
	btnDuck    = 0x4
	btnForward = 0x8
	btnBack    = 0x10
	btnLeft    = 0x200
	btnRight   = 0x400
	btnAttack2 = 0x800
	btnWalk    = 0x10000
)

// Sample is one tick of the followed player, from ticks.csv.
type Sample struct {
	Tick    int32
	Buttons uint64
	Alive   bool
}

// key is one box of the overlay: where it sits, what it says, and which
// button lights it.
type key struct {
	label      string
	bit        uint64
	x, y, w, h int // relative to the overlay's top left
}

// Layout, in pixels of a 1920x1080 frame. Movement keys on the left, mouse
// buttons on the right, the way a player's hands sit.
//
// There is no aim indicator. An earlier version drew a ring whose dot moved
// with how fast the view turned; on a POV video the camera already shows the
// aim, so it only added noise.
const (
	unit = 54
	gap  = 6
)

var keys = []key{
	{"W", btnForward, unit + gap, 0, unit, unit},
	{"A", btnLeft, 0, unit + gap, unit, unit},
	{"S", btnBack, unit + gap, unit + gap, unit, unit},
	{"D", btnRight, 2 * (unit + gap), unit + gap, unit, unit},
	{"WALK", btnWalk, 0, 2 * (unit + gap), unit + gap/2 + unit/2, unit * 3 / 4},
	{"DUCK", btnDuck, unit + gap/2 + unit/2 + gap, 2 * (unit + gap), 3*unit + 2*gap - (unit + gap/2 + unit/2 + gap), unit * 3 / 4},
	{"JUMP", btnJump, 0, 2*(unit+gap) + unit*3/4 + gap, 3*unit + 2*gap, unit * 3 / 4},
	{"M1", btnAttack, 3*(unit+gap) + 2*gap, 0, unit, unit},
	{"M2", btnAttack2, 4*(unit+gap) + 2*gap, 0, unit, unit},
}

// Options place and time the overlay.
type Options struct {
	X, Y      int     // top left of the overlay on a 1920x1080 frame
	TickRate  float64 // demo ticks a second
	StartTick int32   // the tick at the video's first frame
}

// interval is one stretch of video time in seconds.
type interval struct{ from, to float64 }

// ASS renders the overlay for samples (sorted by tick, one per tick) as an
// Advanced SubStation subtitle script, for FFmpeg's ass filter to burn in.
//
// Each key is drawn as an outline that stays up the whole clip plus a filled
// box for every stretch it is held, so the script grows with how often keys
// change, not with the clip's length in frames.
func ASS(samples []Sample, opt Options) string {
	var b strings.Builder
	b.WriteString(assHeader)
	if len(samples) == 0 {
		return b.String()
	}
	at := func(tick int32) float64 { return float64(tick-opt.StartTick) / opt.TickRate }
	clipEnd := at(samples[len(samples)-1].Tick) + 1/opt.TickRate

	// The buttons column lags the rest of the row by one tick (the schema's
	// trap 2), so the mask for tick t is read off the row for t+1.
	pressed := func(i int) uint64 {
		if i+1 < len(samples) && samples[i+1].Tick == samples[i].Tick+1 {
			return samples[i+1].Buttons
		}
		return samples[i].Buttons
	}
	alive := aliveIntervals(samples, at, clipEnd)

	for _, k := range keys {
		for _, iv := range alive {
			event(&b, 0, iv, box(opt.X+k.x, opt.Y+k.y, k.w, k.h, "&H000000&", "&H70&"))
			event(&b, 2, iv, label(opt.X+k.x+k.w/2, opt.Y+k.y+k.h/2, k.label, k.h))
		}
		for _, iv := range heldIntervals(samples, pressed, k.bit, at, clipEnd) {
			event(&b, 1, iv, box(opt.X+k.x, opt.Y+k.y, k.w, k.h, "&H3FC8FF&", "&H20&"))
		}
	}
	return b.String()
}

// aliveIntervals is when the player is alive: a dead player's rows keep
// their last buttons (the schema's trap 1), so the overlay is hidden then.
func aliveIntervals(samples []Sample, at func(int32) float64, end float64) []interval {
	var out []interval
	start := -1.0
	for _, s := range samples {
		switch {
		case s.Alive && start < 0:
			start = at(s.Tick)
		case !s.Alive && start >= 0:
			out = append(out, interval{start, at(s.Tick)})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, interval{start, end})
	}
	return out
}

func heldIntervals(samples []Sample, pressed func(int) uint64, bit uint64, at func(int32) float64, end float64) []interval {
	var out []interval
	start := -1.0
	for i, s := range samples {
		down := s.Alive && pressed(i)&bit != 0
		switch {
		case down && start < 0:
			start = at(s.Tick)
		case !down && start >= 0:
			out = append(out, interval{start, at(s.Tick)})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, interval{start, end})
	}
	return out
}

const assHeader = `[Script Info]
ScriptType: v4.00+
PlayResX: 1920
PlayResY: 1080
WrapStyle: 2
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Key,Arial,24,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,1,0,0,0,100,100,0,0,1,2,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

func event(b *strings.Builder, layer int, iv interval, text string) {
	if iv.to <= iv.from {
		return
	}
	fmt.Fprintf(b, "Dialogue: %d,%s,%s,Key,,0,0,0,,%s\n", layer, assTime(iv.from), assTime(iv.to), text)
}

// assTime formats seconds as ASS's h:mm:ss.cc.
func assTime(sec float64) string {
	cs := int(math.Round(max(sec, 0) * 100))
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}

func box(x, y, w, h int, fill, alpha string) string {
	return fmt.Sprintf(`{\an7\pos(%d,%d)\p1\bord2\shad0\1c%s\1a%s\3c&HFFFFFF&\3a&H40&}m 0 0 l %d 0 %d %d 0 %d{\p0}`,
		x, y, fill, alpha, w, w, h, h)
}

func label(cx, cy int, text string, h int) string {
	size := 22
	if len(text) > 2 {
		size = 16
	}
	size = min(size, h/2)
	return fmt.Sprintf(`{\an5\pos(%d,%d)\fs%d\bord2\shad0}%s`, cx, cy, size, text)
}
