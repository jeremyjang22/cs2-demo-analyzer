// hello-demo opens a CS2 .dem and prints header, tick-0 positions,
// and the positions on the first live-round tick.
//
// demoinfocs-golang v5 removed the public ParseHeader() method; header
// data is instead surfaced via net-message handlers that fire during parsing.
//
// Live-round detection strategy (mirrors the Python playground):
//   - IsWarmupPeriod() defaults false at tick 0 before game-rules initialise,
//     causing a false-positive RoundStart at tick 0.
//   - Python used begin_new_match → round_freeze_end. Go equivalent:
//     AnnouncementMatchStarted (tick ~1279) as floor, then next non-warmup
//     RoundStart. This lands at tick 2543 for mega_ot_mirage.dem.
//   - Python's round_freeze_end floor lands at tick 4238; the tick difference
//     is expected — demoparser2 and demoinfocs surface different event concepts.
package main

import (
	"fmt"
	"os"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: hello-demo <path-to-dem>")
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	// --- Header info via net-message handlers (v5 API) ---
	// CDemoFileHeader fires at the very start of parsing, before the first frame.
	var (
		mapName    string
		serverName string
	)
	parser.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		mapName = m.GetMapName()
		serverName = m.GetServerName()
	})

	// CSVCMsg_ServerInfo carries host name. CDemoFileInfo is not present in all
	// FACEIT demos so PlaybackTicks / PlaybackTime are obtained via TickRate()
	// and the final ingame tick after ParseToEnd().
	parser.RegisterNetMessageHandler(func(m *msg.CSVCMsg_ServerInfo) {
		if mapName == "" {
			mapName = m.GetMapName()
		}
		if serverName == "" {
			serverName = m.GetHostName()
		}
	})

	// --- State for the interesting ticks ---
	var (
		headerPrinted    bool
		playersPrinted   bool
		tick0Printed     bool
		liveTickPrinted  bool
		announcementSeen bool // floor: AnnouncementMatchStarted must have fired
	)

	printPositions := func(label string) {
		fmt.Printf("\n== %s ==\n", label)
		fmt.Printf("Tick: %d\n", parser.GameState().IngameTick())
		players := parser.GameState().Participants().Playing()
		for _, p := range players {
			pos := p.Position()
			fmt.Printf("  %-24s  x=%8.1f  y=%8.1f  z=%8.1f\n", p.Name, pos.X, pos.Y, pos.Z)
		}
		if len(players) == 0 {
			fmt.Println("  (game state not yet populated — demoinfocs backfills positions only after subsequent ticks)")
		}
	}

	// Header prints on the first FrameDone after CDemoFileHeader has fired.
	// Tick 0 ("raw first tick") prints on the same first FrameDone so it
	// genuinely shows frame-0 state — even though that state is empty,
	// the section's whole point is to surface that emptiness as a
	// cross-library finding versus demoparser2's back-filled tick 0.
	parser.RegisterEventHandler(func(e events.FrameDone) {
		if !headerPrinted && mapName != "" {
			fmt.Println("== Header ==")
			fmt.Printf("Map:          %s\n", mapName)
			fmt.Printf("Server:       %s\n", serverName)
			headerPrinted = true
		}
		if !tick0Printed && headerPrinted {
			printPositions("Tick 0 (raw first tick)")
			tick0Printed = true
		}
	})

	// AnnouncementMatchStarted is the reliable "warmup is over, match is real"
	// signal. It fires at tick ~1279 in mega_ot_mirage.dem (earlier than the
	// second MatchStart at tick 2543 which is the actual first competitive round).
	// We also use it as the moment to print Players, since by then the
	// game-state entities (including names and steam IDs) are populated.
	parser.RegisterEventHandler(func(e events.AnnouncementMatchStarted) {
		announcementSeen = true
		if !playersPrinted {
			// SourceTV is included intentionally — surfaces a cross-library
			// difference (demoparser2 filters it; demoinfocs exposes it).
			fmt.Println("\n== Players ==")
			all := parser.GameState().Participants().All()
			for _, p := range all {
				fmt.Printf("  - %s  (%d)\n", p.Name, p.SteamID64)
			}
			fmt.Printf("  (%d total)\n", len(all))
			playersPrinted = true
		}
	})

	// First non-warmup RoundStart after AnnouncementMatchStarted.
	parser.RegisterEventHandler(func(e events.RoundStart) {
		if liveTickPrinted || !announcementSeen {
			return
		}
		if parser.GameState().IsWarmupPeriod() {
			return
		}

		printPositions("First live-round tick")
		liveTickPrinted = true
	})

	if err := parser.ParseToEnd(); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	// Append playback info (available after ParseToEnd).
	// CDemoFileInfo is absent from many FACEIT demos; fall back to TickRate().
	fmt.Printf("\n== Playback Info ==\n")
	tickRate := parser.TickRate()
	fmt.Printf("TickRate:  %.1f\n", tickRate)
	fmt.Printf("Duration:  %s\n", parser.CurrentTime())

	if !liveTickPrinted {
		fmt.Println("\n== First live-round tick ==")
		fmt.Println("  (no RoundStart found after AnnouncementMatchStarted)")
	}
}
