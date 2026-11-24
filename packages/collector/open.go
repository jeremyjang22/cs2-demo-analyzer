package collector

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// zstSuffix is how demos usually arrive when they are shared: a .dem is mostly
// redundant entity updates and compresses to around a third of its size.
const zstSuffix = ".zst"

// IsDemoFile reports whether path names a demo this package can open: a .dem,
// or a .dem.zst.
func IsDemoFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".dem") || strings.HasSuffix(lower, ".dem"+zstSuffix)
}

// DemoName is a demo's basename without its extensions - game5.dem.zst and
// game5.dem are both "game5" - and is what output folders and fact rows are
// keyed by.
func DemoName(path string) string {
	base := filepath.Base(path)
	if strings.HasSuffix(strings.ToLower(base), zstSuffix) {
		base = base[:len(base)-len(zstSuffix)]
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// OpenDemo opens a demo for parsing, decompressing a .dem.zst as it is read.
//
// Streaming rather than unpacking to disk first is the point: a team's worth
// of demos is several gigabytes compressed and roughly three times that
// unpacked, and the parser only ever reads forward, so nothing needs to exist
// as a file but the original.
func OpenDemo(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	if !strings.HasSuffix(strings.ToLower(path), zstSuffix) {
		return f, nil
	}
	// Low-memory mode: decoding runs far ahead of a parser that is busy
	// building entities, so the extra buffering of the default decoder would
	// only hold more of the demo in memory without making the parse faster.
	dec, err := zstd.NewReader(f, zstd.WithDecoderLowmem(true))
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: not a zstd stream: %w", filepath.Base(path), err)
	}
	return &zstdFile{Decoder: dec, f: f}, nil
}

// zstdFile closes both the decoder and the file under it.
type zstdFile struct {
	*zstd.Decoder
	f *os.File
}

func (z *zstdFile) Close() error {
	z.Decoder.Close()
	return z.f.Close()
}
