package collector

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDemoName(t *testing.T) {
	for path, want := range map[string]string{
		"data/game5.dem":                  "game5",
		"data/game5.dem.zst":              "game5",
		"team demo data/GAME5.DEM.ZST":    "GAME5",
		"05-11-2026_mirage_44-32-10.dem":  "05-11-2026_mirage_44-32-10",
		"folder.with.dots/anubis.pug.dem": "anubis.pug",
	} {
		if got := DemoName(path); got != want {
			t.Errorf("DemoName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestIsDemoFile(t *testing.T) {
	for path, want := range map[string]bool{
		"a.dem": true, "a.dem.zst": true, "A.DEM.ZST": true,
		"a.zst": false, "a.dem.gz": false, "manifest.json": false,
	} {
		if got := IsDemoFile(path); got != want {
			t.Errorf("IsDemoFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// A .dem.zst must read back as exactly the bytes that were compressed, or the
// parser sees a corrupt demo and fails somewhere far from the cause.
func TestOpenDemoDecompressesAZst(t *testing.T) {
	want := bytes.Repeat([]byte("PBDEMS2\x00 entity update "), 50_000)
	dir := t.TempDir()

	var packed bytes.Buffer
	enc, err := zstd.NewWriter(&packed)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(want)
	enc.Close()
	zst := filepath.Join(dir, "game.dem.zst")
	plain := filepath.Join(dir, "game.dem")
	if err := os.WriteFile(zst, packed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, want, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{zst, plain} {
		r, err := OpenDemo(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: read %d bytes, want the original %d", filepath.Base(path), len(got), len(want))
		}
	}
}

func TestOpenDemoRejectsAFileThatIsNotZstd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renamed.dem.zst")
	if err := os.WriteFile(path, []byte("PBDEMS2 - a plain demo with the wrong name"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenDemo(path)
	if err == nil {
		// The zstd reader may defer the header check to the first read.
		_, err = io.ReadAll(r)
		r.Close()
	}
	if err == nil {
		t.Error("a plain file named .dem.zst opened and read without error")
	}
}
