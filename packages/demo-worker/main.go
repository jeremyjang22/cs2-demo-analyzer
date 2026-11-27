// Command demo-worker processes demos uploaded through the site: it waits for
// jobs in the queue, builds each upload into the site's files, and publishes
// it to its uploader.
//
//	demo-worker                        # run until stopped (Ctrl-C hands the job back)
//	demo-worker -once                  # empty the queue, then exit
//	demo-worker -log-dir <dir>         # also log to <dir>/demo-worker-<date>.log
//
// On the owner's PC it is started at login by a scheduled task
// (scripts/worker/install-worker.ps1); in the cloud it is a service. The
// settings are the same names in both: DATABASE_URL and R2_*, from the
// environment or -env. docs/demo-worker.md has the details and where to look
// when something goes wrong.
//
// `demo-worker process` is the child each demo is built in - see
// worker.Subprocess for why a child - and is not for running by hand;
// demo-export does the same from a terminal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/catalog"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/envfile"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/export"
	"github.com/jeremyjang22/cs2-demo-analyzer/packages/worker"
)

// version is set at build time (-ldflags "-X main.version=..."); otherwise
// it is read from the build's VCS stamp.
var version = ""

func main() {
	if len(os.Args) > 1 && os.Args[1] == "process" {
		os.Exit(process(os.Args[2:]))
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "demo-worker: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	host, _ := os.Hostname()
	var (
		env     = flag.String("env", filepath.Join("api", ".env"), "settings file, for DATABASE_URL and R2_* not already set")
		pros    = flag.String("pros", filepath.Join("web", "src", "pros.json"), "the pro catalogue, read again for every publish")
		radar   = flag.String("radar", filepath.Join("..", "assets", "radar"), "radar overview images and .txt files")
		scratch = flag.String("scratch", filepath.Join(os.TempDir(), "cs2-demo-worker"), "where a job's files go while it runs")
		logDir  = flag.String("log-dir", "", "also write the log to a file a day in this folder, keeping two weeks")
		name    = flag.String("name", host, "this worker's name in the workers table")
		once    = flag.Bool("once", false, "process what is queued, then exit")
		poll    = flag.Duration("poll", 5*time.Second, "how often to look for work when idle")
		timeout = flag.Duration("timeout", 15*time.Minute, "the longest one demo may take to process")
	)
	flag.Parse()

	var logOut io.Writer = os.Stderr
	if *logDir != "" {
		daily := &worker.DailyLog{Dir: *logDir, Keep: 14}
		defer daily.Close()
		logOut = tee{daily, os.Stderr}
	}
	log := slog.New(slog.NewJSONHandler(logOut, nil))

	if _, err := envfile.Load(*env); err != nil {
		return err
	}
	// Absolute, because the child process is given them and the scheduled
	// task's working directory is not something to rely on twice.
	for _, p := range []*string{radar, pros, scratch} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
	if _, err := os.Stat(*radar); err != nil {
		return fmt.Errorf("-radar: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pub, err := catalog.FromEnv(ctx, *pros)
	if err != nil {
		return err
	}
	defer pub.Close()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	w := &worker.Worker{
		DB:      pub.DB,
		Store:   pub.Store,
		Process: worker.Subprocess(exe, "process", "-radar", *radar),
		Publish: func(ctx context.Context, dir string, out io.Writer) error {
			// Read each time, so a pro added to the catalogue is matched in
			// the next upload without restarting the worker.
			p, err := catalog.LoadPros(*pros)
			if err != nil {
				return err
			}
			pub.Pros, pub.Log = p, out
			_, err = pub.Put(ctx, []string{dir})
			return err
		},
		Log: log,
		Config: worker.Config{
			Name: *name, Version: buildVersion(), Scratch: *scratch,
			Poll: *poll, Timeout: *timeout, MaxAttempts: 3, Once: *once,
		},
	}
	return w.Run(ctx)
}

// process builds one demo, as the child of a running worker. It ends a
// failure with "error: <why>", which the worker shows the uploader, and exits
// worker.ExitUnreadable when the demo itself is the problem.
func process(args []string) (code int) {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	radar := fs.String("radar", "", "radar overview images and .txt files")
	out := fs.String("out", "", "where the demo's folder is written")
	name := fs.String("name", "", "the demo's name on the site")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || *radar == "" || *out == "" || *name == "" {
		fmt.Println("error: usage: demo-worker process -radar <dir> -out <dir> -name <name> <demo>")
		return 2
	}
	// The parser's own panics on the parsing goroutine are recovered here;
	// one on another goroutine ends the process, which the worker reports.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v\n%s\nerror: the demo could not be read: the parser crashed (%v)\n", r, debug.Stack(), r)
			code = worker.ExitUnreadable
		}
	}()
	r, err := export.Write(fs.Arg(0), *out, *name, export.Options{Radar: *radar, Progress: os.Stdout})
	if err != nil {
		fmt.Printf("error: the demo could not be read: %v\n", err)
		return worker.ExitUnreadable
	}
	fmt.Println(r)
	return 0
}

// buildVersion is the commit this binary was built from, with "-dirty" if it
// had uncommitted changes - so a job's log says exactly what code ran.
func buildVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// tee writes to a file and, when there is one, the console. A worker started
// at login has no console, and a failed write there must not cost the file
// its line - which io.MultiWriter would.
type tee struct {
	file    io.Writer
	console io.Writer
}

func (t tee) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	t.console.Write(p)
	if err != nil {
		return n, errors.Join(errors.New("log file"), err)
	}
	return n, nil
}
