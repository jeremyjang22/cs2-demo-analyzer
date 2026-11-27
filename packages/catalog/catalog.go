// Package catalog is the list of demos the site can show: a row per demo in
// Postgres, and data/index.json, the projection of those rows that the Worker
// reads to decide who sees what (worker/access.ts).
//
// The database is the source of truth and the index is generated from it,
// never edited on its own. Until November 2026 it was the other way round -
// index.json was built from whatever folders sat on the publishing machine -
// which could not survive uploads processed somewhere else. docs/uploads.md
// has the design.
//
// A row holds two kinds of fact, kept apart as info.json and movement.json
// always kept them (scripts/demo-info.mjs):
//
//   - DECLARED by a person: visibility, owner, kind, event, date, title,
//     sample. Never guessed from a file name, a timestamp or the roster.
//   - MEASURED from the payload: Summary.
package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	// The same rule as the database's CHECK on demos.name.
	validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	steamID   = regexp.MustCompile(`^\d{17}$`)
	isoDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Visibilities and Kinds, as the database's enums spell them.
var (
	Visibilities = []string{"public", "link", "private"}
	Kinds        = []string{"pug", "match"}
)

// CheckName refuses a name that is not safe as both a storage key and a URL
// parameter.
func CheckName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("%q is not a usable demo name: letters, digits, '.', '_' and '-', starting with a letter or digit, at most 128", name)
	}
	return nil
}

// Demo is a catalogue row's declared half, with the name it is published
// under. Empty strings are undeclared.
type Demo struct {
	Name       string
	Visibility string // never empty: undeclared is private
	Owner      string // a 64-bit steamid
	Kind       string
	Event      string
	Date       string // YYYY-MM-DD, the day it was played
	Title      string
	Sample     bool
}

// Problems is what is wrong with d, as messages; empty when it can be
// published. The database enforces the same rules, but it says so in
// constraint names.
func (d Demo) Problems() []string {
	var out []string
	if err := CheckName(d.Name); err != nil {
		out = append(out, err.Error())
	}
	if !slices.Contains(Visibilities, d.Visibility) {
		out = append(out, fmt.Sprintf("visibility must be %s, not %q", strings.Join(Visibilities, ", "), d.Visibility))
	}
	if d.Kind != "" && !slices.Contains(Kinds, d.Kind) {
		out = append(out, fmt.Sprintf("kind must be %s, not %q", strings.Join(Kinds, " or "), d.Kind))
	}
	if d.Owner != "" && !steamID.MatchString(d.Owner) {
		out = append(out, fmt.Sprintf("owner must be a 64-bit steamid (17 digits), not %s", d.Owner))
	}
	if d.Date != "" && !validDate(d.Date) {
		out = append(out, fmt.Sprintf("date must be a real YYYY-MM-DD, not %q", d.Date))
	}
	// A sample is what a signed-out visitor is shown. The Worker would refuse
	// a private one to exactly the people it is for.
	if d.Sample && d.Visibility != "public" {
		out = append(out, fmt.Sprintf("a sample must be public (it is shown to signed-out visitors); visibility is %s", d.Visibility))
	}
	return out
}

// Warning is what an otherwise valid row will surprise someone with, or "".
func (d Demo) Warning() string {
	if d.Visibility != "public" && d.Owner == "" {
		return "not public and no owner, so nobody can open it"
	}
	return ""
}

// Info is an info.json: a declaration where every field may be absent. The
// file demo-export and set-info write beside each payload.
type Info struct {
	Kind       *string `json:"kind,omitempty"`
	Event      *string `json:"event,omitempty"`
	Date       *string `json:"date,omitempty"`
	Title      *string `json:"title,omitempty"`
	Visibility *string `json:"visibility,omitempty"`
	Owner      *string `json:"owner,omitempty"`
	Sample     *bool   `json:"sample,omitempty"`
}

// ParseInfo reads an info.json. An owner written as a number is accepted, as
// index-demos accepts it.
func ParseInfo(b []byte) (Info, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return Info{}, fmt.Errorf("info.json: %w", err)
	}
	if o, ok := raw["owner"]; ok && len(o) > 0 && o[0] != '"' && string(o) != "null" {
		raw["owner"] = json.RawMessage(`"` + string(o) + `"`)
	}
	fixed, _ := json.Marshal(raw)
	var info Info
	if err := json.Unmarshal(fixed, &info); err != nil {
		return Info{}, fmt.Errorf("info.json: %w", err)
	}
	return info, nil
}

// Apply lays a declaration over d: what info declares replaces d's value, and
// what it leaves out keeps it. So publishing from a machine whose info.json
// says less than the catalogue does not quietly make a public demo private.
// To clear a field, declare it empty.
func (d *Demo) Apply(info Info) {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&d.Kind, info.Kind)
	set(&d.Event, info.Event)
	set(&d.Date, info.Date)
	set(&d.Title, info.Title)
	set(&d.Owner, info.Owner)
	if info.Visibility != nil {
		d.Visibility = strings.TrimSpace(*info.Visibility)
		if d.Visibility == "" {
			d.Visibility = "private"
		}
	}
	if info.Sample != nil {
		d.Sample = *info.Sample
	}
}

func validDate(s string) bool {
	if !isoDate.MatchString(s) {
		return false
	}
	var y, m, d int
	fmt.Sscanf(s, "%d-%d-%d", &y, &m, &d)
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[m-1]
	if m == 2 && (y%4 == 0 && (y%100 != 0 || y%400 == 0)) {
		days = 29
	}
	return d <= days
}
