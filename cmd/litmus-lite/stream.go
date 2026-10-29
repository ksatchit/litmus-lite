package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/stream"
)

// labelFlag collects repeated -label key=value flags.
type labelFlag map[string]string

func (l labelFlag) String() string { return fmt.Sprint(map[string]string(l)) }

func (l labelFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("label %q: want key=value", v)
	}
	l[strings.TrimSpace(k)] = val
	return nil
}

func cmdStream(args []string) int {
	if len(args) < 1 {
		streamUsage()
		return 2
	}
	switch args[0] {
	case "from-report":
		return cmdStreamFromReport(args[1:])
	case "replay":
		return cmdStreamReplay(args[1:])
	case "validate":
		return cmdStreamValidate(args[1:])
	case "receive":
		return cmdStreamReceive(args[1:])
	default:
		streamUsage()
		return 2
	}
}

func streamUsage() {
	fmt.Fprint(os.Stderr, `stream — run event streams (see docs/reference/streaming.md)

  stream from-report FILE.json   write a report as an NDJSON event stream (-out, -run-id, -label, -samples)
  stream replay FILE.ndjson...   serve recordings over HTTP/SSE (-stream-pull, -speed, -cors-origin), or push them (-stream-push, -fresh, -run-id)
  stream validate FILE.ndjson... check the structure of recordings
  stream receive -dir DIR        reference push receiver for -stream-push (-listen, -print, -delay, -fail-percent)

`)
}

func cmdStreamFromReport(args []string) int {
	fs := flag.NewFlagSet("stream from-report", flag.ContinueOnError)
	out := fs.String("out", "", "write NDJSON here (default: stdout)")
	runID := fs.String("run-id", "", "run id (default: derived from the report)")
	samples := fs.Bool("samples", false, "also emit probe.sample events and keep raw samples on run.finished")
	labels := labelFlag{}
	fs.Var(labels, "label", "label key=value on every event (repeatable)")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "stream from-report FILE.json")
		return 2
	}
	res, err := report.LoadJSON(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(labels) == 0 {
		labels = nil
	}
	events := stream.FromReport(res, stream.FromReportOptions{
		RunID: *runID, Labels: labels, Samples: *samples, Version: version,
	})
	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		w = f
	}
	if err := stream.Write(w, events); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdStreamValidate(args []string) int {
	fs := flag.NewFlagSet("stream validate", flag.ContinueOnError)
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "stream validate FILE.ndjson...")
		return 2
	}
	bad := 0
	for _, path := range fs.Args() {
		rec, err := stream.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			bad++
			continue
		}
		s := stream.Summary(rec)
		fmt.Printf("%s ok runId=%s events=%d status=%s\n", path, s.RunID, s.Events, s.Status)
	}
	if bad > 0 {
		return 1
	}
	return 0
}

func cmdStreamReplay(args []string) int {
	fs := flag.NewFlagSet("stream replay", flag.ContinueOnError)
	addr := fs.String("stream-pull", "127.0.0.1:7070", "address to serve on")
	speed := fs.Float64("speed", 1, "playback speed; 0 sends everything at once")
	maxGap := fs.Duration("max-gap", 0, "cap the pause between two events (0 = no cap)")
	cors := fs.String("cors-origin", "", "Access-Control-Allow-Origin value (e.g. http://localhost:5173)")
	var push pushFlag
	fs.Var(&push, "stream-push", "push the recordings to this ingest URL at their recorded pace instead of serving them (repeatable)")
	insecure := fs.Bool("stream-insecure", false, "allow http:// push to a non-loopback host")
	runID := fs.String("run-id", "", "with -stream-push: push under this run id (one recording only)")
	fresh := fs.Bool("fresh", false, "with -stream-push: give each recording a new run id, so it can be pushed again")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "stream replay FILE.ndjson...")
		return 2
	}
	pullSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "stream-pull" {
			pullSet = true
		}
	})
	pushing := len(push) > 0
	serving := !pushing || pullSet
	token := os.Getenv("LITMUS_LITE_STREAM_TOKEN")
	if serving {
		if err := stream.CheckBind(*addr, token); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if !pushing && (*runID != "" || *fresh) {
		fmt.Fprintln(os.Stderr, "-run-id and -fresh apply to -stream-push")
		return 2
	}
	if *runID != "" && (*fresh || fs.NArg() != 1 || stream.ValidRunID(*runID) != nil) {
		fmt.Fprintln(os.Stderr, "-run-id needs one recording, a valid id (A-Z a-z 0-9 . _ -, up to 64) and no -fresh")
		return 2
	}
	var sinks []*stream.PushSink
	for _, raw := range push {
		u, err := stream.ValidatePushURL(raw, *insecure)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		sinks = append(sinks, stream.NewPushSink(stream.PushOptions{URL: u, Token: token, Version: version}))
	}
	var runs []*stream.Recording
	seen := map[string]string{}
	for _, path := range fs.Args() {
		rec, err := stream.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if prev, dup := seen[rec.RunID()]; dup {
			fmt.Fprintf(os.Stderr, "%s and %s have the same runId %s\n", prev, path, rec.RunID())
			return 1
		}
		seen[rec.RunID()] = path
		runs = append(runs, rec)
	}
	if pushing {
		code := replayPush(runs, sinks, stream.ReplayOptions{Speed: *speed, MaxGap: *maxGap, RunID: *runID, Fresh: *fresh})
		if code != 0 || !serving {
			return code
		}
	}
	srv := &stream.Server{
		Runs: runs, Speed: *speed, MaxGap: *maxGap, CORSOrigin: *cors, Token: token, Version: version,
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(os.Stderr, "serving %d run(s) on http://%s (speed %g). Ctrl-C to stop.\n", len(runs), ln.Addr(), *speed)
	for _, rec := range runs {
		s := stream.Summary(rec)
		fmt.Fprintf(os.Stderr, "  %s  %s  %s  %d events\n", s.RunID, s.Name, s.Status, s.Events)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- httpSrv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
		_ = httpSrv.Close()
		return 0
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
}

func cmdStreamReceive(args []string) int {
	fs := flag.NewFlagSet("stream receive", flag.ContinueOnError)
	dir := fs.String("dir", ".litmus-lite/runs", "directory for one NDJSON file per run")
	listen := fs.String("listen", "127.0.0.1:9000", "address to listen on")
	print := fs.Bool("print", false, "echo a line per stored event to stderr")
	delay := fs.Duration("delay", 0, "wait this long before answering a batch (to test a sender)")
	failPct := fs.Int("fail-percent", 0, "answer 503 for this percentage of batches (to test a sender)")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if *failPct < 0 || *failPct > 100 {
		fmt.Fprintln(os.Stderr, "-fail-percent must be between 0 and 100")
		return 2
	}
	token := os.Getenv("LITMUS_LITE_STREAM_TOKEN")
	if err := stream.CheckBind(*listen, token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rcv, err := stream.NewReceiver(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rcv.Token, rcv.Delay, rcv.FailPercent = token, *delay, *failPct
	if *print {
		rcv.Print = func(line string) { fmt.Fprintln(os.Stderr, line) }
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srv := &http.Server{Handler: rcv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	auth := "no token"
	if token != "" {
		auth = "token required"
	}
	fmt.Fprintf(os.Stderr, "receiving on http://%s/v1/ingest (%s), writing runs to %s. Ctrl-C to stop.\n", ln.Addr(), auth, *dir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return 0
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
}

// replayPush sends recordings to the push sinks and reports what arrived.
func replayPush(runs []*stream.Recording, sinks []*stream.PushSink, opt stream.ReplayOptions) int {
	all := make([]stream.Sink, len(sinks))
	for i, s := range sinks {
		all[i] = s
	}
	p := stream.NewPusher(all...)
	events := 0
	for _, r := range runs {
		events += len(r.Events)
	}
	var dests []string
	for _, s := range sinks {
		dests = append(dests, s.Name())
	}
	fmt.Fprintf(os.Stderr, "pushing %d run(s), %d events, to %s (speed %g). Ctrl-C to stop.\n", len(runs), events, strings.Join(dests, ", "), opt.Speed)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ids, err := stream.ReplayPush(ctx, runs, p, opt)
	stuck := p.Close(10 * time.Second)
	for _, w := range p.Warnings() {
		fmt.Fprintln(os.Stderr, w)
	}
	code := 0
	for _, s := range sinks {
		d, l := s.Delivered()
		fmt.Fprintf(os.Stderr, "%s: %d events delivered, %d lost\n", s.Name(), d, l)
		if l > 0 {
			code = 1
		}
	}
	if len(stuck) > 0 {
		fmt.Fprintf(os.Stderr, "did not finish: %s\n", strings.Join(stuck, ", "))
		code = 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "interrupted")
		return 130
	}
	if code == 0 {
		fmt.Fprintf(os.Stderr, "pushed run id(s): %s\n", strings.Join(ids, ", "))
	}
	return code
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := fs.String("dir", ".litmus-lite/runs", "directory of NDJSON run files (-stream-file auto, stream receive)")
	listen := fs.String("listen", "127.0.0.1:7070", "address to listen on")
	cors := fs.String("cors-origin", "", "Access-Control-Allow-Origin value (e.g. http://localhost:5173)")
	stale := fs.Duration("stale", 2*time.Minute, "report a run with no end that has not been written to for this long as incomplete")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "serve takes no file arguments; use -dir")
		return 2
	}
	token := os.Getenv("LITMUS_LITE_STREAM_TOKEN")
	ln, err := stream.ListenDir(*listen, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if strings.Contains(err.Error(), "not loopback") {
			return 2
		}
		return 1
	}
	srv := &stream.DirServer{
		Dir: *dir, Token: token, CORSOrigin: *cors, Version: version, Stale: *stale,
		Warn: func(m string) { fmt.Fprintln(os.Stderr, m) },
	}
	if _, err := os.Stat(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "note: %s does not exist yet; it will be read when it appears\n", *dir)
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	auth := "no token"
	if token != "" {
		auth = "token required"
	}
	fmt.Fprintf(os.Stderr, "serving %s on http://%s (%s). Read-only; it never starts a run. Ctrl-C to stop.\n", *dir, ln.Addr(), auth)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- httpSrv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
		_ = httpSrv.Close()
		return 0
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
}
