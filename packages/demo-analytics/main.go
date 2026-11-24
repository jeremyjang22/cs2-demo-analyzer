// demo-analytics turns one demo, or a whole set of them, into match
// statistics: weapon kills, opening duels, clutches, and how often each
// X-vs-Y player count converts into a round win - by player and by team.
//
// Usage:
//
//	demo-analytics [-out <dir>] [-web <dir>] [-team "Name=player,player,..."] <input>...
//
// An input is a round-collector output folder, a .dem or .dem.zst file, or a
// folder of any of those. See docs/demo-analytics.md for what every number means.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
)

// teamFlags collects a repeatable -team flag.
type teamFlags []string

func (t *teamFlags) String() string     { return strings.Join(*t, " ") }
func (t *teamFlags) Set(v string) error { *t = append(*t, v); return nil }

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "demo-analytics: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var teams teamFlags
	outDir := flag.String("out", filepath.Join("out", "_analytics"), "output directory")
	quiet := flag.Bool("quiet", false, "suppress the printed summary")
	webDir := flag.String("web", "", "also write <dir>/<demo>/analytics.json for the web app's per-game "+
		"view, e.g. -web web/public/data")
	flag.Var(&teams, "team", `name a team by its player pool, as "Name=player,player,..." where a `+
		"player is a steamid64 or an in-game name; repeatable")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			"usage: demo-analytics [flags] <input>...\n\n"+
				"An input is a round-collector output folder, a .dem or .dem.zst file, or a folder of those.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

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
		start := time.Now()
		m, err := analytics.Open(in)
		if err != nil {
			return err
		}
		// The demo name is the key every fact row carries, so two inputs that
		// share a basename would be silently merged into one match.
		if prev, dup := seen[m.Name]; dup {
			return fmt.Errorf("%s and %s are both named %q; rename one", prev, in, m.Name)
		}
		seen[m.Name] = in
		if !*quiet {
			fmt.Printf("%-40s %-12s %2d rounds  %.1fs\n", m.Name, m.Map, len(m.Rounds), time.Since(start).Seconds())
		}
		for id, name := range m.Names {
			names[id] = name
		}
		matches = append(matches, m)
	}

	defs, warnings, err := analytics.ParseTeamDefs(teams, names)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	report := analytics.NewReport()
	for _, m := range matches {
		report.Add(m, defs)
	}
	for _, w := range report.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if err := report.Write(*outDir); err != nil {
		return err
	}
	if *webDir != "" {
		for _, m := range matches {
			if err := analytics.WriteGame(*webDir, m, defs); err != nil {
				return err
			}
		}
	}

	if !*quiet {
		// With teams named, they are the subject and every opponent is noise;
		// with none named, there is nothing to prefer.
		var show []string
		for _, d := range defs {
			show = append(show, d.Name)
		}
		report.Summary(os.Stdout, show)
		fmt.Printf("\n%d demo(s) -> %s\n", len(matches), *outDir)
	}
	return nil
}
