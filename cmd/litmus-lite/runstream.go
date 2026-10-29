package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/live"
	"github.com/litmuschaos/litmus-lite/internal/stream"
)

// pushFlag collects repeated -stream-push URLs.
type pushFlag []string

func (p *pushFlag) String() string { return strings.Join(*p, ",") }
func (p *pushFlag) Set(v string) error {
	*p = append(*p, v)
	return nil
}

// runStream holds the streaming flags of `run` (docs/reference/streaming.md). All of
// them are optional: with none set, `run` behaves as it always has.
type runStream struct {
	file       string
	push       pushFlag
	pull       string
	linger     time.Duration
	insecure   bool
	corsOrigin string
	scenario   bool
	runID      string
	samples    bool
	labels     labelFlag

	// The live view, dormant until a destination is configured (see internal/live).
	liveOn  bool
	noLive  bool
	noOpen  bool
	liveURL string
}

func (s *runStream) register(fs *flag.FlagSet) {
	s.labels = labelFlag{}
	fs.StringVar(&s.file, "stream-file", "", "write the run as NDJSON events to PATH, or `auto` for .litmus-lite/runs/<run id>.ndjson")
	fs.Var(&s.push, "stream-push", "POST events to this ingest URL (repeatable; https unless loopback)")
	fs.StringVar(&s.pull, "stream-pull", "", "serve the run as SSE on ADDR (for example 127.0.0.1:7070) while it runs")
	fs.DurationVar(&s.linger, "stream-pull-linger", 15*time.Second, "keep serving -stream-pull this long after the run")
	fs.BoolVar(&s.insecure, "stream-insecure", false, "allow -stream-push over http:// to a non-loopback host")
	fs.StringVar(&s.corsOrigin, "stream-cors-origin", "", "Access-Control-Allow-Origin for -stream-pull (browser clients)")
	fs.BoolVar(&s.scenario, "stream-scenario", false, "include the scenario, credentials masked, in run.started")
	fs.StringVar(&s.runID, "run-id", "", "run id used on stream events (default: random)")
	fs.BoolVar(&s.samples, "stream-samples", false, "also stream every probe sample (high volume)")
	fs.Var(s.labels, "label", "label key=value on every stream event (repeatable)")
	fs.BoolVar(&s.liveOn, "live", false, "also show the run in the live view when another -stream destination is used (needs a live-view destination)")
	fs.BoolVar(&s.noLive, "no-live", false, "do not stream to the configured live view (also LITMUS_LITE_LIVE=off)")
	fs.BoolVar(&s.noOpen, "no-open", false, "stream to the live view but do not open the browser (also LITMUS_LITE_OPEN=off; never opened in CI)")
	fs.StringVar(&s.liveURL, "live-url", "", "live-view service to stream this run to, and the switch that turns the live view on (also LITMUS_LITE_LIVE_URL, or `litmus-lite live enable URL`)")
}

// streaming is what setup produced for one run.
type streaming struct {
	rec      *stream.Recorder
	live     *stream.LiveServer
	linger   time.Duration
	warnings []string // problems known before the run starts
	audit    string   // what the audit log may say: kinds and push hosts, nothing else
}

// setup returns the streaming state for this run, or nil when no sink is
// configured. A flag wins over its environment variable. A destination that
// breaks a rule (http:// to a remote host, an open bind without a token, a
// bad run id) is refused, so the run does not start. A sink that cannot be set
// up for another reason, such as a file that cannot be written or a port that
// is taken, is a warning and the run goes on without it.
func (s *runStream) setup() (*streaming, error) {
	file := s.file
	if file == "" {
		file = os.Getenv("LITMUS_LITE_STREAM_FILE")
	}
	pushes := []string(s.push)
	if len(pushes) == 0 {
		for _, u := range strings.Split(os.Getenv("LITMUS_LITE_STREAM_PUSH"), ",") {
			if u = strings.TrimSpace(u); u != "" {
				pushes = append(pushes, u)
			}
		}
	}
	pull := s.pull
	if pull == "" {
		pull = os.Getenv("LITMUS_LITE_STREAM_PULL")
	}
	runID := s.runID
	if runID == "" {
		runID = os.Getenv("LITMUS_LITE_RUN_ID")
	}
	if runID != "" {
		if err := stream.ValidRunID(runID); err != nil {
			return nil, err
		}
	} else {
		runID = stream.NewRunID()
	}
	explicit := file != "" || len(pushes) > 0 || pull != ""
	// A configured live view is the destination when the user asked for none
	// of their own. -live adds it to the ones they asked for. With nothing
	// configured there is no live view: nothing is sent, opened or written.
	var lv *liveView
	if live.Enabled(s.noLive) && (!explicit || s.liveOn) {
		base, err := live.Destination(s.liveURL)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "live view: off (%v)\n", err)
		case base == nil && s.liveOn:
			return nil, errNoLiveDestination
		case base != nil:
			lv = s.resolveLive(base)
		}
	}
	if !explicit && lv == nil {
		return nil, nil
	}
	token := os.Getenv("LITMUS_LITE_STREAM_TOKEN")

	// Rules first, so a refused destination never starts a run.
	var urls []*url.URL
	for _, raw := range pushes {
		u, err := stream.ValidatePushURL(raw, s.insecure)
		if err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	if pull != "" {
		if err := stream.CheckBind(pull, token); err != nil {
			return nil, err
		}
	}

	st := &streaming{linger: s.linger}
	var sinks []stream.Sink
	var kinds []string
	var where []string
	if file != "" {
		path := file
		if file == "auto" {
			path = filepath.Join(".litmus-lite", "runs", runID+".ndjson")
		}
		fsink, err := stream.NewFileSink(path)
		if err != nil {
			st.warnings = append(st.warnings, "stream: "+err.Error()+"; the run goes on without it")
		} else {
			sinks = append(sinks, fsink)
			kinds = append(kinds, "file")
			where = append(where, "file "+path)
		}
	}
	for _, u := range urls {
		sinks = append(sinks, stream.NewPushSink(stream.PushOptions{URL: u, Token: token, Version: version}))
		kinds = append(kinds, "push:"+u.Host)
		where = append(where, "push "+u.Scheme+"://"+u.Host)
	}
	if pull != "" {
		srv, err := stream.NewLiveServer(pull, token, s.corsOrigin, version)
		if err != nil {
			st.warnings = append(st.warnings, "stream: "+err.Error()+"; the run goes on without it")
		} else {
			st.live = srv
			sinks = append(sinks, srv)
			kinds = append(kinds, "pull")
			where = append(where, "pull http://"+srv.Addr)
		}
	}
	if lv != nil {
		// Its own token: the project's push token, not LITMUS_LITE_STREAM_TOKEN.
		sinks = append(sinks, stream.NewPushSink(stream.PushOptions{URL: lv.push, Token: lv.token, Version: version}))
		kinds = append(kinds, "live:"+lv.push.Host)
	}

	var labels map[string]string
	if len(s.labels) > 0 {
		labels = map[string]string(s.labels)
	}
	st.rec = stream.NewRecorder(stream.RecorderOptions{
		RunID: runID, Labels: labels, Samples: s.samples, Version: version, IncludeScenario: s.scenario,
	}, sinks...)
	st.audit = strings.Join(kinds, ",")
	if len(where) > 0 {
		fmt.Fprintf(os.Stderr, "streaming run %s: %s\n", runID, strings.Join(where, ", "))
	}
	if lv != nil {
		fmt.Fprintf(os.Stderr, "live view: %s   (not wanted? -no-live, or LITMUS_LITE_LIVE=off)\n", lv.page)
		if live.OpenEnabled(s.noOpen) {
			if err := live.Opener(lv.page); err != nil {
				fmt.Fprintf(os.Stderr, "live view: could not open the browser (%v); open the URL above\n", err)
			}
		}
	}
	return st, nil
}

// errNoLiveDestination is what -live says when nothing says where the live
// view is: it would otherwise do nothing and look like it worked.
var errNoLiveDestination = errors.New("-live needs a live-view service: pass -live-url URL, set LITMUS_LITE_LIVE_URL, or run `litmus-lite live enable URL`")

// liveView is the resolved live-view destination of one run.
type liveView struct {
	push  *url.URL
	token string
	page  string
}

// resolveLive prepares the live view at base, or says on stderr why there is
// none. It never fails the run: a service that is down, or an identity file
// that cannot be kept, just means a run without the view.
func (s *runStream) resolveLive(base *url.URL) *liveView {
	id, err := live.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "live view: off (%v)\n", err)
		return nil
	}
	push, err := stream.ValidatePushURL(id.PushURL(base), false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "live view: off (%v)\n", err)
		return nil
	}
	if err := live.Probe(context.Background(), base); err != nil {
		fmt.Fprintf(os.Stderr, "live view: off, %s://%s %v. The run goes on without it (-no-live to stop this message)\n", base.Scheme, base.Host, err)
		return nil
	}
	if id.Created {
		fmt.Fprintf(os.Stderr, "live view: made a project for this machine (%s), kept in %s\n", id.ProjectID, id.Path)
	}
	return &liveView{push: push, token: id.PushToken, page: id.PageURL(base)}
}

// close flushes the sinks, bounded, and reports what went wrong with them. It
// runs after the reports are written, so a stuck sink cannot delay them or
// change the exit code. A pull server then lingers so a late client can read
// the final event; ctx (cancelled by Ctrl-C) cuts that short.
func (st *streaming) close(ctx context.Context) {
	if st == nil {
		return
	}
	stuck := st.rec.Close(5 * time.Second)
	for _, w := range st.rec.Warnings() {
		fmt.Fprintln(os.Stderr, w)
	}
	for _, name := range stuck {
		fmt.Fprintf(os.Stderr, "stream: %s did not finish; some events may be missing\n", name)
	}
	if st.live != nil {
		st.live.Shutdown(ctx, st.linger)
	}
}
