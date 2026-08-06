// round-collector dumps a CS2 demo to per-round CSV: one row per
// (round, tick, player) plus round metadata and economy.
//
// Usage:
//
//	round-collector -demo <path> [-out <dir>] [-max-rounds N] [-quiet]
//
// Output lands in <out>/<demo-basename>/ as manifest.json, players.csv,
// rounds.csv, round_players.csv and ticks.csv.gz. See
// docs/superpowers/specs/2026-08-05-round-collector-design.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "round-collector: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	var (
		demoPath  = flag.String("demo", "", "path to the .dem file (required)")
		outDir    = flag.String("out", "out", "output directory")
		maxRounds = flag.Int("max-rounds", 0, "stop after N rounds (0 = all)")
		quiet     = flag.Bool("quiet", false, "suppress per-round progress")
	)
	flag.Parse()

	if *demoPath == "" {
		flag.Usage()
		return fmt.Errorf("-demo is required")
	}

	f, err := os.Open(*demoPath)
	if err != nil {
		return fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	base := strings.TrimSuffix(filepath.Base(*demoPath), filepath.Ext(*demoPath))
	dir := filepath.Join(*outDir, base)

	sink, err := csvsink.New(dir, csvsink.Meta{
		DemoFile: filepath.Base(*demoPath),
		TickRate: parser.TickRate(),
	})
	if err != nil {
		return err
	}
	// Guarantee Close() runs - and that a real error from it surfaces -
	// however run() exits: an early return (e.g. sink.Players failing below)
	// or a panic unwinding out of c.Run() (demoinfocs re-panics anything that
	// isn't an unexpected EOF). Close() is idempotent, so this is safe even
	// though the happy path below also calls it implicitly via this defer
	// only (no separate explicit call remains). A Close error is only
	// surfaced here if run() would otherwise have returned nil - an error
	// already set by the code below takes priority and is not overwritten.
	defer func() {
		if cerr := sink.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// The map name arrives via a net-message before the first frame; v5 removed
	// the public ParseHeader (see playground/go/cmd/hello-demo for the details).
	// Registered after the sink exists so it can be handed straight through.
	var mapName string
	parser.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		mapName = m.GetMapName()
		sink.SetMap(mapName)
	})

	c := collector.New(parser, sink)
	c.SetMaxRounds(*maxRounds)

	// TickRate() is unusable at construction time above (parsing hasn't
	// started, so it always reports <= 0); CSVCMsg_ServerInfo carries the
	// real value and arrives mid-parse, well before any gameplay frame. Wire
	// it to both consumers that were built with the too-early value: the
	// sink's manifest and the collector's velocity tracker.
	parser.RegisterNetMessageHandler(func(m *msg.CSVCMsg_ServerInfo) {
		interval := m.GetTickInterval()
		if interval <= 0 {
			return
		}
		rate := 1 / float64(interval)
		sink.SetTickRate(rate)
		c.SetTickRate(rate)
	})

	start := time.Now()
	if !*quiet {
		c.OnRound(func(r *collector.Round) {
			fmt.Printf("round %2d  ticks %6d%s\n",
				r.Meta.Number, len(r.Ticks), completeness(r))
		})
	}

	runErr := c.Run()

	if err := sink.Players(c.Names()); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}

	rounds, ticks := c.Stats()
	fmt.Printf("\n%d rounds, %d tick rows, map %s, %.1fs -> %s\n",
		rounds, ticks, mapName, time.Since(start).Seconds(), dir)
	return nil
}

func completeness(r *collector.Round) string {
	if r.Meta.Complete {
		return ""
	}
	return "  (incomplete)"
}
