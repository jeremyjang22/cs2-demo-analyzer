package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/analytics"
)

// csdmConfig is CS Demo Manager's `csdm video --config-file` input
// (VideoCommandConfig in its src/cli/commands/video-command.ts). Anything left
// out falls back to the video settings in the CS Demo Manager app, so
// resolution, framerate, codec and the HLAE/FFmpeg choice live there.
type csdmConfig struct {
	DemoPath                string         `json:"demoPath"`
	OutputFolderPath        string         `json:"outputFolderPath,omitempty"`
	OutputFileName          string         `json:"outputFileName"`
	ConcatenateSequences    bool           `json:"concatenateSequences"`
	CloseGameAfterRecording bool           `json:"closeGameAfterRecording"`
	TrueView                bool           `json:"trueView"`
	Sequences               []csdmSequence `json:"sequences"`
}

type csdmSequence struct {
	Number               int          `json:"number"`
	StartTick            int32        `json:"startTick"`
	EndTick              int32        `json:"endTick"`
	ShowXRay             bool         `json:"showXRay"`
	ShowAssists          bool         `json:"showAssists"`
	ShowOnlyDeathNotices bool         `json:"showOnlyDeathNotices"`
	PlayersOptions       []struct{}   `json:"playersOptions"`
	PlayerCameras        []csdmCamera `json:"playerCameras"`
	Cameras              []struct{}   `json:"cameras"`
	PlayerVoicesEnabled  bool         `json:"playerVoicesEnabled"`
	RecordAudio          bool         `json:"recordAudio"`
	DeathNoticesDuration int          `json:"deathNoticesDuration"`
}

type csdmCamera struct {
	Tick          int32  `json:"tick"`
	PlayerSteamID string `json:"playerSteamId"`
	PlayerName    string `json:"playerName"`
}

// csdmOptions are the clip-list flags that only matter to CS Demo Manager.
type csdmOptions struct {
	dir      string // where the config files go
	demoDir  string // where the .dem files are on the recording machine
	videoDir string // where CS Demo Manager writes; empty for its default
	reel     bool   // join each demo's clips into one video
}

// writeCSDM writes one config per demo: CS Demo Manager records one demo per
// video job.
func writeCSDM(clips []analytics.Clip, opt csdmOptions) error {
	if err := os.MkdirAll(opt.dir, 0o755); err != nil {
		return err
	}
	var order []string
	byDemo := make(map[string][]analytics.Clip)
	for _, c := range clips {
		if byDemo[c.Demo] == nil {
			order = append(order, c.Demo)
		}
		byDemo[c.Demo] = append(byDemo[c.Demo], c)
	}
	for _, demo := range order {
		cfg := csdmConfig{
			DemoPath:                joinWindows(opt.demoDir, demo+".dem"),
			OutputFileName:          demo,
			ConcatenateSequences:    opt.reel,
			CloseGameAfterRecording: true,
			TrueView:                true,
		}
		if opt.videoDir != "" {
			cfg.OutputFolderPath = joinWindows(opt.videoDir, demo)
		}
		for _, c := range byDemo[demo] {
			cfg.Sequences = append(cfg.Sequences, csdmSequence{
				Number: c.Sequence, StartTick: c.StartTick, EndTick: c.EndTick,
				ShowAssists: true, ShowOnlyDeathNotices: true,
				PlayersOptions: []struct{}{}, Cameras: []struct{}{},
				// One camera at the first tick: CS Demo Manager keeps the
				// view on that player until the sequence ends.
				PlayerCameras: []csdmCamera{{Tick: c.StartTick, PlayerSteamID: c.SteamID, PlayerName: c.PlayerName}},
				RecordAudio:   true, DeathNoticesDuration: 5,
			})
		}
		raw, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(opt.dir, demo+".json")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// joinWindows joins a path for the recording machine, which is Windows
// whatever machine runs clip-list. A relative demo dir stays relative: csdm
// resolves it against the folder it is started in.
func joinWindows(dir, name string) string {
	if dir == "" {
		return name
	}
	return strings.TrimRight(dir, `\/`) + `\` + name
}
