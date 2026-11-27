package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func zstdOf(t *testing.T, b []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	return enc.EncodeAll(b, nil)
}

func TestSniff(t *testing.T) {
	demo := append([]byte("PBDEMS2\x00"), bytes.Repeat([]byte{7}, 100)...)
	for name, c := range map[string]struct {
		in        []byte
		zst       bool
		permanent string // "" for no error
	}{
		"cs2":          {in: demo},
		"cs2 in zstd":  {in: zstdOf(t, demo), zst: true},
		"csgo":         {in: []byte("HL2DEMO\x00rest"), permanent: "CS:GO"},
		"text":         {in: []byte("hello, this is not a demo"), permanent: "not a CS2 demo"},
		"tiny":         {in: []byte("PB"), permanent: "too short"},
		"zstd of text": {in: zstdOf(t, []byte("not a demo either")), zst: true, permanent: "not a CS2 demo"},
		"zstd twice":   {in: zstdOf(t, zstdOf(t, demo)), zst: true, permanent: "twice"},
		"broken zstd":  {in: append([]byte{0x28, 0xb5, 0x2f, 0xfd}, bytes.Repeat([]byte{0xff}, 40)...), zst: true, permanent: "not readably"},
	} {
		zst, err := Sniff(bytes.NewReader(c.in))
		if c.permanent == "" {
			if err != nil || zst != c.zst {
				t.Errorf("%s: zst=%v err=%v", name, zst, err)
			}
			continue
		}
		if !IsPermanent(err) || !strings.Contains(err.Error(), c.permanent) {
			t.Errorf("%s: want a permanent failure about %q, got %v", name, c.permanent, err)
		}
	}
}

func TestReason(t *testing.T) {
	if got := Reason(Permanent("the file is not a CS2 demo")); got != "the file is not a CS2 demo" {
		t.Errorf("permanent: %q", got)
	}
	if got := Reason(errors.New("connection reset")); got != "processing failed: connection reset" {
		t.Errorf("other: %q", got)
	}
	if got := Reason(fmt.Errorf("wrapped: %w", Permanent("x"))); got != "wrapped: x" {
		t.Errorf("wrapped permanent: %q", got)
	}
	if got := Reason(errors.New(strings.Repeat("é", 400))); len([]rune(got)) != 300 {
		t.Errorf("long: %d runes", len([]rune(got)))
	}
}

func TestLastLine(t *testing.T) {
	l := &lastLine{}
	fmt.Fprint(l, "  round  1\n  round  2\n\n")
	fmt.Fprint(l, "error: the demo could not be")
	fmt.Fprint(l, " read: bad packet\n\n")
	if got := l.String(); got != "error: the demo could not be read: bad packet" {
		t.Errorf("got %q", got)
	}
	fmt.Fprint(l, "no newline at the end")
	if got := l.String(); got != "no newline at the end" {
		t.Errorf("unterminated: %q", got)
	}
}

func TestJobLogCap(t *testing.T) {
	l := NewJobLog()
	line := bytes.Repeat([]byte("x"), 1000)
	for range 2000 {
		l.Write(line)
	}
	b := l.Bytes()
	if len(b) > jobLogCap+100 || !bytes.HasSuffix(b, []byte("[log truncated at 1 MB]\n")) {
		t.Errorf("%d bytes, ends %q", len(b), b[len(b)-30:])
	}
}

func TestDailyLog(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC)
	d := &DailyLog{Dir: dir, Keep: 3, Now: func() time.Time { return day }}
	defer d.Close()
	for i := range 6 {
		fmt.Fprintf(d, "line on day %d\n", i)
		fmt.Fprintf(d, "second line on day %d\n", i)
		day = day.Add(24 * time.Hour)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"demo-worker-2026-11-04.log", "demo-worker-2026-11-05.log", "demo-worker-2026-11-06.log"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("files %v, want %v", names, want)
	}
	d.Close()
	b, _ := os.ReadFile(filepath.Join(dir, want[2]))
	if string(b) != "line on day 5\nsecond line on day 5\n" {
		t.Errorf("last file: %q", b)
	}
}

func TestCompress(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.dem"), filepath.Join(dir, "a.dem.zst")
	body := bytes.Repeat([]byte("PBDEMS2\x00 entity update "), 10000)
	os.WriteFile(src, body, 0o644)
	n, err := compress(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	packed, _ := os.ReadFile(dst)
	if int64(len(packed)) != n || n >= int64(len(body))/10 {
		t.Errorf("%d bytes from %d", n, len(body))
	}
	dec, _ := zstd.NewReader(nil)
	back, err := dec.DecodeAll(packed, nil)
	if err != nil || !bytes.Equal(back, body) {
		t.Errorf("round trip: %v", err)
	}
}

// The test binary is its own child process here: run with
// WORKER_TEST_CHILD set, it behaves like `demo-worker process` would.
func TestMain(m *testing.M) {
	switch os.Getenv("WORKER_TEST_CHILD") {
	case "":
		os.Exit(m.Run())
	case "ok":
		fmt.Println("  round  1")
		os.Exit(0)
	case "unreadable":
		fmt.Println("  round  1")
		fmt.Println("error: the demo could not be read: bad packet")
		os.Exit(ExitUnreadable)
	case "flaky":
		fmt.Println("error: no space left on device")
		os.Exit(1)
	case "crash":
		fmt.Println("panic: runtime error")
		fmt.Println("goroutine 7 [running]:")
		os.Exit(2)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestSubprocess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string, timeout time.Duration) (error, string) {
		t.Setenv("WORKER_TEST_CHILD", mode)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		log := NewJobLog()
		err := Subprocess(exe)(ctx, "x.dem", t.TempDir(), "x", log)
		return err, string(log.Bytes())
	}

	if err, log := run("ok", time.Minute); err != nil || !strings.Contains(log, "round  1") {
		t.Errorf("ok: %v, log %q", err, log)
	}
	if err, _ := run("unreadable", time.Minute); !IsPermanent(err) || err.Error() != "the demo could not be read: bad packet" {
		t.Errorf("unreadable: want permanent, got %v", err)
	}
	if err, _ := run("flaky", time.Minute); err == nil || IsPermanent(err) || err.Error() != "no space left on device" {
		t.Errorf("flaky: want retryable, got %v", err)
	}
	if err, _ := run("crash", time.Minute); err == nil || IsPermanent(err) || !strings.Contains(err.Error(), "stopped unexpectedly") {
		t.Errorf("crash: %v", err)
	}
	start := time.Now()
	if err, _ := run("hang", 500*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("hang: %v after %v", err, time.Since(start))
	}
}
