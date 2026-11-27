// Command demo-export turns a demo into what the web app serves: the replay
// payload (movement.json), its analytics page (analytics.json) and, when told
// who it belongs to, the info.json that decides who may see it.
//
//	demo-export ../data/foo.dem                      # parse, write web/public/data/foo/
//	demo-export -visibility private -owner 7656119... ../data/foo.dem
//	demo-export -kind pug -visibility public "C:/demos/1-604132bd.dem.zst"
//	demo-export ../out/foo                           # re-export a folder parsed earlier
//	demo-export -upload ../data/foo.dem              # ...and publish it to the site
//
// A .dem is parsed once and both files are built as the rounds stream past,
// so nothing is written in between and memory holds one round of ticks at a
// time. -csv also writes the round-collector CSV folder from the same pass,
// for notebooks. A folder is read back with csvsource and gives exactly the
// payload the .dem would have.
//
// This replaced four steps - round-collector, export_movement.py,
// demo-analytics -web, upload-data - and is the code the upload worker will
// run, which is why the payload is built in Go now.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/catalog"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsource"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/envfile"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/payload"
)

type options struct {
	out, radar, csv string
	env, pros       string
	hz              float64
	upload, quiet   bool
	info            map[string]string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "demo-export: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var o options
	flag.StringVar(&o.out, "out", filepath.Join("web", "public", "data"), "where each demo's folder is written")
	flag.StringVar(&o.radar, "radar", filepath.Join("..", "assets", "radar"), "radar overview images and .txt files")
	flag.StringVar(&o.csv, "csv", "", "also write the round-collector CSV folder under this directory (a .dem input only)")
	flag.Float64Var(&o.hz, "hz", payload.DefaultHz, "position samples per second")
	flag.BoolVar(&o.upload, "upload", false, "publish the exported demos afterwards, through the catalogue (see demo-catalog)")
	flag.StringVar(&o.env, "env", filepath.Join("api", ".env"), "-upload: settings file, for DATABASE_URL and R2_* not already set")
	flag.StringVar(&o.pros, "pros", filepath.Join("web", "src", "pros.json"), "-upload: the pro catalogue")
	flag.BoolVar(&o.quiet, "quiet", false, "no per-round progress")

	// What the demo cannot say about itself, written to info.json. The same
	// fields and rules as `npm run set-info`, which can change them later
	// without re-exporting.
	visibility := flag.String("visibility", "", "public | link | private - who may see it on the site; undeclared means private")
	owner := flag.String("owner", "", "64-bit steamid of the account that owns it on the site")
	kind := flag.String("kind", "", "pug | match - declared, never guessed from the file")
	date := flag.String("date", "", "when it was played, YYYY-MM-DD")
	event := flag.String("event", "", "tournament or league, for a pro match")
	title := flag.String("title", "", "a readable name for lists, in place of the file name")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: demo-export [flags] <file.dem | file.dem.zst | parsed folder>...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		return errors.New("no demo given")
	}

	o.info = map[string]string{"visibility": *visibility, "owner": *owner, "kind": *kind,
		"date": *date, "event": *event, "title": *title}
	if err := checkInfo(o.info); err != nil {
		return err
	}

	// Connect before parsing, so a missing key fails now rather than after
	// the minutes a batch of demos takes.
	var publisher *catalog.Publisher
	if o.upload {
		if _, err := envfile.Load(o.env); err != nil {
			return err
		}
		var err error
		if publisher, err = catalog.FromEnv(context.Background(), o.pros); err != nil {
			return fmt.Errorf("-upload: %w", err)
		}
		defer publisher.Close()
		publisher.Log = os.Stdout
	}

	var names []string
	for _, in := range flag.Args() {
		name, err := export(in, o)
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
		names = append(names, name)
	}

	if publisher != nil {
		dirs := make([]string, len(names))
		for i, name := range names {
			dirs[i] = filepath.Join(o.out, name)
		}
		fmt.Println()
		_, err := publisher.Put(context.Background(), dirs)
		return err
	}
	return nil
}

// export writes one demo's folder and returns its name.
func export(in string, o options) (string, error) {
	start := time.Now()
	st, err := os.Stat(in)
	if err != nil {
		return "", err
	}

	var (
		name    string
		builder *payload.Builder
		match   *analytics.Match
	)
	if st.IsDir() {
		f, err := csvsource.Open(in)
		if err != nil {
			return "", err
		}
		name = filepath.Base(filepath.Clean(in))
		builder = payload.NewBuilder(name, o.radar, o.hz)
		builder.SetMap(f.Manifest.Map)
		builder.SetTickRate(f.Manifest.TickRate)
		match = &analytics.Match{Name: name, Map: f.Manifest.Map, TickRate: f.Manifest.TickRate, Names: f.Names}
		if err := f.Run(&both{builder, match, o.quiet}); err != nil {
			return "", err
		}
	} else {
		if !collector.IsDemoFile(in) {
			return "", errors.New("not a .dem, a .dem.zst or a parsed folder")
		}
		name = collector.DemoName(in)
		builder = payload.NewBuilder(name, o.radar, o.hz)
		match = &analytics.Match{Name: name}
		if err := parse(in, o, builder, match); err != nil {
			return "", err
		}
	}

	p, err := builder.Finish(match.Names)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(o.out, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "movement.json"), raw, 0o644); err != nil {
		return "", err
	}
	if err := analytics.WriteGame(o.out, match, nil); err != nil {
		return "", err
	}
	if err := writeInfo(filepath.Join(dir, "info.json"), o.info); err != nil {
		return "", err
	}

	fmt.Printf("%s: %s, %d rounds, %d players, %d KB, %.1fs -> %s\n",
		name, p.Map, p.Demos[0].Rounds, len(p.Players), len(raw)/1024, time.Since(start).Seconds(), dir)
	return name, nil
}

// parse runs the collector over a .dem, feeding the payload and the analytics
// (and the CSV folder, with -csv) from the one pass.
func parse(path string, o options, builder *payload.Builder, match *analytics.Match) error {
	f, err := collector.OpenDemo(path)
	if err != nil {
		return err
	}
	defer f.Close()
	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	sinks := multi{&both{builder, match, o.quiet}}
	var csv *csvsink.Sink
	if o.csv != "" {
		csv, err = csvsink.New(filepath.Join(o.csv, collector.DemoName(path)), csvsink.Meta{
			DemoFile: filepath.Base(path), TickRate: parser.TickRate(),
		})
		if err != nil {
			return err
		}
		sinks = append(sinks, csv)
	}

	parser.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		match.Map = m.GetMapName()
		builder.SetMap(match.Map)
		if csv != nil {
			csv.SetMap(match.Map)
		}
	})
	c := collector.New(parser, sinks)
	// parser.TickRate() is unusable before parsing starts; ServerInfo carries
	// the real value, the same source round-collector records.
	parser.RegisterNetMessageHandler(func(m *msg.CSVCMsg_ServerInfo) {
		if interval := m.GetTickInterval(); interval > 0 {
			rate := 1 / float64(interval)
			match.TickRate = rate
			builder.SetTickRate(rate)
			c.SetTickRate(rate)
			if csv != nil {
				csv.SetTickRate(rate)
			}
		}
	})

	runErr := c.Run()
	match.Names = c.Names()
	if csv != nil {
		// The collector never closes its sink, and Close is what writes the
		// manifest - the file that makes the folder readable at all.
		if err := errors.Join(csv.Players(match.Names), csv.Close()); err != nil {
			return err
		}
	}
	// A recording that stops mid-stream is still every round up to that
	// point, and most recordings end that way.
	if runErr != nil && !errors.Is(runErr, demoinfocs.ErrUnexpectedEndOfDemo) {
		return runErr
	}
	return nil
}

// both feeds each round to the payload and keeps the three small parts the
// analytics read, letting the ticks go.
type both struct {
	builder *payload.Builder
	match   *analytics.Match
	quiet   bool
}

func (b *both) Round(r *collector.Round) error {
	if !b.quiet {
		fmt.Printf("  round %2d  %6d ticks\n", r.Meta.Number, len(r.Ticks))
	}
	b.match.Rounds = append(b.match.Rounds, analytics.Round{Meta: r.Meta, Kills: r.Kills, Bomb: r.Bomb})
	return b.builder.Round(r)
}

func (b *both) Close() error { return b.builder.Close() }

// multi is several sinks fed the same rounds.
type multi []collector.Sink

func (m multi) Round(r *collector.Round) error {
	for _, s := range m {
		if err := s.Round(r); err != nil {
			return err
		}
	}
	return nil
}

func (m multi) Close() error {
	var errs []error
	for _, s := range m {
		errs = append(errs, s.Close())
	}
	return errors.Join(errs...)
}

var (
	steamidRe = regexp.MustCompile(`^\d{17}$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// checkInfo applies the rules index-demos enforces, before any parsing, so a
// typo fails in a second rather than after a minutes-long parse.
func checkInfo(info map[string]string) error {
	if v := info["visibility"]; v != "" && v != "public" && v != "link" && v != "private" {
		return fmt.Errorf("-visibility %q: want public, link or private", v)
	}
	if v := info["owner"]; v != "" && !steamidRe.MatchString(v) {
		return fmt.Errorf("-owner %q: want a 64-bit steamid (17 digits)", v)
	}
	if v := info["kind"]; v != "" && v != "pug" && v != "match" {
		return fmt.Errorf("-kind %q: want pug or match", v)
	}
	if v := info["date"]; v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil || !dateRe.MatchString(v) {
			return fmt.Errorf("-date %q: want YYYY-MM-DD", v)
		}
	}
	return nil
}

// writeInfo records what a person declared about the demo. Fields not given
// this run keep their previous values, so re-exporting never forgets who owns
// a demo. Nothing given and no file yet writes nothing: undeclared is private.
func writeInfo(path string, given map[string]string) error {
	info := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &info); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	changed := false
	for _, k := range []string{"kind", "event", "date", "title", "visibility", "owner"} {
		if v := given[k]; v != "" {
			info[k] = v
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
