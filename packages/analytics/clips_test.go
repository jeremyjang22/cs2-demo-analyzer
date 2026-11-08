package analytics

import (
	"slices"
	"testing"
)

// A player opens the round, the teammate dies, and the player finishes a 3k
// as a 1v1 clutch. The three moments overlap, so by default they are one
// recording; with NoMerge they are three.
func TestClipsMergesOverlappingMoments(t *testing.T) {
	const me = steamID64Base + 42
	rd := round(3, tSide, []uint64{me, 2}, []uint64{11, 12, 13}, []uint64{me},
		kill(1000, me, tSide, 11, ctSide), kill(1300, me, tSide, 12, ctSide),
		kill(1400, 13, ctSide, 2, tSide), kill(1600, me, tSide, 13, ctSide))
	rd.Meta.EndTick, rd.Meta.OfficialEndTick = 1700, 1750
	m := &Match{Name: "demo", Map: "de_mirage", TickRate: 64, Rounds: []Round{rd},
		Names: map[uint64]string{me: "me", 11: "a"}}
	opt := ClipOptions{Players: map[uint64]bool{me: true}, Before: 1, After: 1}

	got := Clips(m, opt)
	if len(got) != 1 {
		t.Fatalf("got %d clips, want one merged clip: %+v", len(got), got)
	}
	c := got[0]
	if !slices.Equal(c.Kinds, []string{"opening", "multikill", "clutch"}) {
		t.Errorf("kinds = %v", c.Kinds)
	}
	// From a second before the opening to round end plus a second, capped at
	// the official end.
	if c.StartTick != 1000-64 || c.EndTick != 1750 {
		t.Errorf("range = %d..%d, want 936..1750", c.StartTick, c.EndTick)
	}
	if c.AccountID != 42 || c.ID != "demo_r03_42_opening+multikill+clutch" {
		t.Errorf("account id %d, id %q", c.AccountID, c.ID)
	}
	if !slices.Equal(c.EventTicks, []int32{1000, 1300, 1400, 1600}) {
		t.Errorf("event ticks = %v", c.EventTicks)
	}

	opt.NoMerge = true
	got = Clips(m, opt)
	if len(got) != 3 {
		t.Fatalf("NoMerge: got %d clips, want 3", len(got))
	}
	if cl := got[2]; cl.Kinds[0] != ClipClutch || cl.StartTick != 1400-64 {
		t.Errorf("clutch = %+v, want it to start a second before the teammate died", cl)
	}

	// The other team's players are not followed.
	if got := Clips(m, ClipOptions{Players: map[uint64]bool{11: true}}); len(got) != 0 {
		t.Errorf("player 11 got clips %+v, want none", got)
	}
}

func TestClipsRoundKindAndNoMerge(t *testing.T) {
	const me = steamID64Base + 1
	rd := round(1, tSide, []uint64{me}, []uint64{11}, []uint64{me}, kill(500, me, tSide, 11, ctSide))
	rd.Meta.EndTick = 600
	m := &Match{Name: "d", TickRate: 64, Rounds: []Round{rd}}

	got := Clips(m, ClipOptions{Kinds: []string{ClipRound, ClipOpening}, NoMerge: true, Before: 2})
	if len(got) != 3 {
		t.Fatalf("got %d clips, want a round clip for each player and one opening: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Kinds[0] == ClipRound && (c.StartTick != 10 || c.EndTick != 600) {
			t.Errorf("round clip %+v, want freeze end (10) to round end (600)", c)
		}
	}
}
