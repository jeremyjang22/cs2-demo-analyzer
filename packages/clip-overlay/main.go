// clip-overlay draws the followed player's keys and mouse buttons over
// recorded clips.
//
// It reads the clip list clip-list wrote, pulls each clip's player and tick
// range out of the demo's ticks.csv.gz, and writes an ASS subtitle script per
// clip. Given ffmpeg and the folder CS Demo Manager recorded into, it also
// burns each script into a copy of the clip.
//
// Usage:
//
//	clip-overlay -clips clips.json -data ../out [-videos D:\videos -ffmpeg ffmpeg.exe] [-out overlays]
//
// See docs/clip-capture.md.
package main

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "clip-overlay: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	clipsPath := flag.String("clips", "clips.json", "the clip list clip-list wrote")
	dataDir := flag.String("data", "", "folder holding each demo's round-collector output (the clips' source)")
	videoDir := flag.String("videos", "", "folder CS Demo Manager recorded into, one folder per demo "+
		"(clip-list's -video-dir); with -ffmpeg, the clips found there get the overlay burnt in")
	ffmpeg := flag.String("ffmpeg", "", "ffmpeg executable, built with libass")
	outDir := flag.String("out", "overlays", "where the .ass scripts and overlaid videos go")
	x := flag.Int("x", 60, "overlay's left edge on a 1920x1080 frame")
	y := flag.Int("y", 600, "overlay's top edge on a 1920x1080 frame")
	flag.Parse()
	if *dataDir == "" {
		return errors.New("-data is required")
	}

	raw, err := os.ReadFile(*clipsPath)
	if err != nil {
		return err
	}
	var list analytics.ClipList
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s: %w", *clipsPath, err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	byDemo := make(map[string][]analytics.Clip)
	var order []string
	for _, c := range list.Clips {
		if byDemo[c.Demo] == nil {
			order = append(order, c.Demo)
		}
		byDemo[c.Demo] = append(byDemo[c.Demo], c)
	}

	for _, demo := range order {
		clips := byDemo[demo]
		samples, err := loadSamples(filepath.Join(*dataDir, demo, "ticks.csv.gz"), clips)
		if err != nil {
			return err
		}
		for i, c := range clips {
			script := ASS(samples[i], Options{X: *x, Y: *y, TickRate: c.TickRate, StartTick: c.StartTick})
			assPath := filepath.Join(*outDir, c.ID+".ass")
			if err := os.WriteFile(assPath, []byte(script), 0o644); err != nil {
				return err
			}
			fmt.Printf("%s: %d ticks -> %s\n", c.ID, len(samples[i]), assPath)

			if *ffmpeg == "" || *videoDir == "" {
				continue
			}
			video := filepath.Join(*videoDir, demo, fmt.Sprintf("sequence-%d-tick-%d-to-%d.mp4", c.Sequence, c.StartTick, c.EndTick))
			if _, err := os.Stat(video); err != nil {
				fmt.Printf("  no recording at %s, skipped\n", video)
				continue
			}
			if err := burn(*ffmpeg, video, assPath, filepath.Join(*outDir, c.ID+".mp4")); err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
		}
	}
	return nil
}

// burn re-encodes video with the script drawn on. FFmpeg's filter syntax
// treats the colon in a Windows drive letter as a separator, so it runs from
// the script's folder and names the script without a path.
func burn(ffmpeg, video, assPath, out string) error {
	absVideo, err := filepath.Abs(video)
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	cmd := exec.Command(ffmpeg, "-y", "-loglevel", "error", "-i", absVideo,
		"-vf", "ass="+filepath.Base(assPath),
		"-c:v", "libx264", "-crf", "18", "-preset", "medium", "-pix_fmt", "yuv420p",
		"-c:a", "copy", absOut)
	cmd.Dir = filepath.Dir(assPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	fmt.Printf("  -> %s\n", out)
	return nil
}

// loadSamples streams a demo's ticks.csv.gz once and keeps, for each clip,
// its player's rows inside its tick range, in tick order. The file is the
// big one (tens of MB gzipped), so it is never held whole.
func loadSamples(path string, clips []analytics.Clip) ([][]Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rd := csv.NewReader(gz)
	rd.ReuseRecord = true
	header, err := rd.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[h] = i
	}
	for _, name := range []string{"tick", "steamid", "buttons", "is_alive"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("%s has no %q column", path, name)
		}
	}

	out := make([][]Sample, len(clips))
	for line := 2; ; line++ {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		tick64, err := strconv.ParseInt(rec[col["tick"]], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: tick: %w", path, line, err)
		}
		tick := int32(tick64)
		steamid := rec[col["steamid"]]
		for i, c := range clips {
			// One tick past the end, for the buttons lag.
			if steamid != c.SteamID || tick < c.StartTick || tick > c.EndTick+1 {
				continue
			}
			s := Sample{Tick: tick, Alive: rec[col["is_alive"]] == "1"}
			s.Buttons, _ = strconv.ParseUint(rec[col["buttons"]], 10, 64)
			out[i] = append(out[i], s)
		}
	}
	return out, nil
}
