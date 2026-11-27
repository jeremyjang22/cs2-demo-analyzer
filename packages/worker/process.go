package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ExitUnreadable is how `demo-worker process` says the demo itself could not
// be read: the same file will fail the same way, so the job fails at once
// rather than being tried three times. Any other failure - the child killed,
// out of memory, a crash outside the parse - is worth another try.
const ExitUnreadable = 3

// Subprocess processes each demo in a child process: exe with args, then
// -out, -name and the demo, as `demo-worker process` takes them.
//
// A child rather than a function call, because the file is someone else's
// and the parser is a large library reading it. A panic on one of its
// goroutines, or a demo that drives memory through the roof, ends the child
// and fails one job, where in-process it would end the worker. The timeout
// is enforced by killing it, which a goroutine cannot be. And a finished
// child gives every byte it used back to the machine - this one is the
// owner's PC.
func Subprocess(exe string, args ...string) ProcessFunc {
	return func(ctx context.Context, demo, out, name string, log io.Writer) error {
		cmd := exec.CommandContext(ctx, exe, append(append([]string{}, args...), "-out", out, "-name", name, demo)...)
		last := &lastLine{}
		w := io.MultiWriter(log, last)
		cmd.Stdout, cmd.Stderr = w, w
		cmd.WaitDelay = 5 * time.Second
		background(cmd)
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// `process` ends a failure with "error: <why>"; anything else is
			// the child dying, whose last line is the middle of a stack trace.
			if msg, ok := strings.CutPrefix(last.String(), "error: "); ok {
				var exit *exec.ExitError
				if errors.As(err, &exit) && exit.ExitCode() == ExitUnreadable {
					return Permanent("%s", msg)
				}
				return fmt.Errorf("%s", msg)
			}
			return fmt.Errorf("the parser stopped unexpectedly (%v); the job log has its output", err)
		}
		return nil
	}
}

// lastLine keeps the last non-empty line written to it.
type lastLine struct {
	mu   sync.Mutex
	line []byte
	part []byte
}

func (l *lastLine) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.part = append(l.part, p...)
	for {
		i := bytes.IndexByte(l.part, '\n')
		if i < 0 {
			break
		}
		if line := bytes.TrimSpace(l.part[:i]); len(line) > 0 {
			l.line = append(l.line[:0], line...)
		}
		l.part = l.part[i+1:]
	}
	if len(l.part) > 4096 { // a line that never ends is not a message
		l.part = l.part[len(l.part)-4096:]
	}
	return len(p), nil
}

func (l *lastLine) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if line := bytes.TrimSpace(l.part); len(line) > 0 {
		return string(line)
	}
	return string(l.line)
}
