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
// The building is package export, which demo-worker runs for uploads. -csv
// also writes the round-collector CSV folder from the same pass, for
// notebooks.
//
// This replaced four steps - round-collector, export_movement.py,
// demo-analytics -web, upload-data.
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

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/catalog"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/envfile"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/export"
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
		name, err := exportOne(in, o)
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

// exportOne writes one demo's folder, and its info.json, and returns its name.
func exportOne(in string, o options) (string, error) {
	opts := export.Options{Radar: o.radar, Hz: o.hz, CSV: o.csv}
	if !o.quiet {
		opts.Progress = os.Stdout
	}
	if o.csv != "" {
		if st, err := os.Stat(in); err == nil && st.IsDir() {
			opts.CSV = "" // a folder is already the CSVs
		}
	}
	r, err := export.Write(in, o.out, "", opts)
	if err != nil {
		return "", err
	}
	if err := writeInfo(filepath.Join(r.Dir, "info.json"), o.info); err != nil {
		return "", err
	}
	fmt.Println(r)
	return r.Name, nil
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
