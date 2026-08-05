package collector

import (
	"fmt"
	"io"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Collector translates demoinfocs events into assembler calls and samples
// per-tick player state. It is the imperative shell around the pure core.
type Collector struct {
	parser demoinfocs.Parser
	sink   Sink
	asm    *assembler
	vel    *velocityTracker

	// matchStarted gates out warmup and the false-positive RoundStart at tick 0.
	// Strategy proven in playground/go/cmd/hello-demo.
	matchStarted bool

	names     map[uint64]string
	maxRounds int
	onRound   func(*Round)

	nRounds int
	nTicks  int64
	err     error // first sink error; aborts the parse
}

// New wires a Collector to a parser. Taking the demoinfocs.Parser interface
// rather than an io.Reader keeps this constructible from a fake in tests.
func New(p demoinfocs.Parser, sink Sink) *Collector {
	c := &Collector{
		parser: p,
		sink:   sink,
		vel:    newVelocityTracker(p.TickRate()),
		names:  make(map[uint64]string, 16),
	}
	c.asm = newAssembler(c.emitRound)
	c.register()
	return c
}

// NewFromReader is the convenience path for callers that just have a file.
func NewFromReader(r io.Reader, sink Sink) *Collector {
	return New(demoinfocs.NewParser(r), sink)
}

// SetMaxRounds stops parsing after n complete rounds. Zero means no limit.
// A development affordance: iterating on a 667MB demo takes seconds, not minutes.
func (c *Collector) SetMaxRounds(n int) { c.maxRounds = n }

// OnRound registers an extra consumer, called before the sink writes. Future
// analyzers (counterstrafe, spray) hook in here without touching this file.
func (c *Collector) OnRound(f func(*Round)) { c.onRound = f }

func (c *Collector) Stats() (int, int64) { return c.nRounds, c.nTicks }

// Names returns the observed steamid -> name map, for players.csv.
func (c *Collector) Names() map[uint64]string { return c.names }

func (c *Collector) Run() error {
	err := c.parser.ParseToEnd()

	// A demo that cuts off mid-stream is a partial success: flush what we have.
	if err != nil && err != demoinfocs.ErrCancelled {
		c.asm.finish()
		if c.err != nil {
			return c.err
		}
		return fmt.Errorf("parse: %w", err)
	}

	c.asm.finish()
	return c.err
}

func (c *Collector) emitRound(r *Round) {
	if c.err != nil {
		return
	}
	if c.onRound != nil {
		c.onRound(r)
	}
	if err := c.sink.Round(r); err != nil {
		c.err = err
		c.parser.Cancel()
		return
	}
	c.nRounds++
	c.nTicks += int64(len(r.Ticks))

	if c.maxRounds > 0 && c.nRounds >= c.maxRounds {
		c.parser.Cancel()
	}
}

func (c *Collector) live() bool {
	return c.matchStarted && !c.parser.GameState().IsWarmupPeriod()
}

func (c *Collector) tick() int32 {
	return int32(c.parser.GameState().IngameTick())
}

func (c *Collector) register() {
	c.parser.RegisterEventHandler(func(events.AnnouncementMatchStarted) {
		c.matchStarted = true
	})

	c.parser.RegisterEventHandler(func(events.RoundStart) {
		if !c.live() {
			return
		}
		c.asm.roundStart(c.tick(), int32(c.parser.GameState().TotalRoundsPlayed()+1))
		c.vel.reset() // players teleport to spawn; prior positions are meaningless
		c.readTimeout()
	})

	c.parser.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		c.asm.freezeEnd(c.tick(), c.snapshotEconomy())
	})

	c.parser.RegisterEventHandler(func(e events.RoundEnd) {
		c.asm.roundEnd(c.tick(), e.Winner, e.Reason)
		c.markSurvivors()
	})

	c.parser.RegisterEventHandler(func(events.RoundEndOfficial) {
		c.asm.roundEndOfficial(c.tick())
	})

	// Death leaves a gap; without forgetting, the respawn differences into a
	// teleport-sized velocity spike.
	c.parser.RegisterEventHandler(func(e events.Kill) {
		if e.Victim != nil {
			c.vel.forget(e.Victim.SteamID64)
		}
	})

	c.parser.RegisterEventHandler(func(events.FrameDone) {
		c.sampleFrame()
	})
}

func (c *Collector) sampleFrame() {
	if !c.asm.active() {
		return
	}
	tick := c.tick()
	phase := c.asm.phase()
	round := c.asm.cur.Meta.Number

	for _, p := range c.parser.GameState().Participants().Playing() {
		if p == nil {
			continue
		}
		if p.Name != "" {
			c.names[p.SteamID64] = p.Name
		}

		pos := p.Position()
		x, y, z := float32(pos.X), float32(pos.Y), float32(pos.Z)

		vx, vy, vz, speed, valid := float32(0), float32(0), float32(0), float32(0), false
		if p.IsAlive() {
			vx, vy, vz, speed, valid = c.vel.compute(p.SteamID64, tick, x, y, z)
		} else {
			c.vel.forget(p.SteamID64)
		}

		t := PlayerTick{
			Round: round, Tick: tick, Phase: phase,
			SteamID: p.SteamID64, Team: p.Team,
			X: x, Y: y, Z: z,
			Yaw: p.ViewDirectionX(), Pitch: p.ViewDirectionY(),
			VelX: vx, VelY: vy, VelZ: vz, Speed: speed, VelValid: valid,
			IsDucking:  p.IsDucking(),
			IsWalking:  p.IsWalking(),
			IsAirborne: p.IsAirborne(),
			IsScoped:   p.IsScoped(),
			Health:     int16(p.Health()),
			Armor:      int16(p.Armor()),
			IsAlive:    p.IsAlive(),
			Place:      p.LastPlaceName(),
		}

		if fd := p.FlashDurationTimeRemaining(); fd > 0 {
			t.FlashRemaining = float32(fd.Seconds())
		}
		if w := p.ActiveWeapon(); w != nil {
			t.ActiveWeapon = w.String()
		}

		c.readPawnProps(p, &t)
		c.asm.appendTick(t)
	}
}

// readPawnProps pulls the three properties demoinfocs does not wrap. Missing
// properties degrade to zero rather than failing the run - a demo from a
// different CS2 build may not carry all of them.
func (c *Collector) readPawnProps(p *common.Player, t *PlayerTick) {
	ent := p.PlayerPawnEntity()
	if ent == nil {
		return
	}
	if v, ok := ent.PropertyValue("m_iShotsFired"); ok {
		t.ShotsFired = int16(v.Int())
	}
	if v, ok := ent.PropertyValue("m_pMovementServices.m_flMaxspeed"); ok {
		t.MaxSpeed = v.Float()
	}
	if v, ok := ent.PropertyValue("m_pCameraServices.m_vecCsViewPunchAngle"); ok {
		punch := v.R3Vec()
		t.PunchPitch = float32(punch.X)
		t.PunchYaw = float32(punch.Y)
	}
}

func (c *Collector) snapshotEconomy() []PlayerRound {
	players := c.parser.GameState().Participants().Playing()
	out := make([]PlayerRound, 0, len(players))
	for _, p := range players {
		if p == nil {
			continue
		}
		out = append(out, PlayerRound{
			SteamID:               p.SteamID64,
			Team:                  p.Team,
			MoneyAtFreezeEnd:      int32(p.Money()),
			EquipValueAtFreezeEnd: int32(p.EquipmentValueFreezeTimeEnd()),
		})
	}
	return out
}

func (c *Collector) markSurvivors() {
	if !c.asm.active() {
		return
	}
	alive := make(map[uint64]bool, 10)
	for _, p := range c.parser.GameState().Participants().Playing() {
		if p != nil && p.IsAlive() {
			alive[p.SteamID64] = true
		}
	}
	players := c.asm.cur.Meta.Players
	for i := range players {
		players[i].Survived = alive[players[i].SteamID]
	}
}

// readTimeout reads timeout state from the game rules entity. demoinfocs has no
// timeout support (see datatables.go:1279 "TODO: timeout data"), but the raw
// properties are present.
func (c *Collector) readTimeout() {
	rules := c.parser.GameState().Rules()
	if rules == nil {
		return
	}
	ent := rules.Entity()
	if ent == nil {
		return
	}
	if v, ok := ent.PropertyValue("m_bTerroristTimeOutActive"); ok && v.BoolVal() {
		c.asm.setTimeout(common.TeamTerrorists)
		return
	}
	if v, ok := ent.PropertyValue("m_bCTTimeOutActive"); ok && v.BoolVal() {
		c.asm.setTimeout(common.TeamCounterTerrorists)
	}
}
