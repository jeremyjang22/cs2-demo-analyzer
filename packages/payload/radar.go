package payload

import (
	"fmt"
	"image"
	_ "image/png" // DecodeConfig needs the format registered
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// referenceWidth is the overview width Valve's "scale" is calibrated against.
const referenceWidth = 1024

// Radar is a map's overview: the world -> image transform the viewer draws
// with, and which floor's image a height belongs on.
//
// Valve ships each radar as a .png plus a .txt (KeyValues) describing how the
// screenshot was taken. pos_x/pos_y is the world coordinate of the top-left
// pixel and scale is world units per pixel; multi-level maps add a
// "verticalsections" block, one sub-block of altitudes per floor.
type Radar struct {
	PosX, PosY, Scale float64
	// Width is the default image's width in pixels.
	Width int
	// Sections is the floor names, "default" first and then the rest from the
	// highest floor down. Indexes into it ARE the payload's level numbers, so
	// the order is part of the contract.
	Sections []string

	// bands is every section that declares altitudes, in file order: Level
	// takes the first band containing a height, as the Python did.
	bands []band
}

type band struct {
	name   string
	lo, hi float64
}

// LoadRadar reads <dir>/<mapName>.txt and the images beside it.
func LoadRadar(dir, mapName string) (*Radar, error) {
	txt := filepath.Join(dir, mapName+".txt")
	png := filepath.Join(dir, mapName+".png")
	for _, p := range []string{txt, png} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("missing radar asset for %s: %s (add %s.png and %s.txt to %s - see the README there)",
				mapName, p, mapName, mapName, dir)
		}
	}

	raw, err := os.ReadFile(txt)
	if err != nil {
		return nil, err
	}
	root := parseKeyValues(string(raw))
	// The file wraps everything in one block named after the map.
	for len(root.keys) == 1 && root.blocks[root.keys[0]] != nil {
		root = root.blocks[root.keys[0]]
	}

	r := &Radar{}
	for _, f := range []struct {
		key string
		dst *float64
	}{{"pos_x", &r.PosX}, {"pos_y", &r.PosY}, {"scale", &r.Scale}} {
		v, ok := root.values[f.key]
		if !ok {
			return nil, fmt.Errorf("%s is missing %q", txt, f.key)
		}
		if *f.dst, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", txt, f.key, err)
		}
	}

	if vs := root.blocks["verticalsections"]; vs != nil {
		for _, name := range vs.keys {
			sec := vs.blocks[name]
			if sec == nil {
				continue
			}
			lo, err1 := strconv.ParseFloat(sec.values["AltitudeMin"], 64)
			hi, err2 := strconv.ParseFloat(sec.values["AltitudeMax"], 64)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("%s: section %q needs numeric AltitudeMin and AltitudeMax", txt, name)
			}
			r.bands = append(r.bands, band{name, lo, hi})
		}
	}

	width, err := pngWidth(png)
	if err != nil {
		return nil, err
	}
	r.Width = width
	// Valve's scale is per pixel of a 1024-wide overview, and the .txt never
	// says so. A higher-resolution export of the same radar keeps the same
	// scale, so taking it literally would stretch the world by the resolution
	// ratio and draw every position in the wrong place.
	if width != referenceWidth {
		r.Scale *= float64(referenceWidth) / float64(width)
	}

	var rest []band
	for _, b := range r.bands {
		if b.name == "default" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, mapName+"_"+b.name+".png")); err != nil {
			return nil, fmt.Errorf("%s declares a %q section but %s_%s.png is missing", mapName, b.name, mapName, b.name)
		}
		rest = append(rest, b)
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].hi > rest[j].hi })
	r.Sections = []string{"default"}
	for _, b := range rest {
		r.Sections = append(r.Sections, b.name)
	}
	return r, nil
}

// Level is the index into Sections of the floor a height is on. Everything is
// floor 0 on a single-level map.
func (r *Radar) Level(z float64) int {
	for _, b := range r.bands {
		if b.lo <= z && z < b.hi {
			for i, name := range r.Sections {
				if name == b.name {
					return i
				}
			}
		}
	}
	return 0
}

func pngWidth(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return cfg.Width, nil
}

// kvBlock is one KeyValues block with its keys in file order. Order matters:
// a height inside two sections' bands belongs to whichever comes first.
type kvBlock struct {
	keys   []string
	values map[string]string
	blocks map[string]*kvBlock
}

var kvToken = regexp.MustCompile(`"([^"]*)"|([{}])`)
var kvComment = regexp.MustCompile(`//[^\n]*`)

// parseKeyValues reads Valve's overview format: quoted keys, each followed by
// a quoted value or a braced block. Comments are // to end of line.
// Multi-level maps nest a block per floor, so this has to handle nesting -
// flattening would let one floor's AltitudeMin overwrite another's.
func parseKeyValues(text string) *kvBlock {
	var tokens []string
	for _, m := range kvToken.FindAllStringSubmatch(kvComment.ReplaceAllString(text, ""), -1) {
		if m[2] != "" {
			tokens = append(tokens, m[2])
		} else {
			tokens = append(tokens, m[1])
		}
	}
	pos := 0
	var block func() *kvBlock
	block = func() *kvBlock {
		b := &kvBlock{values: map[string]string{}, blocks: map[string]*kvBlock{}}
		for pos < len(tokens) {
			key := tokens[pos]
			pos++
			if key == "}" {
				break
			}
			if pos >= len(tokens) {
				break
			}
			if tokens[pos] == "{" {
				pos++
				if _, seen := b.blocks[key]; !seen {
					b.keys = append(b.keys, key)
				}
				b.blocks[key] = block()
				continue
			}
			if _, seen := b.values[key]; !seen {
				b.keys = append(b.keys, key)
			}
			b.values[key] = tokens[pos]
			pos++
		}
		return b
	}
	return block()
}
