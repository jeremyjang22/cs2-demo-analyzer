package analytics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// FromDemo parses a .dem directly into a Match, writing nothing. It is the
// path for analysing a pile of demos you have no other use for: the same
// numbers as LoadDir on that demo's output folder, without the ~20 MB of CSV
// per demo in between. It costs a full parse, though, so a demo that is
// already parsed is better read with LoadDir.
func FromDemo(path string) (*Match, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	parser := demoinfocs.NewParser(f)
	defer parser.Close()

	m := &Match{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	parser.RegisterNetMessageHandler(func(h *msg.CDemoFileHeader) { m.Map = h.GetMapName() })
	// parser.TickRate() is unusable before parsing starts; ServerInfo carries
	// the real value. Same source round-collector records in its manifest.
	parser.RegisterNetMessageHandler(func(s *msg.CSVCMsg_ServerInfo) {
		if interval := s.GetTickInterval(); interval > 0 {
			m.TickRate = 1 / float64(interval)
		}
	})

	c := collector.New(parser, (*matchSink)(m))
	// A demo that stops mid-stream is still every round up to that point, and
	// most recordings end that way. The cut-off round has no winner, so Add
	// counts it as skipped rather than as anyone's loss.
	if err := c.Run(); err != nil && !errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Names = c.Names()
	return m, nil
}

// matchSink keeps the three small parts of each round and lets the tick
// stream - nearly all of a round's memory - go.
type matchSink Match

func (s *matchSink) Round(r *collector.Round) error {
	s.Rounds = append(s.Rounds, Round{Meta: r.Meta, Kills: r.Kills, Bomb: r.Bomb})
	return nil
}

func (s *matchSink) Close() error { return nil }
