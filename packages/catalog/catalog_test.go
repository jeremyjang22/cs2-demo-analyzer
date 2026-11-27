package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The contract with the JavaScript: testdata/summary.json is what
// demo-info.mjs's measure() makes of testdata/movement.json, and the web
// package's test checks it still does. The fixture covers the rules that
// were each a bug once: teammate and self damage are not ADR, hpt not hp,
// halves round up, rounds sort by number, a player with no stats line reads
// as zeros.
func TestSummaryMatchesTheJavaScript(t *testing.T) {
	movement := read(t, "testdata/movement.json")
	m, err := Measure(movement)
	if err != nil {
		t.Fatal(err)
	}
	got := asJSON(t, m.Summary, "ids")
	want := asJSON(t, json.RawMessage(read(t, "testdata/summary.json")))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary differs from the JavaScript's\n got %v\nwant %v", got, want)
	}
	if strings.Join(m.IDs, ",") != "76561198000000001,76561198000000002,76561198074762801,76561198000000004" {
		t.Errorf("ids = %v", m.IDs)
	}
	if m.TickRate != 64 || len(m.Roster) != 4 || m.Roster[2].Name != "m0NESY" || m.Roster[3].K != 0 {
		t.Errorf("tick rate %v, roster %+v", m.TickRate, m.Roster)
	}
}

func TestMergedPayloadHasNoScore(t *testing.T) {
	m, err := Measure([]byte(`{"demos":[{},{}],"rounds":{
		"1":{"n":1,"dm":0,"t0":0,"dur":10},"2":{"n":2,"dm":0,"t0":10,"dur":5},
		"3":{"n":1,"dm":1,"t0":0,"dur":7.5}},"players":[{"id":"1","team":0}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Games != 2 || m.Score != nil || m.Lines != nil || m.Seconds != 23 || m.Map != "unknown" {
		t.Errorf("got %+v", m.Summary)
	}
}

func TestEntryFollowsIndexDemos(t *testing.T) {
	m, err := Measure(read(t, "testdata/movement.json"))
	if err != nil {
		t.Fatal(err)
	}
	pros := Pros{"76561198074762801": "m0nesy", "76561198999999999": "m0nesy"}
	b, err := Index([]Row{
		{Demo: Demo{Name: "zeta", Visibility: "private", Owner: "76561198230376396"}, Summary: m.Summary},
		{Demo: Demo{Name: "Alpha", Visibility: "public", Sample: true, Kind: "pug", Title: "A & B"}, Summary: m.Summary},
	}, pros)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{`"A & B"`, `"pros": [` + "\n" + `        "m0nesy"`, `"kind": "unknown"`, `"sample": true`} {
		if !strings.Contains(text, want) {
			t.Errorf("index lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"ids"`) || strings.Index(text, "Alpha") > strings.Index(text, "zeta") {
		t.Errorf("ids leaked, or not ordered by name:\n%s", text)
	}
}

func TestIndexRefusesAPrivateSample(t *testing.T) {
	_, err := Index([]Row{{Demo: Demo{Name: "x", Visibility: "private", Sample: true}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "sample must be public") {
		t.Errorf("err = %v", err)
	}
}

func TestApplyKeepsWhatInfoLeavesOut(t *testing.T) {
	d := Demo{Name: "x", Visibility: "public", Owner: "76561198230376396", Kind: "pug", Sample: true}
	info, err := ParseInfo([]byte(`{"date":"2026-08-14","owner":76561198000000001}`))
	if err != nil {
		t.Fatal(err)
	}
	d.Apply(info)
	want := Demo{Name: "x", Visibility: "public", Owner: "76561198000000001", Kind: "pug", Date: "2026-08-14", Sample: true}
	if d != want {
		t.Errorf("got %+v", d)
	}
	empty := ""
	d.Apply(Info{Kind: &empty, Visibility: &empty})
	if d.Kind != "" || d.Visibility != "private" {
		t.Errorf("clearing: got %+v", d)
	}
}

func TestProblems(t *testing.T) {
	bad := Demo{Name: "../x", Visibility: "everyone", Kind: "scrim", Owner: "123", Date: "2026-02-30"}
	if got := len(bad.Problems()); got != 5 {
		t.Errorf("%d problems, want 5: %v", got, bad.Problems())
	}
	good := Demo{Name: "1-604132bd-e063-4e28-acfa-c2b4dc1d670c-1-1", Visibility: "link", Date: "2024-02-29"}
	if p := good.Problems(); len(p) != 0 {
		t.Errorf("good: %v", p)
	}
}

// The published index's real demos, regenerated from their info.json and
// movement.json, must equal what index-demos.mjs wrote for them. Opt in with
// CATALOG_DATA=<packages/web/public/data> after `npm run dev` or
// `node scripts/index-demos.mjs` has written its index.json.
func TestMatchesIndexDemos(t *testing.T) {
	dir := os.Getenv("CATALOG_DATA")
	if dir == "" {
		t.Skip("set CATALOG_DATA to a public/data folder with an index.json from index-demos")
	}
	pros, err := LoadPros(filepath.Join("..", "web", "src", "pros.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Demos []json.RawMessage `json:"demos"`
	}
	if err := json.Unmarshal(read(t, filepath.Join(dir, "index.json")), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Demos) == 0 {
		t.Fatal("the index lists no demos")
	}
	for _, raw := range index.Demos {
		var name struct{ Name string }
		json.Unmarshal(raw, &name)
		row := Row{Demo: Demo{Name: name.Name, Visibility: "private"}}
		if b, err := os.ReadFile(filepath.Join(dir, name.Name, "info.json")); err == nil {
			info, err := ParseInfo(b)
			if err != nil {
				t.Fatal(err)
			}
			row.Apply(info)
		}
		m, err := Measure(read(t, filepath.Join(dir, name.Name, "movement.json")))
		if err != nil {
			t.Fatal(err)
		}
		row.Summary = m.Summary
		got, want := asJSON(t, NewEntry(row, pros)), asJSON(t, raw)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %v\nwant %v", name.Name, got, want)
		}
	}
	t.Logf("%d demos match", len(index.Demos))
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// asJSON is v as generic JSON, for comparing what two encoders wrote
// without caring about key order. Fields named in drop are removed.
func asJSON(t *testing.T, v any, drop ...string) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range drop {
		delete(out, k)
	}
	return out
}
