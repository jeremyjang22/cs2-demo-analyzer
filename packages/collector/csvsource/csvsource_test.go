package csvsource

import (
	"reflect"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
)

type collect struct{ rounds []collector.Round }

func (c *collect) Round(r *collector.Round) error { c.rounds = append(c.rounds, *r); return nil }
func (c *collect) Close() error                   { return nil }

// What csvsink writes, csvsource reads back: every table, every column, at
// the two decimals the CSVs carry. The values below are already at two
// decimals so the round trip is exact.
func TestRoundTripsThroughCSVSink(t *testing.T) {
	const a, b = uint64(76561198000000001), uint64(76561198000000002)
	round := func(n int32, base int32) *collector.Round {
		return &collector.Round{
			Meta: collector.RoundMeta{
				Number: n, StartTick: base, FreezeEndTick: base + 100, EndTick: base + 500,
				OfficialEndTick: base + 948, Winner: common.TeamTerrorists,
				Reason: events.RoundEndReasonCTWin, Complete: true,
				Players: []collector.PlayerRound{
					{SteamID: a, Team: common.TeamTerrorists, MoneyAtFreezeEnd: 800, Survived: true},
					{SteamID: b, Team: common.TeamCounterTerrorists, JoinedLate: true},
				},
			},
			Ticks: []collector.PlayerTick{
				{Round: n, Tick: base + 100, Phase: collector.PhaseLive, SteamID: a, Team: common.TeamTerrorists,
					X: -416.5, Y: 2216.25, Z: 2.03, Yaw: -90, Health: 100, IsAlive: true, Money: 800,
					Loadout: collector.Loadout{HasHelmet: true, Primary: "AK-47", Nades: "fs"}},
				{Round: n, Tick: base + 101, Phase: collector.PhasePostRound, SteamID: b, Team: common.TeamCounterTerrorists},
			},
			Kills: []collector.Kill{{Round: n, Tick: base + 200, Phase: collector.PhaseLive,
				KillerSteamID: a, KillerTeam: common.TeamTerrorists, VictimSteamID: b,
				VictimTeam: common.TeamCounterTerrorists, Weapon: "AK-47", Headshot: true, Penetrated: 1, Distance: 12.5}},
			Utility: []collector.Utility{{Round: n, StartTick: base + 150, EndTick: base + 400, Phase: collector.PhaseLive,
				Kind: "smoke", ThrowerSteamID: a, ThrowerTeam: common.TeamTerrorists, X: 1, Y: 2, Z: 3, Radius: 144.5}},
			Shots: []collector.Shot{{Round: n, Tick: base + 199, Phase: collector.PhaseLive, SteamID: a,
				Team: common.TeamTerrorists, Weapon: "AK-47", X: 1.25, Y: 2.5, Z: 3, Yaw: 10.75, Pitch: -1.5}},
			Damage: []collector.Damage{{Round: n, Tick: base + 200, Phase: collector.PhaseLive, VictimSteamID: b,
				VictimTeam: common.TeamCounterTerrorists, AttackerSteamID: a, AttackerTeam: common.TeamTerrorists,
				Weapon: "AK-47", Kind: "bullet", HealthDamage: 111, HealthRemaining: 0, HitGroup: 1, X: 5, Y: 6, Z: 7}},
			Bomb: []collector.BombSample{{Round: n, Tick: base + 100, Phase: collector.PhaseFreeze, State: "carried",
				CarrierSteamID: a, CarrierTeam: common.TeamTerrorists, X: 1, Y: 1, Z: 1}},
			Kits: []collector.KitEvent{{Round: n, Tick: base + 300, Phase: collector.PhaseLive, Event: "dropped",
				KitID: 1, SteamID: b, Team: common.TeamCounterTerrorists, X: 9, Y: 9, Z: 0}},
			Trajectories: []collector.TrajectoryPoint{{Round: n, Tick: base + 150, Phase: collector.PhaseLive,
				ProjectileID: 3, Seq: 0, Kind: "smoke", ThrowerSteamID: a, ThrowerTeam: common.TeamTerrorists, X: 1, Y: 2, Z: 3}},
		}
	}
	in := []*collector.Round{round(1, 1000), round(2, 2000)}

	dir := t.TempDir()
	sink, err := csvsink.New(dir, csvsink.Meta{DemoFile: "test.dem", TickRate: 64})
	if err != nil {
		t.Fatal(err)
	}
	sink.SetMap("de_test")
	for _, r := range in {
		if err := sink.Round(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Players(map[uint64]string{a: "alpha", b: "bravo"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Manifest.Map != "de_test" || f.Manifest.TickRate != 64 || f.Names[b] != "bravo" {
		t.Errorf("manifest/names: %+v %v", f.Manifest, f.Names)
	}
	var got collect
	if err := f.Run(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.rounds) != len(in) {
		t.Fatalf("got %d rounds, want %d", len(got.rounds), len(in))
	}
	for i := range in {
		if !reflect.DeepEqual(got.rounds[i], *in[i]) {
			t.Errorf("round %d differs:\n got %+v\nwant %+v", i+1, got.rounds[i], *in[i])
		}
	}
}
