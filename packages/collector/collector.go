package collector

import (
	"errors"
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
	onRound   []func(*Round)

	// tickGuard suppresses a duplicate sampleFrame for the same ingame tick.
	// See its doc comment for why this happens.
	tickGuard sampleGuard

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
// May be called more than once; every registered consumer runs, in
// registration order.
func (c *Collector) OnRound(f func(*Round)) { c.onRound = append(c.onRound, f) }

// SetTickRate corrects the velocity tracker's tick rate once the real value
// is known. New() constructs the tracker before parsing starts, when
// TickRate() is not yet available (see velocityTracker.SetTickRate); the
// caller should invoke this from a CSVCMsg_ServerInfo net-message handler as
// soon as parsing reveals the real rate.
func (c *Collector) SetTickRate(rate float64) { c.vel.SetTickRate(rate) }

func (c *Collector) Stats() (int, int64) { return c.nRounds, c.nTicks }

// Names returns the observed steamid -> name map, for players.csv.
func (c *Collector) Names() map[uint64]string { return c.names }

func (c *Collector) Run() error {
	err := c.parser.ParseToEnd()

	// A demo that cuts off mid-stream is a partial success: flush what we have.
	if err != nil && !errors.Is(err, demoinfocs.ErrCancelled) {
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
	for _, f := range c.onRound {
		f(r)
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
		c.vel.reset()       // players teleport to spawn; prior positions are meaningless
		c.tickGuard.reset() // new round: the first tick sampled in it must never be treated as a repeat
		c.readTimeout()
	})

	c.parser.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		if !c.asm.active() {
			return // no open round (e.g. warmup): skip snapshotEconomy rather than walking every participant for a result that would be discarded
		}
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
	if !c.tickGuard.shouldSample(tick) {
		return
	}
	phase := c.asm.phase()
	round := c.asm.cur.Meta.Number

	for _, p := range c.parser.GameState().Participants().Playing() {
		if !includeParticipant(p) {
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
			Buttons:    p.ButtonsPressedState,
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
		if !includeParticipant(p) {
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

// includeParticipant reports whether p is a real player worth sampling.
// Participants().Playing() can return a non-player entity alongside real
// players - observed in practice as a "Crew" participant with SteamID64 0 -
// which would otherwise collide with every other unauthenticated entity
// under the (round, tick, steamid) key and inflate roster counts. Nil
// entries are also guarded against defensively.
func includeParticipant(p *common.Player) bool {
	return p != nil && p.SteamID64 != 0
}

// sampleGuard suppresses a duplicate sampleFrame call for the same ingame
// tick. FrameDone fires once per demo frame, and demoinfocs emits a frame
// for DEM_FullPacket as well as DEM_Packet - both carrying the same ingame
// tick on periodic keyframe ticks. Without this, every (round, tick,
// steamid) row on such a tick would be written twice, and the second
// velocityTracker.compute() call would zero out (it hits the tick <=
// prev.tick guard), so a naive downstream de-dup on the full row would not
// collapse the pair back to one correct sample.
type sampleGuard struct {
	lastTick int32
	have     bool
}

// shouldSample reports whether tick is new since the last call, recording it
// if so.
func (g *sampleGuard) shouldSample(tick int32) bool {
	if g.have && tick == g.lastTick {
		return false
	}
	g.lastTick = tick
	g.have = true
	return true
}

// reset drops the tracked tick, called at round boundaries so a new round's
// first sampled tick is never mistaken for a repeat of whatever tick the
// prior round last sampled.
func (g *sampleGuard) reset() { g.have = false }
