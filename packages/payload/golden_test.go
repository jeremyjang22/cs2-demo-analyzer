package payload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector/csvsource"
)

// TestMatchesPythonExport rebuilds each demo from its CSV folder and compares
// the result, value by value, with a movement.json the Python exporter wrote
// from the same folder. It is how this port earned the right to replace
// movement_payload.py, and the check to re-run if either side changes.
//
// It needs demos, which never enter the repo, so it runs only when asked:
//
//	PAYLOAD_GOLDEN=<folder of <demo>/movement.json>  go test ./payload -run Python
//
// with the parsed folders in <repo>/out (PAYLOAD_OUT overrides). Numbers are
// compared as JSON numbers, so the Python's 3.0 equals Go's 3.
func TestMatchesPythonExport(t *testing.T) {
	golden := os.Getenv("PAYLOAD_GOLDEN")
	if golden == "" {
		t.Skip("PAYLOAD_GOLDEN not set: needs Python-exported payloads to compare against")
	}
	outDir := os.Getenv("PAYLOAD_OUT")
	if outDir == "" {
		outDir = filepath.Join("..", "..", "out")
	}
	radarDir := filepath.Join("..", "..", "assets", "radar")

	entries, err := os.ReadDir(golden)
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, e := range entries {
		want := filepath.Join(golden, e.Name(), "movement.json")
		dir := filepath.Join(outDir, e.Name())
		if _, err := os.Stat(want); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			continue
		}
		compared++
		t.Run(e.Name(), func(t *testing.T) {
			got, err := buildFromFolder(dir, radarDir)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(want)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if err := json.Unmarshal(raw, &a); err != nil {
				t.Fatalf("reference: %v", err)
			}
			if err := json.Unmarshal(got, &b); err != nil {
				t.Fatalf("ours: %v", err)
			}
			var diffs []string
			diff("", a, b, &diffs)
			if len(diffs) > 0 {
				shown := diffs
				if len(shown) > 25 {
					shown = shown[:25]
				}
				t.Errorf("%d differences (python vs go), first %d:\n  %s", len(diffs), len(shown),
					joinLines(shown))
			}
		})
	}
	if compared == 0 {
		t.Fatalf("no demo in %s has a parsed folder in %s", golden, outDir)
	}
}

func buildFromFolder(dir, radarDir string) ([]byte, error) {
	f, err := csvsource.Open(dir)
	if err != nil {
		return nil, err
	}
	b := NewBuilder(filepath.Base(dir), radarDir, DefaultHz)
	b.SetMap(f.Manifest.Map)
	b.SetTickRate(f.Manifest.TickRate)
	if err := f.Run(b); err != nil {
		return nil, err
	}
	p, err := b.Finish(f.Names)
	if err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// diff records every path where two decoded JSON values disagree.
func diff(path string, a, b any, out *[]string) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: object vs %T", path, b))
			return
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			x, inA := av[k]
			y, inB := bv[k]
			switch {
			case !inA:
				*out = append(*out, fmt.Sprintf("%s.%s: only in go (%v)", path, k, short(y)))
			case !inB:
				*out = append(*out, fmt.Sprintf("%s.%s: only in python (%v)", path, k, short(x)))
			default:
				diff(path+"."+k, x, y, out)
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: array vs %T", path, b))
			return
		}
		if len(av) != len(bv) {
			*out = append(*out, fmt.Sprintf("%s: length %d vs %d", path, len(av), len(bv)))
		}
		for i := 0; i < min(len(av), len(bv)); i++ {
			diff(fmt.Sprintf("%s[%d]", path, i), av[i], bv[i], out)
		}
	default:
		if a != b {
			*out = append(*out, fmt.Sprintf("%s: %v vs %v", path, short(a), short(b)))
		}
	}
}

func short(v any) string {
	s := fmt.Sprint(v)
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}
