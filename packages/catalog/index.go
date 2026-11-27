package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Entry is one demo in data/index.json. The fields, their names and when each
// is left out are index-demos.mjs's, which wrote this file before the
// catalogue moved into the database; the web app (src/demos.ts) and the
// Worker (worker/access.ts) read it unchanged.
type Entry struct {
	Name       string                `json:"name"`
	Title      string                `json:"title,omitempty"`
	Kind       string                `json:"kind"`
	Event      string                `json:"event,omitempty"`
	Date       string                `json:"date,omitempty"`
	Visibility string                `json:"visibility"`
	Owner      string                `json:"owner,omitempty"`
	Sample     bool                  `json:"sample,omitempty"`
	Games      int                   `json:"games"`
	Pros       []string              `json:"pros"`
	Map        string                `json:"map"`
	Rounds     int                   `json:"rounds"`
	Players    int                   `json:"players"`
	KB         int                   `json:"kb"`
	Seconds    int                   `json:"seconds"`
	Score      *[2]int               `json:"score,omitempty"`
	Lines      map[string][5]float64 `json:"lines,omitempty"`
}

// Row is a published demo: its declaration and its measurements.
type Row struct {
	Demo
	Summary Summary
}

// NewEntry is a row as the index shows it. Which pros the demo contains is
// worked out here, from the roster, never stored or tagged by hand: adding a
// pro to pros.json lists their existing games the next time the index is
// generated.
func NewEntry(r Row, pros Pros) Entry {
	kind := r.Kind
	if kind == "" {
		kind = "unknown"
	}
	s := r.Summary
	return Entry{
		Name: r.Name, Title: r.Title, Kind: kind, Event: r.Event, Date: r.Date,
		Visibility: r.Visibility, Owner: r.Owner, Sample: r.Sample,
		Games: s.Games, Pros: pros.In(s.IDs), Map: s.Map, Rounds: s.Rounds,
		Players: s.Players, KB: s.KB, Seconds: s.Seconds, Score: s.Score, Lines: s.Lines,
	}
}

// Index is the whole data/index.json for rows, ordered by name and
// formatted as index-demos wrote it, so a diff against the old file shows
// only what actually changed. It refuses a row with problems rather than
// publishing it wider or narrower than whoever declared it meant.
func Index(rows []Row, pros Pros) ([]byte, error) {
	var problems []string
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		for _, p := range r.Problems() {
			problems = append(problems, r.Name+": "+p)
		}
		entries = append(entries, NewEntry(r, pros))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("index not written:\n  %s", strings.Join(problems, "\n  "))
	}
	slices.SortFunc(entries, func(a, b Entry) int { return compareNames(a.Name, b.Name) })
	// Not HTML-escaped: JSON.stringify writes "&" in a title as "&", and so
	// does this.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Demos []Entry `json:"demos"`
	}{entries}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// compareNames orders case-insensitively, then by text, which is close
// enough to the localeCompare the old index used that nobody reordering it by
// hand would notice. The app sorts the list itself; this only keeps the file
// stable.
func compareNames(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Pros maps every steamid in the pro catalogue (packages/web/src/pros.json)
// to its pro's slug. Pros play on alternates, so one pro has several.
type Pros map[string]string

// LoadPros reads the pro catalogue.
func LoadPros(path string) (Pros, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pro catalogue: %w", err)
	}
	var doc struct {
		Pros []struct {
			Slug     string   `json:"slug"`
			SteamIDs []string `json:"steamids"`
		} `json:"pros"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("pro catalogue %s: %w", path, err)
	}
	pros := Pros{}
	for _, p := range doc.Pros {
		for _, id := range p.SteamIDs {
			pros[id] = p.Slug
		}
	}
	return pros, nil
}

// In is the pros among ids: each slug once, sorted.
func (p Pros) In(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if slug, ok := p[id]; ok && !slices.Contains(out, slug) {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return out
}
