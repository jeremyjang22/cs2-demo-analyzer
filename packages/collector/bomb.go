package collector

import (
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// Bomb states, as written to the state column.
const (
	// BombCarried means a player is holding it; Position is that player's
	// position, and CarrierSteamID says whose.
	BombCarried = "carried"
	// BombDropped means it is loose on the ground (or mid-flight, having just
	// been thrown or dropped by a dying player).
	BombDropped = "dropped"
	BombPlanted = "planted"
	BombDefused = "defused"
	// BombExploded is terminal and always sits at the plant position.
	BombExploded = "exploded"
)

// BombSample is one observation of the C4: where it was, who had it, and what
// was happening to it. This struct IS the schema: BombColumns and AppendRow
// below must stay aligned with it.
//
// Rows are emitted on change, not per tick. While the bomb is carried its
// position is the carrier's, which ticks.csv.gz already records once per tick
// per player - repeating it here would be tens of thousands of redundant rows
// to say "still on the same player". So a carried bomb produces exactly one
// row, at pickup; a consumer wanting its path reads the carrier's ticks. A
// loose bomb has no such shadow, so those rows are emitted whenever it
// actually moves - which is what makes a thrown C4 animate rather than
// teleport.
//
// CarrierSteamID is 0 for every state but carried. It is NOT cleared to mean
// "unknown": the planter is recorded on the planted row so a consumer can
// attribute the plant without joining anything.
type BombSample struct {
	Round int32
	Tick  int32
	Phase Phase

	State          string
	CarrierSteamID uint64
	CarrierTeam    common.Team

	// Site is "A", "B", or empty. Only ever set on a planted row, and only
	// when demoinfocs resolved the bombsite - it reports 0 on some demos.
	Site string

	X, Y, Z float32
}

// BombColumns is the CSV header for bomb.csv, in AppendRow's emit order.
func BombColumns() []string {
	return []string{
		"round", "tick", "phase", "state",
		"carrier_steamid", "carrier_team", "site", "x", "y", "z",
	}
}

// AppendRow appends this sample's CSV fields to dst and returns the extended
// slice, matching PlayerTick.AppendRow's reusable-buffer contract.
func (s *BombSample) AppendRow(dst []string) []string {
	return append(dst,
		i32(s.Round),
		i32(s.Tick),
		s.Phase.String(),
		s.State,
		strconv.FormatUint(s.CarrierSteamID, 10),
		strconv.Itoa(int(s.CarrierTeam)),
		s.Site,
		f32(s.X), f32(s.Y), f32(s.Z),
	)
}

// siteName renders a demoinfocs Bombsite as "A", "B" or "" for unknown. The
// constants are the ASCII codes of the letters themselves, but only for the
// two real sites - BomsiteUnknown is 0 and must not become "\x00".
func siteName(site rune) string {
	switch site {
	case 'A', 'B':
		return string(site)
	default:
		return ""
	}
}

// bombTracker decides when the bomb is worth writing a row for.
//
// It is a plain state machine over polled observations rather than a set of
// event handlers, because the two things a viewer needs - "who is carrying it"
// and "where is it lying" - are entity state, not events. demoinfocs does fire
// BombPickup and BombDropped, but neither carries a position, and BombDropped
// explicitly does not fire when the bomb passes from one player to another.
// Polling covers all of it uniformly; the events are still used for the three
// transitions polling genuinely cannot see (plant, defuse, explode), since
// those change what the same "nobody is carrying it" observation means.
type bombTracker struct {
	// have is false until the first row of a round is emitted, so the opening
	// state is always recorded even when nothing has changed yet.
	have  bool
	state string
	// carrier is the last emitted carrier, tracked separately from state
	// because a bomb passed between two teammates never leaves "carried".
	carrier uint64
	// x, y, z is the last emitted position. z is carried purely so a defuse or
	// an explosion can be reported at the height the bomb was actually at: on
	// a multi-level map the floor a row lands on is derived from z, and
	// leaving it zero put Nuke's B-site detonations on the upper radar.
	x, y, z float32

	// planted latches at the plant and is what makes an uncarried bomb read
	// "planted" rather than "dropped". defused and exploded are terminal: the
	// round's outcome is settled and further rows would be noise.
	planted  bool
	terminal bool
	site     string
}

// moveEpsilon is how far a loose bomb must travel before it earns another
// row, in Hammer units. A player is 32 units wide, so 4 units is visually
// nothing - but it is well above the sub-unit jitter a settling physics prop
// produces, which would otherwise emit a row per tick forever after a drop.
const moveEpsilon = 4.0

func (b *bombTracker) reset() {
	*b = bombTracker{}
}

// shouldEmit reports whether this observation differs enough from the last
// emitted row to be worth one of its own, and records it if so.
func (b *bombTracker) shouldEmit(state string, carrier uint64, x, y, z float32) bool {
	if !b.have || state != b.state || carrier != b.carrier {
		b.have, b.state, b.carrier = true, state, carrier
		b.x, b.y, b.z = x, y, z
		return true
	}
	// A carried bomb's position is the carrier's, already in ticks.csv.gz.
	if state != BombDropped {
		return false
	}
	if abs32(x-b.x) < moveEpsilon && abs32(y-b.y) < moveEpsilon {
		return false
	}
	b.x, b.y, b.z = x, y, z
	return true
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// plant latches the planted state and the position the bomb will hold for the
// rest of the round. Called from the BombPlanted handler, which writes the row
// itself - this only updates what the tracker will compare against, so a
// subsequent poll does not emit a duplicate.
func (b *bombTracker) plant(planter uint64, site string, x, y, z float32) {
	b.planted = true
	b.site = site
	b.have, b.state, b.carrier = true, BombPlanted, planter
	b.x, b.y, b.z = x, y, z
}
