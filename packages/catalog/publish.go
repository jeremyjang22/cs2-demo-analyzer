package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/store"
)

// IndexKey is where the Worker reads the catalogue.
const IndexKey = "data/index.json"

// Publisher puts demos on the site: payloads to the bucket, rows to the
// database, and the index regenerated from the rows.
type Publisher struct {
	DB    *pgxpool.Pool
	Store *store.Store
	Pros  Pros
	Log   io.Writer
}

// FromEnv connects to DATABASE_URL and the R2 bucket, and reads the pro
// catalogue at prosPath. Close it when done.
func FromEnv(ctx context.Context, prosPath string) (*Publisher, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, errors.New("DATABASE_URL is not set (packages/api/.env, see docs/uploads.md)")
	}
	st, err := store.FromEnv()
	if err != nil {
		return nil, err
	}
	pros, err := LoadPros(prosPath)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	return &Publisher{DB: pool, Store: st, Pros: pros, Log: io.Discard}, nil
}

func (p *Publisher) Close() { p.DB.Close() }

func (p *Publisher) logf(format string, args ...any) { fmt.Fprintf(p.Log, format, args...) }

// Folder is an exported demo on disk: <name>/movement.json, and optionally
// analytics.json and info.json beside it - what demo-export writes.
type Folder struct {
	Dir      string
	Name     string
	Movement []byte
	Info     Info
	Measured Measured
	Demo     Demo // the declaration it will be published with
	New      bool // not in the catalogue before
}

// ReadFolder reads and measures an exported demo. Nothing is published.
func ReadFolder(dir string) (*Folder, error) {
	f := &Folder{Dir: dir, Name: filepath.Base(filepath.Clean(dir))}
	if err := CheckName(f.Name); err != nil {
		return nil, err
	}
	var err error
	if f.Movement, err = os.ReadFile(filepath.Join(dir, "movement.json")); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if f.Measured, err = Measure(f.Movement); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "info.json")); err == nil {
		if f.Info, err = ParseInfo(b); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return f, nil
}

// Put publishes exported demos. Every folder is read and checked before
// anything is uploaded, so one bad info.json publishes nothing rather than
// half a batch. A demo already in the catalogue keeps whatever its info.json
// does not declare (Demo.Apply).
func (p *Publisher) Put(ctx context.Context, dirs []string) ([]*Folder, error) {
	var folders []*Folder
	for _, dir := range dirs {
		f, err := ReadFolder(dir)
		if err != nil {
			return nil, err
		}
		existing, err := Load(ctx, p.DB, f.Name)
		if err != nil {
			return nil, err
		}
		f.Demo = Demo{Name: f.Name, Visibility: "private"}
		if existing != nil {
			f.Demo = existing.Demo
		} else {
			f.New = true
		}
		f.Demo.Apply(f.Info)
		if problems := f.Demo.Problems(); len(problems) > 0 {
			return nil, fmt.Errorf("%s: nothing was published: %s", f.Name, problems[0])
		}
		folders = append(folders, f)
	}

	// The index this ends with must be publishable, so find out before
	// uploading anything rather than after. The demos being put may be
	// missing from the catalogue now; they will not be by the end.
	names := make([]string, len(folders))
	for i, f := range folders {
		names[i] = f.Name
	}
	if _, err := p.Preview(ctx, names...); err != nil {
		return nil, fmt.Errorf("nothing was published: %w", err)
	}

	// Payloads first, rows second: a demo is never listed before its files
	// are there to serve.
	p.logf("publishing to r2://%s\n", p.Store.Bucket())
	for _, f := range folders {
		if err := p.Store.Put(ctx, "data/"+f.Name+"/movement.json", f.Movement, "application/json"); err != nil {
			return nil, err
		}
		// Optional: a demo without it still replays, and its analytics page
		// says there is nothing published.
		if b, err := os.ReadFile(filepath.Join(f.Dir, "analytics.json")); err == nil {
			if err := p.Store.Put(ctx, "data/"+f.Name+"/analytics.json", b, "application/json"); err != nil {
				return nil, err
			}
		}
		p.logf("  uploaded data/%s/  %d KB\n", f.Name, f.Measured.KB)
	}

	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, f := range folders {
		if err := Save(ctx, tx, f.Demo, f.Measured); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for _, f := range folders {
		verb := "replaced"
		if f.New {
			verb = "added"
		}
		p.logf("  %s %s  [%s]\n", verb, f.Name, f.Demo.Audience())
	}
	change, err := p.Publish(ctx)
	if err != nil {
		return folders, fmt.Errorf("saved, but the index was not republished (run demo-catalog publish): %w", err)
	}
	p.logf("wrote r2://%s/%s\n", p.Store.Bucket(), IndexKey)
	change.Print(p.Log)
	return folders, nil
}

// Audience is who d is shown to, for the line that says what is being
// published - so it also says to whom.
func (d Demo) Audience() string {
	switch {
	case d.Visibility == "public" && d.Sample:
		return "public, and shown signed out (sample)"
	case d.Visibility == "public":
		return "public: every signed-in account"
	case d.Owner == "":
		return d.Visibility + ", NO OWNER: nobody can open it"
	default:
		return d.Visibility + " to " + d.Owner
	}
}

// Print writes what changed, one line per kind of change.
func (c Change) Print(w io.Writer) {
	say := func(label string, names []string) {
		if len(names) > 0 {
			fmt.Fprintf(w, "  %s: %s\n", label, strings.Join(names, ", "))
		}
	}
	say("listed", c.Added)
	say("updated", c.Changed)
	say("unlisted", c.Removed)
	if len(c.Added)+len(c.Changed)+len(c.Removed) == 0 {
		fmt.Fprintln(w, "  no change to the index")
	}
	fmt.Fprintf(w, "  %d demo(s) in the index\n", c.Total)
}

// Change is what a publish did to the index, by demo name.
type Change struct {
	Added, Changed, Removed []string
	Total                   int
}

// Publish regenerates data/index.json from the database and writes it to
// the bucket.
//
// Serialised by an advisory lock held across reading the rows and writing the
// file. Without it two publishers could each read the catalogue, and the one
// that read first could write last - an index missing the other's demo, with
// nothing wrong in the database to show for it.
//
// It refuses to drop a demo the published index lists unless that demo is
// named in unlisting. A catalogue that has lost rows - an empty database
// before the first import, DATABASE_URL pointed at the wrong one - would
// otherwise publish an index that hides every demo it does not know, and the
// Worker serves nothing the index does not list.
func (p *Publisher) Publish(ctx context.Context, unlisting ...string) (Change, error) {
	return p.publish(ctx, false, unlisting)
}

// Preview is Publish without writing anything.
func (p *Publisher) Preview(ctx context.Context, unlisting ...string) (Change, error) {
	return p.publish(ctx, true, unlisting)
}

func (p *Publisher) publish(ctx context.Context, dryRun bool, unlisting []string) (Change, error) {
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return Change{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('catalog.publish'))`); err != nil {
		return Change{}, err
	}
	rows, err := Ready(ctx, tx)
	if err != nil {
		return Change{}, err
	}
	index, err := Index(rows, p.Pros)
	if err != nil {
		return Change{}, err
	}
	old, err := p.Store.Get(ctx, IndexKey)
	if errors.Is(err, store.ErrNotFound) {
		old, err = []byte(`{"demos":[]}`), nil
	}
	if err != nil {
		return Change{}, err
	}
	change, err := Diff(old, index)
	if err != nil {
		return Change{}, err
	}
	var lost []string
	for _, name := range change.Removed {
		if !slices.Contains(unlisting, name) {
			lost = append(lost, name)
		}
	}
	if len(lost) > 0 {
		return change, fmt.Errorf("index not written: it would drop %s, which the published index lists and the catalogue does not have. "+
			"Wrong DATABASE_URL, or the catalogue not imported yet (demo-catalog import); to take a demo off the site, demo-catalog unlist <name>",
			strings.Join(lost, ", "))
	}
	if dryRun {
		return change, nil
	}
	if err := p.Store.Put(ctx, IndexKey, index, "application/json"); err != nil {
		return Change{}, err
	}
	return change, tx.Commit(ctx)
}

// Diff compares two index files entry by entry, ignoring key order and
// formatting, so it reports what a viewer would see change.
func Diff(old, new []byte) (Change, error) {
	a, err := entriesByName(old)
	if err != nil {
		return Change{}, fmt.Errorf("old index: %w", err)
	}
	b, err := entriesByName(new)
	if err != nil {
		return Change{}, fmt.Errorf("new index: %w", err)
	}
	c := Change{Total: len(b)}
	for name, e := range b {
		if prev, ok := a[name]; !ok {
			c.Added = append(c.Added, name)
		} else if !reflect.DeepEqual(prev, e) {
			c.Changed = append(c.Changed, name)
		}
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			c.Removed = append(c.Removed, name)
		}
	}
	slices.Sort(c.Added)
	slices.Sort(c.Changed)
	slices.Sort(c.Removed)
	return c, nil
}

func entriesByName(index []byte) (map[string]map[string]any, error) {
	var doc struct {
		Demos []map[string]any `json:"demos"`
	}
	if err := json.Unmarshal(index, &doc); err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, e := range doc.Demos {
		name, _ := e["name"].(string)
		out[name] = e
	}
	return out, nil
}
