// Package export turns a demo into what the web app serves: the replay
// payload (movement.json) and its analytics page (analytics.json), in a
// folder named after the demo.
//
// A .dem is parsed once and both files are built as the rounds stream past,
// so nothing is written in between and memory holds one round of ticks at a
// time. A parsed CSV folder is read back with csvsource and gives exactly the
// payload the .dem would have.
//
// demo-export (by hand) and demo-worker (uploads) both run this, so a demo
// published either way is built by the same code.
package export

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsink"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsource"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/payload"
)

// Options is how to build. The zero value is not usable: Radar is required.
type Options struct {
	Radar    string    // radar overview images and .txt files (assets/radar)
	Hz       float64   // position samples per second; 0 is payload.DefaultHz
	CSV      string    // also write the round-collector CSV folder under this directory (.dem input only)
	Progress io.Writer // a line per round; nil for none
}

// Result is what was written.
type Result struct {
	Name    string
	Dir     string // <out>/<name>
	Map     string
	Rounds  int
	Players int
	Bytes   int // movement.json
	Took    time.Duration
}

func (r Result) String() string {
	return fmt.Sprintf("%s: %s, %d rounds, %d players, %d KB, %.1fs -> %s",
		r.Name, r.Map, r.Rounds, r.Players, r.Bytes/1024, r.Took.Seconds(), r.Dir)
}

// Write builds in (a .dem, a .dem.zst or a parsed CSV folder) into
// <out>/<name>/. An empty name is the input's own: the file's name without
// its extensions, or the folder's.
func Write(in, out, name string, o Options) (Result, error) {
	start := time.Now()
	if o.Hz == 0 {
		o.Hz = payload.DefaultHz
	}
	st, err := os.Stat(in)
	if err != nil {
		return Result{}, err
	}

	var (
		builder *payload.Builder
		match   *analytics.Match
	)
	if st.IsDir() {
		f, err := csvsource.Open(in)
		if err != nil {
			return Result{}, err
		}
		if name == "" {
			name = filepath.Base(filepath.Clean(in))
		}
		builder = payload.NewBuilder(name, o.Radar, o.Hz)
		builder.SetMap(f.Manifest.Map)
		builder.SetTickRate(f.Manifest.TickRate)
		match = &analytics.Match{Name: name, Map: f.Manifest.Map, TickRate: f.Manifest.TickRate, Names: f.Names}
		if err := f.Run(&both{builder, match, o.Progress}); err != nil {
			return Result{}, err
		}
	} else {
		if !collector.IsDemoFile(in) {
			return Result{}, errors.New("not a .dem, a .dem.zst or a parsed folder")
		}
		if name == "" {
			name = collector.DemoName(in)
		}
		builder = payload.NewBuilder(name, o.Radar, o.Hz)
		match = &analytics.Match{Name: name}
		if err := parse(in, name, o, builder, match); err != nil {
			return Result{}, err
		}
	}

	p, err := builder.Finish(match.Names)
	if err != nil {
		return Result{}, err
	}
	dir := filepath.Join(out, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "movement.json"), raw, 0o644); err != nil {
		return Result{}, err
	}
	if err := analytics.WriteGame(out, match, nil); err != nil {
		return Result{}, err
	}
	return Result{
		Name: name, Dir: dir, Map: p.Map, Rounds: p.Demos[0].Rounds, Players: len(p.Players),
		Bytes: len(raw), Took: time.Since(start),
	}, nil
}

// parse runs the collector over a .dem, feeding the payload and the analytics
// (and the CSV folder, with Options.CSV) from the one pass.
func parse(path, name string, o Options, builder *payload.Builder, match *analytics.Match) error {
	f, err := collector.OpenDemo(path)
	if err != nil {
		return err
	}
	defer f.Close()
	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	sinks := multi{&both{builder, match, o.Progress}}
	var csv *csvsink.Sink
	if o.CSV != "" {
		csv, err = csvsink.New(filepath.Join(o.CSV, name), csvsink.Meta{
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
	builder  *payload.Builder
	match    *analytics.Match
	progress io.Writer
}

func (b *both) Round(r *collector.Round) error {
	if b.progress != nil {
		fmt.Fprintf(b.progress, "  round %2d  %6d ticks\n", r.Meta.Number, len(r.Ticks))
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
