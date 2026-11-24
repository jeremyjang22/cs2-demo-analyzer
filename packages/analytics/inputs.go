package analytics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// IsDemoFile reports whether path names a recording: a .dem, or a .dem.zst.
func IsDemoFile(path string) bool { return collector.IsDemoFile(path) }

// Open reads one input: a .dem or .dem.zst costs a full parse, a parsed folder
// loads from its small tables.
func Open(path string) (*Match, error) {
	if IsDemoFile(path) {
		return FromDemo(path)
	}
	return LoadDir(path)
}

// Inputs turns a command line into one path per demo. A folder that is not
// itself a demo's output is searched one level down - for parsed output
// folders and for .dem / .dem.zst files alike - so `out` picks up every parsed
// demo in it, and a folder of recordings can be named as it is. The result is
// sorted within each folder so the same inputs always produce the same row
// order.
func Inputs(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		if IsDemoFile(arg) || IsDemoDir(arg) {
			out = append(out, arg)
			continue
		}
		entries, err := os.ReadDir(arg)
		if err != nil {
			return nil, fmt.Errorf("%s is not a demo or a folder: %w", arg, err)
		}
		var found []string
		for _, e := range entries {
			p := filepath.Join(arg, e.Name())
			if (e.IsDir() && IsDemoDir(p)) || (!e.IsDir() && IsDemoFile(p)) {
				found = append(found, p)
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s holds no demos and no round-collector output", arg)
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
}
