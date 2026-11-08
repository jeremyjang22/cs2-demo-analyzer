// clip-list picks the moments in a set of demos worth recording from a
// player's point of view and writes them as a JSON list. The HLAE runner on the
// recording machine (scripts/clips/record-clips.ps1) hands each demo's clips
// to CS Demo Manager, which drives CS2 and HLAE to record them.
//
// Usage:
//
//	clip-list [-player name,steamid,...] [-kinds opening,multikill,clutch,round] [-out clips.json] <input>...
//
// An input is a round-collector output folder, a folder of those, or a .dem
// file, the same as demo-analytics. See docs/clip-capture.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "clip-list: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "clips.json", "file to write; - for stdout")
	players := flag.String("player", "", "comma-separated players to record, each a steamid64 or an in-game name; "+
		"empty records everyone")
	kinds := flag.String("kinds", "opening,multikill,clutch", "comma-separated clip kinds: "+
		strings.Join(analytics.ClipKinds, ", "))
	minMulti := flag.Int("min-multi", 3, "kills in one round that make a multikill clip")
	allClutches := flag.Bool("all-clutches", false, "also record clutches that were lost")
	before := flag.Float64("before", 3, "seconds kept before each moment")
	after := flag.Float64("after", 2, "seconds kept after each moment")
	noMerge := flag.Bool("no-merge", false, "keep overlapping clips of one player in one round separate")
	var cs csdmOptions
	flag.StringVar(&cs.dir, "csdm", "", "also write one CS Demo Manager video config per demo into this folder, "+
		"for `csdm video --config-file`")
	flag.StringVar(&cs.demoDir, "demo-dir", `C:\demos`, "with -csdm: the folder holding the .dem files on the "+
		"recording machine")
	flag.StringVar(&cs.videoDir, "video-dir", "", "with -csdm: where the recording machine writes videos, one "+
		"folder per demo; empty uses CS Demo Manager's setting")
	flag.BoolVar(&cs.reel, "reel", false, "with -csdm: join each demo's clips into one video")
	flag.StringVar(&cs.recorder, "recorder", "CS", `with -csdm: CS Demo Manager's recording system, "CS" (CS2's own) `+
		`or "HLAE"; empty uses the app's setting`)
	flag.IntVar(&cs.fps, "fps", 60, "with -csdm: video framerate; 0 uses the app's setting")
	flag.IntVar(&cs.width, "width", 1920, "with -csdm: video width; 0 uses the app's setting")
	flag.IntVar(&cs.height, "height", 1080, "with -csdm: video height; 0 uses the app's setting")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			"usage: clip-list [flags] <input>...\n\n"+
				"An input is a round-collector output folder, a folder of those, or a .dem file.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	opt := analytics.ClipOptions{
		MinMulti: *minMulti, AllClutches: *allClutches,
		Before: *before, After: *after, NoMerge: *noMerge,
	}
	for _, k := range strings.Split(*kinds, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !slices.Contains(analytics.ClipKinds, k) {
			return fmt.Errorf("unknown clip kind %q; want one of %s", k, strings.Join(analytics.ClipKinds, ", "))
		}
		opt.Kinds = append(opt.Kinds, k)
	}

	inputs, err := analytics.Inputs(flag.Args())
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		flag.Usage()
		return fmt.Errorf("no demos given")
	}
	matches := make([]*analytics.Match, 0, len(inputs))
	names := make(map[uint64]string)
	seen := make(map[string]string, len(inputs))
	for _, in := range inputs {
		m, err := analytics.Open(in)
		if err != nil {
			return err
		}
		// The runner finds a demo by this name, so two that share one would
		// record the wrong game.
		if prev, dup := seen[m.Name]; dup {
			return fmt.Errorf("%s and %s are both named %q; rename one", prev, in, m.Name)
		}
		seen[m.Name] = in
		for id, n := range m.Names {
			names[id] = n
		}
		matches = append(matches, m)
	}

	// A player list resolves exactly like a -team pool in demo-analytics.
	if strings.TrimSpace(*players) != "" {
		defs, err := analytics.ParseTeamDefs([]string{"players=" + *players}, names)
		if err != nil {
			return err
		}
		opt.Players = defs[0].IDs
	}

	var clips []analytics.Clip
	for _, m := range matches {
		clips = append(clips, analytics.Clips(m, opt)...)
	}
	raw, err := json.MarshalIndent(analytics.NewClipList(clips), "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if *out == "-" {
		_, err = os.Stdout.Write(raw)
		return err
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		return err
	}

	if cs.dir != "" {
		if err := writeCSDM(clips, cs); err != nil {
			return err
		}
	}

	var secs float64
	for _, c := range clips {
		secs += float64(c.EndTick-c.StartTick) / c.TickRate
	}
	fmt.Printf("%d clip(s) from %d demo(s), %.0fs of footage -> %s\n", len(clips), len(matches), secs, *out)
	if cs.dir != "" {
		fmt.Printf("CS Demo Manager configs -> %s\n", cs.dir)
	}
	return nil
}
