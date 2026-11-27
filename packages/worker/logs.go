package worker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// JobLog is one attempt's log: the worker's steps and everything the
// processing printed, stored in the bucket when the attempt ends. Capped, so
// a parser that floods its output cannot fill memory; the cap is far above
// what a normal demo prints (a line a round).
type JobLog struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

const jobLogCap = 1 << 20

func NewJobLog() *JobLog { return &JobLog{} }

func (l *JobLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := jobLogCap - l.buf.Len(); room < len(p) {
		if !l.truncated {
			l.buf.Write(p[:max(room, 0)])
			l.buf.WriteString("\n[log truncated at 1 MB]\n")
			l.truncated = true
		}
		return len(p), nil
	}
	return l.buf.Write(p)
}

// Bytes is the log so far.
func (l *JobLog) Bytes() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Clone(l.buf.Bytes())
}

// DailyLog is the worker's own log as one file a day,
// <dir>/demo-worker-YYYY-MM-DD.log, keeping the last Keep days. For a worker
// started at login, which has no console to print to.
type DailyLog struct {
	Dir  string
	Keep int              // days of files to keep; 0 keeps everything
	Now  func() time.Time // the clock; nil is time.Now

	mu   sync.Mutex
	day  string
	file *os.File
}

const dailyPrefix = "demo-worker-"

func (d *DailyLog) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	day := now().Format("2006-01-02")
	if d.file == nil || day != d.day {
		if d.file != nil {
			d.file.Close()
		}
		if err := os.MkdirAll(d.Dir, 0o755); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(filepath.Join(d.Dir, dailyPrefix+day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, err
		}
		d.file, d.day = f, day
		d.prune()
	}
	return d.file.Write(p)
}

// prune deletes the files past Keep, oldest first. The names sort by date.
func (d *DailyLog) prune() {
	if d.Keep <= 0 {
		return
	}
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, dailyPrefix) && strings.HasSuffix(n, ".log") {
			logs = append(logs, n)
		}
	}
	sort.Strings(logs)
	for _, n := range logs[:max(len(logs)-d.Keep, 0)] {
		os.Remove(filepath.Join(d.Dir, n))
	}
}

func (d *DailyLog) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

// String is where today's file is, for the line that says so.
func (d *DailyLog) String() string {
	return filepath.Join(d.Dir, fmt.Sprintf("%s<date>.log", dailyPrefix))
}
