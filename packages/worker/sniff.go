package worker

import (
	"bytes"
	"errors"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// The first bytes of each kind of file an upload might be. The name it was
// uploaded under is the uploader's say-so; these are the file's.
var (
	cs2Magic  = []byte("PBDEMS2\x00")
	csgoMagic = []byte("HL2DEMO\x00")
	zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

// Sniff reports whether r is a CS2 demo, and whether it is zstd-compressed
// around one. Anything else is a permanent failure, said to the uploader.
//
// Checked before the parser sees the file, because the parser's reaction to
// a file that is not a demo at all is an opaque error several layers down.
func Sniff(r io.Reader) (zst bool, err error) {
	head := make([]byte, len(cs2Magic))
	if _, err := io.ReadFull(r, head); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, Permanent("the file is too short to be a demo")
		}
		return false, err
	}
	switch {
	case bytes.Equal(head, cs2Magic):
		return false, nil
	case bytes.Equal(head, csgoMagic):
		return false, Permanent("this is a CS:GO demo; only CS2 demos can be read")
	case bytes.HasPrefix(head, zstdMagic):
		dec, err := zstd.NewReader(io.MultiReader(bytes.NewReader(head), r), zstd.WithDecoderLowmem(true))
		if err != nil {
			return true, Permanent("the file is compressed, but not readably: %v", err)
		}
		defer dec.Close()
		inner, err := Sniff(dec)
		if err != nil {
			if IsPermanent(err) {
				return true, err
			}
			return true, Permanent("the file is compressed, but not readably: %v", err)
		}
		if inner {
			return true, Permanent("the file is compressed twice; upload the .dem or a single .dem.zst")
		}
		return true, nil
	default:
		return false, Permanent("the file is not a CS2 demo")
	}
}

// SniffFile is Sniff on the file at path.
func SniffFile(path string) (zst bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return Sniff(f)
}
