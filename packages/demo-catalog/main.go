// Command demo-catalog manages the list of demos the site shows: a row per
// demo in the database, and data/index.json in the bucket, generated from
// those rows (packages/catalog, docs/uploads.md).
//
//	demo-catalog list                         # what is published, and to whom
//	demo-catalog put nukepug                  # publish web/public/data/nukepug/
//	demo-catalog put ../some/folder/x         # ...or any exported folder
//	demo-catalog set nukepug -visibility public -kind pug
//	demo-catalog set nukepug -event ""        # an empty value clears a field
//	demo-catalog unlist nukepug               # off the site; files stay in the bucket
//	demo-catalog publish [-dry-run]           # regenerate the index from the database
//	demo-catalog import [-apply]              # once: register what the old index listed
//
// Run from packages/. Settings come from the environment, falling back to
// packages/api/.env: DATABASE_URL, and R2_ACCOUNT_ID, R2_ACCESS_KEY_ID,
// R2_SECRET_ACCESS_KEY, R2_BUCKET for an R2 API token scoped to the bucket.
//
// It replaced upload-data's merging of index files: the index used to be
// built from whatever folders sat on the publishing machine and merged into
// the published one, which cannot work once uploads are processed somewhere
// other than the machine that publishes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/catalog"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/envfile"
)

const usage = `usage: demo-catalog <command> [flags]

  list                      what is published, and to whom
  put <name|folder>...      publish exported demos (a bare name is web/public/data/<name>)
  set <name> [flags]        change what a published demo declares
  unlist <name>...          take demos off the site (their files stay in the bucket)
  publish [-dry-run]        regenerate data/index.json from the database
  import [-apply]           once: register every demo the published index lists

every command takes -env (default api/.env) and -pros (default web/src/pros.json)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "demo-catalog: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(os.Stderr, usage)
		return nil
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	envPath := fs.String("env", filepath.Join("api", ".env"), "settings file, for variables not already set")
	prosPath := fs.String("pros", filepath.Join("web", "src", "pros.json"), "the pro catalogue")
	dataDir := fs.String("data", filepath.Join("web", "public", "data"), "where put finds a demo given by name")
	dryRun := fs.Bool("dry-run", false, "publish: say what would change, change nothing")
	apply := fs.Bool("apply", false, "import: write the rows (without it, only compares)")
	decl := declFlags(fs)
	if err := fs.Parse(interleave(args)); err != nil {
		return err
	}

	if _, err := envfile.Load(*envPath); err != nil {
		return err
	}
	ctx := context.Background()
	p, err := catalog.FromEnv(ctx, *prosPath)
	if err != nil {
		return err
	}
	defer p.Close()
	p.Log = os.Stdout

	switch cmd {
	case "list":
		return list(ctx, p)
	case "put":
		return put(ctx, p, *dataDir, fs.Args())
	case "set":
		if fs.NArg() != 1 {
			return errors.New("set takes one demo name")
		}
		return set(ctx, p, fs.Arg(0), decl.info(fs))
	case "unlist":
		return unlist(ctx, p, fs.Args())
	case "publish":
		return publish(ctx, p, *dryRun, nil)
	case "import":
		return importPublished(ctx, p, *apply)
	}
	return fmt.Errorf("no command %q\n\n%s", cmd, usage)
}

func list(ctx context.Context, p *catalog.Publisher) error {
	rows, err := catalog.Ready(ctx, p.DB)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tMAP\tSCORE\tDATE\tKIND\tWHO SEES IT")
	for _, r := range rows {
		score := ""
		if s := r.Summary.Score; s != nil {
			score = fmt.Sprintf("%d-%d", s[0], s[1])
		}
		kind := r.Kind
		if pros := p.Pros.In(r.Summary.IDs); len(pros) > 0 {
			kind = strings.TrimSpace(kind + " " + strings.Join(pros, ","))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Summary.Map, score, r.Date, kind, r.Audience())
	}
	fmt.Fprintf(w, "\n%d demo(s)\n", len(rows))
	return w.Flush()
}

func put(ctx context.Context, p *catalog.Publisher, dataDir string, args []string) error {
	if len(args) == 0 {
		return errors.New("put: name the demos to publish")
	}
	dirs := make([]string, len(args))
	for i, a := range args {
		dirs[i] = a
		if !strings.ContainsAny(a, `/\`) {
			if st, err := os.Stat(a); err != nil || !st.IsDir() {
				dirs[i] = filepath.Join(dataDir, a)
			}
		}
	}
	_, err := p.Put(ctx, dirs)
	return err
}

func set(ctx context.Context, p *catalog.Publisher, name string, info catalog.Info) error {
	row, err := catalog.Load(ctx, p.DB, name)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("%s is not in the catalogue (demo-catalog list)", name)
	}
	if info == (catalog.Info{}) {
		return errors.New("set: nothing to change; give -visibility, -owner, -kind, -date, -event, -title or -sample")
	}
	d := row.Demo
	d.Apply(info)
	if err := catalog.Declare(ctx, p.DB, d); err != nil {
		return err
	}
	fmt.Printf("%s  [%s]\n", name, d.Audience())
	if w := d.Warning(); w != "" {
		fmt.Printf("  warning: %s\n", w)
	}
	return publish(ctx, p, false, nil)
}

func unlist(ctx context.Context, p *catalog.Publisher, names []string) error {
	if len(names) == 0 {
		return errors.New("unlist: name the demos to take off the site")
	}
	for _, name := range names {
		ok, err := catalog.Remove(ctx, p.DB, name)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Printf("  %s is not in the catalogue; nothing to unlist\n", name)
		}
	}
	if err := publish(ctx, p, false, names); err != nil {
		return err
	}
	fmt.Println("Unlisted demos are no longer served, but their files stay in the bucket.")
	return nil
}

// publish regenerates the index. unlisting names the demos it may drop.
func publish(ctx context.Context, p *catalog.Publisher, dryRun bool, unlisting []string) error {
	if dryRun {
		change, err := p.Preview(ctx, unlisting...)
		fmt.Printf("would write r2://%s/%s\n", p.Store.Bucket(), catalog.IndexKey)
		change.Print(os.Stdout)
		return err
	}
	change, err := p.Publish(ctx, unlisting...)
	if err != nil {
		return err
	}
	fmt.Printf("wrote r2://%s/%s\n", p.Store.Bucket(), catalog.IndexKey)
	change.Print(os.Stdout)
	return nil
}

// importPublished registers every demo the published index lists, taking
// the declaration from the index and measuring each payload afresh from the
// bucket. The index it would generate is compared with the published one,
// field by field, before anything is written: the move to the database
// should change nothing a viewer can see.
func importPublished(ctx context.Context, p *catalog.Publisher, apply bool) error {
	published, err := p.Store.Get(ctx, catalog.IndexKey)
	if err != nil {
		return err
	}
	var doc struct {
		Demos []catalog.Entry `json:"demos"`
	}
	if err := json.Unmarshal(published, &doc); err != nil {
		return fmt.Errorf("published index: %w", err)
	}

	var demos []catalog.Demo
	var measured []catalog.Measured
	var rows []catalog.Row
	for _, e := range doc.Demos {
		d := catalog.Demo{Name: e.Name, Visibility: e.Visibility, Owner: e.Owner, Kind: e.Kind,
			Event: e.Event, Date: e.Date, Title: e.Title, Sample: e.Sample}
		if d.Kind == "unknown" {
			d.Kind = ""
		}
		if d.Visibility == "" {
			d.Visibility = "private"
		}
		b, err := p.Store.Get(ctx, "data/"+e.Name+"/movement.json")
		if err != nil {
			return err
		}
		m, err := catalog.Measure(b)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		demos, measured = append(demos, d), append(measured, m)
		rows = append(rows, catalog.Row{Demo: d, Summary: m.Summary})
		fmt.Printf("  %s  [%s]\n", e.Name, d.Audience())
	}

	regenerated, err := catalog.Index(rows, p.Pros)
	if err != nil {
		return err
	}
	change, err := catalog.Diff(published, regenerated)
	if err != nil {
		return err
	}
	if len(change.Added)+len(change.Changed)+len(change.Removed) > 0 {
		fmt.Println("\nthe index generated from these rows differs from the published one:")
		change.Print(os.Stdout)
		printFieldDiffs(published, regenerated, change.Changed)
		return errors.New("nothing imported")
	}
	fmt.Printf("\n%d demo(s); the index generated from them equals the published one.\n", len(rows))
	if !apply {
		fmt.Println("Run again with -apply to register them.")
		return nil
	}

	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i := range demos {
		if err := catalog.Save(ctx, tx, demos[i], measured[i]); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Println("registered.")
	return publish(ctx, p, false, nil)
}

func printFieldDiffs(old, new []byte, names []string) {
	pick := func(index []byte) map[string]map[string]any {
		var doc struct{ Demos []map[string]any }
		json.Unmarshal(index, &doc)
		out := map[string]map[string]any{}
		for _, e := range doc.Demos {
			out[e["name"].(string)] = e
		}
		return out
	}
	a, b := pick(old), pick(new)
	for _, name := range names {
		var keys []string
		for k := range a[name] {
			keys = append(keys, k)
		}
		for k := range b[name] {
			if _, ok := a[name][k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			if !reflect.DeepEqual(a[name][k], b[name][k]) {
				fmt.Printf("    %s.%s: published %v, generated %v\n", name, k, a[name][k], b[name][k])
			}
		}
	}
}

// declaration is set's flags. Only the flags actually given declare anything:
// set -kind pug leaves the visibility alone.
type declaration struct {
	visibility, owner, kind, date, event, title, sample *string
}

func declFlags(fs *flag.FlagSet) *declaration {
	return &declaration{
		visibility: fs.String("visibility", "", "set: public | link | private"),
		owner:      fs.String("owner", "", "set: 64-bit steamid of the account it belongs to"),
		kind:       fs.String("kind", "", "set: pug | match"),
		date:       fs.String("date", "", "set: when it was played, YYYY-MM-DD"),
		event:      fs.String("event", "", "set: tournament or league"),
		title:      fs.String("title", "", "set: a readable name for lists"),
		sample:     fs.String("sample", "", "set: true | false - shown to signed-out visitors (must be public)"),
	}
}

func (d *declaration) info(fs *flag.FlagSet) catalog.Info {
	var info catalog.Info
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "visibility":
			info.Visibility = d.visibility
		case "owner":
			info.Owner = d.owner
		case "kind":
			info.Kind = d.kind
		case "date":
			info.Date = d.date
		case "event":
			info.Event = d.event
		case "title":
			info.Title = d.title
		case "sample":
			b, err := strconv.ParseBool(*d.sample)
			if err != nil {
				fmt.Fprintf(os.Stderr, "demo-catalog: -sample must be true or false\n")
				os.Exit(2)
			}
			info.Sample = &b
		}
	})
	return info
}

// interleave moves flags ahead of positional arguments, so
// `set nukepug -kind pug` reads the way people type it. The flag package
// stops at the first positional argument otherwise.
func interleave(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if !strings.Contains(name, "=") && !isBool(name) && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, rest...)
}

func isBool(name string) bool { return name == "dry-run" || name == "apply" }
