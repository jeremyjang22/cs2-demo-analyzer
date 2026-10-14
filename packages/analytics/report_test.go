package analytics

import (
	"reflect"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

const (
	tSide  = common.TeamTerrorists
	ctSide = common.TeamCounterTerrorists
)

// round builds a decided round. Players are alive at freeze end; survivors
// lists who was still alive when it was won.
func round(number int32, winner common.Team, ts, cts, survivors []uint64, kills ...collector.Kill) Round {
	alive := make(map[uint64]bool)
	for _, id := range survivors {
		alive[id] = true
	}
	r := Round{Meta: collector.RoundMeta{Number: number, FreezeEndTick: 10, Winner: winner}, Kills: kills}
	for i := range r.Kills {
		r.Kills[i].Round = number
	}
	for _, id := range ts {
		r.Meta.Players = append(r.Meta.Players, collector.PlayerRound{SteamID: id, Team: tSide, Survived: alive[id]})
	}
	for _, id := range cts {
		r.Meta.Players = append(r.Meta.Players, collector.PlayerRound{SteamID: id, Team: ctSide, Survived: alive[id]})
	}
	return r
}

func kill(tick int32, killer uint64, killerTeam common.Team, victim uint64, victimTeam common.Team) collector.Kill {
	return collector.Kill{
		Tick: tick, Phase: collector.PhaseLive, Weapon: "AK-47",
		KillerSteamID: killer, KillerTeam: killerTeam, VictimSteamID: victim, VictimTeam: victimTeam,
	}
}

func report(rounds ...Round) *Report {
	r := NewReport()
	r.Add(&Match{Name: "demo", Rounds: rounds}, nil)
	return r
}

// counts lists the player counts one team passed through, as "own v enemy".
func counts(r *Report, team string) [][2]int {
	var out [][2]int
	for _, s := range r.Situations {
		if s.Team == team {
			out = append(out, [2]int{len(s.Own), len(s.Enemy)})
		}
	}
	return out
}

func TestClutchIsWonByTheRoundNotByTheKills(t *testing.T) {
	r := report(round(1, tSide, []uint64{1, 2}, []uint64{11, 12}, []uint64{2},
		kill(100, 11, ctSide, 1, tSide),
		kill(200, 2, tSide, 11, ctSide),
		kill(300, 2, tSide, 12, ctSide),
	))

	if got, want := counts(r, "demo/T-start"), [][2]int{{2, 2}, {1, 2}, {1, 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("T counts = %v, want %v", got, want)
	}
	if len(r.Clutches) != 2 {
		t.Fatalf("got %d clutches, want the T's 1v2 and the CT's 1v1", len(r.Clutches))
	}
	won, lost := r.Clutches[0], r.Clutches[1]
	if won.Player != 2 || len(won.Enemies) != 2 || !won.Won || won.Kills != 2 || !won.Survived {
		t.Errorf("T clutch = %+v, want player 2 winning a 1v2 with 2 kills, alive", won)
	}
	if lost.Player != 12 || len(lost.Enemies) != 1 || lost.Won || lost.Kills != 0 {
		t.Errorf("CT clutch = %+v, want player 12 losing a 1v1", lost)
	}

	// The opening kill went to the side that lost the round.
	if o := r.Openings[0]; o.Killer != 11 || o.KillerTeam != "demo/CT-start" || o.KillerTeamWon {
		t.Errorf("opening = %+v, want player 11's, on the losing team", o)
	}
}

// Two deaths on one tick are one transition: nobody ever played the 3v2.
func TestSameTickDeathsDoNotInventACount(t *testing.T) {
	r := report(round(1, tSide, []uint64{1, 2, 3}, []uint64{11, 12, 13}, []uint64{1, 2, 3, 13},
		kill(100, 1, tSide, 11, ctSide),
		kill(100, 1, tSide, 12, ctSide),
	))
	if got, want := counts(r, "demo/T-start"), [][2]int{{3, 3}, {3, 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("T counts = %v, want %v", got, want)
	}
}

func TestOnlyEnemyKillsAreCredited(t *testing.T) {
	teamkill := kill(50, 1, tSide, 2, tSide)
	world := collector.Kill{Tick: 60, Phase: collector.PhaseLive, Weapon: "World", VictimSteamID: 3, VictimTeam: tSide}
	real := kill(70, 11, ctSide, 1, tSide)
	// demoinfocs' spurious repeat of an existing death at the RoundEnd tick.
	repeat := kill(90, 12, ctSide, 1, tSide)
	// After the win condition: on the scoreboard, but not part of the round.
	late := kill(95, 12, ctSide, 4, tSide)
	late.Phase = collector.PhasePostRound

	r := report(round(1, ctSide, []uint64{1, 2, 3, 4}, []uint64{11, 12}, []uint64{4, 11, 12},
		teamkill, world, real, repeat, late))

	if o := r.Openings[0]; o.Killer != 11 || o.Tick != 70 {
		t.Errorf("opening = %+v, want the first ENEMY kill, at tick 70", o)
	}
	if got := r.teams["demo/T-start"]; got.deaths != 4 || got.kills != 0 {
		t.Errorf("T team = %+v, want 4 deaths and no kills credited for the team kill", got)
	}
	if got := r.players[playerKey{"demo/CT-start", 12}].kills; got != 1 {
		t.Errorf("player 12 kills = %d, want 1: the postround kill, not the repeat", got)
	}
	// 4v2, then the team kill, the world death and the real kill each take a
	// T. The postround death changes nothing: the round was already over.
	if got, want := counts(r, "demo/T-start"), [][2]int{{4, 2}, {3, 2}, {2, 2}, {1, 2}}; !reflect.DeepEqual(got, want) {
		t.Errorf("T counts = %v, want %v", got, want)
	}
}

// Player 3 started the round, did not survive it, and never died: they
// disconnected at an unknown tick, so no count from this round is trustworthy.
func TestRoundWithAnUnexplainedAbsenceHasNoSituations(t *testing.T) {
	r := report(round(1, tSide, []uint64{1, 2, 3}, []uint64{11, 12}, []uint64{1, 2},
		kill(100, 1, tSide, 11, ctSide),
		kill(200, 1, tSide, 12, ctSide),
	))
	if len(r.Situations) != 0 || len(r.Clutches) != 0 {
		t.Errorf("got %d situations and %d clutches, want none", len(r.Situations), len(r.Clutches))
	}
	if len(r.Openings) != 1 || r.Demos[0].RoundsUncertain != 1 {
		t.Errorf("openings = %d, uncertain = %d; want the opening kept and the round counted as uncertain",
			len(r.Openings), r.Demos[0].RoundsUncertain)
	}
}

func TestRoundWithoutAWinnerIsSkipped(t *testing.T) {
	cut := round(2, common.TeamUnassigned, []uint64{1}, []uint64{11}, nil, kill(100, 1, tSide, 11, ctSide))
	r := report(round(1, tSide, []uint64{1}, []uint64{11}, []uint64{1, 11}), cut)

	if d := r.Demos[0]; d.Rounds != 1 || d.RoundsSkipped != 1 {
		t.Errorf("rounds = %d, skipped = %d; want 1 and 1", d.Rounds, d.RoundsSkipped)
	}
	if got := r.players[playerKey{"demo/T-start", 1}]; got.rounds != 1 || got.kills != 0 {
		t.Errorf("player 1 = %+v, want nothing counted from the cut-off round", got)
	}
}

func TestPlantIsRecordedOnCountsReachedAfterIt(t *testing.T) {
	rd := round(1, tSide, []uint64{1, 2}, []uint64{11, 12}, []uint64{1, 2, 12},
		kill(300, 1, tSide, 11, ctSide))
	rd.Bomb = []collector.BombSample{{Tick: 200, State: collector.BombPlanted}}
	r := report(rd)

	var planted []bool
	for _, s := range r.Situations {
		if s.Side == sideT {
			planted = append(planted, s.Planted)
		}
	}
	if want := []bool{false, true}; !reflect.DeepEqual(planted, want) {
		t.Errorf("planted = %v, want %v (2v2 before the plant, 2v1 after)", planted, want)
	}
}
