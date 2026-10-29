package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/compose"
	"github.com/litmuschaos/litmus-lite/internal/faults"
	"github.com/litmuschaos/litmus-lite/internal/obs"
	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

type Options struct {
	LoadBin     string // optional executable override for load.tool
	ScenarioDir string // directory of the *.chaos.yaml, for relative load: paths
	// Emitter, when set, is told what happens during the run (run streaming).
	// The engine never waits on it and its behaviour does not change the result.
	Emitter Emitter
	// RunID, when set, is exported to a load generator as LITMUS_LITE_RUN_ID.
	RunID string
	// Warnings are problems found before the run started (a key nothing
	// reads). They are put at the head of the result's warnings, and sent to
	// the Emitter, so that the report and the stream's run.finished agree.
	Warnings []string
}

// runState lets Run see which faults were armed when the run ends early.
type runState struct {
	started func() []report.FaultEvent
}

// Run executes the scenario. When Options.Emitter is set, the emitter sees
// Started first and then exactly one of Finished or Failed, after faults have
// been rolled back.
func Run(ctx context.Context, sc *scenario.Scenario, opt Options) (*report.Result, error) {
	em := opt.Emitter
	if em == nil {
		em = nopEmitter{}
	}
	var st runState
	res, err := run(ctx, sc, opt, em, &st)
	var started []report.FaultEvent
	if st.started != nil {
		started = st.started()
	}
	switch {
	case ctx.Err() != nil:
		// A cancelled run still goes on to a report, but it did not complete.
		reason := "interrupted"
		if !errors.Is(ctx.Err(), context.Canceled) {
			reason = ctx.Err().Error()
		}
		var partial *report.Result
		if err == nil && res != nil && len(res.Phases) > 0 {
			partial = res
		}
		em.Failed(reason, partial, started)
	case err != nil:
		em.Failed(err.Error(), nil, started)
	default:
		em.Finished(res)
	}
	return res, err
}

func run(ctx context.Context, sc *scenario.Scenario, opt Options, em Emitter, st *runState) (*report.Result, error) {
	res := &report.Result{SchemaVersion: report.SchemaVersion, Name: sc.Metadata.Name, StartedAt: time.Now(), Passed: true}
	em.Started(sc, res.StartedAt)
	for _, w := range opt.Warnings {
		res.Warnings = append(res.Warnings, w)
		em.Warning(w)
	}
	baseline, err := time.ParseDuration(sc.Baseline)
	if err != nil {
		return res, fmt.Errorf("baseline: %w", err)
	}
	recoverFor, err := time.ParseDuration(sc.Recover)
	if err != nil {
		return res, fmt.Errorf("recover: %w", err)
	}

	var mu sync.Mutex
	var samples []probe.Sample
	stopProbes := make(chan struct{})
	var wg sync.WaitGroup
	var probeOnce sync.Once
	probesOn := false
	haltProbes := func() {
		if !probesOn {
			return
		}
		probeOnce.Do(func() { close(stopProbes) })
		wg.Wait()
	}

	var live *livePoints
	if opt.Emitter != nil {
		live = newLivePoints(em, res.StartedAt, func() []probe.Sample {
			mu.Lock()
			defer mu.Unlock()
			return samples[:len(samples):len(samples)]
		})
		live.begin()
		defer live.end()
	}

	runProbe := func(p scenario.Probe) {
		id := live.probeStarted()
		s := probe.Once(ctx, p)
		mu.Lock()
		samples = append(samples, s)
		mu.Unlock()
		live.probeDone(id)
		em.Sample(s)
	}

	for _, p := range sc.SteadyState {
		runProbe(p)
		mu.Lock()
		last := samples[len(samples)-1]
		mu.Unlock()
		if !last.OK {
			return res, startFailure(p, last)
		}
	}

	// SOT probes in probes: take one sample before anything is injected, as
	// steadyState probes do. A failing one means the system was not healthy to
	// begin with, so the run stops here and no fault is started.
	for _, p := range sc.Probes {
		if p.Mode != scenario.ModeSOT {
			continue
		}
		runProbe(p)
		mu.Lock()
		last := samples[len(samples)-1]
		mu.Unlock()
		if !last.OK {
			return res, startFailure(p, last)
		}
	}

	var running []faults.Running
	stopped := make([]bool, 0)
	var events []report.FaultEvent
	// announced[i] is set when fault.started was emitted for events[i], and
	// ended[i] when fault.ended was.
	announced := map[int]bool{}
	ended := map[int]bool{}
	announceStart := func(i int) {
		live.setFaults(events)
		announced[i] = true
		em.FaultStarted(events[i])
	}
	announceEnd := func(i int) {
		live.setFaults(events)
		if announced[i] && !ended[i] {
			ended[i] = true
			em.FaultEnded(events[i])
		}
	}
	st.started = func() []report.FaultEvent {
		var out []report.FaultEvent
		for i := range events {
			if announced[i] {
				out = append(out, events[i])
			}
		}
		return out
	}

	var loadWG sync.WaitGroup
	loadCtx, cancelLoad := context.WithCancel(ctx)
	defer func() {
		haltProbes()
		for i, r := range running {
			if i < len(stopped) && stopped[i] {
				continue
			}
			stopErr := r.Stop()
			if i < len(stopped) {
				stopped[i] = true
			}
			if i >= len(events) {
				continue
			}
			if stopErr != nil {
				events[i].Rollback = "fail: " + stopErr.Error()
			} else if events[i].Rollback == "" {
				events[i].Rollback = "ok"
			}
		}
		for i := range events {
			if events[i].Rollback == "" {
				events[i].Rollback = "ok"
			}
			if announced[i] && !ended[i] {
				// The run is ending early: this fault is rolled back now.
				if events[i].End.IsZero() {
					events[i].End = time.Now()
				}
				announceEnd(i)
			}
		}
		if res.FaultLog == nil {
			res.FaultLog = events
		}
		// A load generator still running (it can start with the baseline) must
		// not outlive an early exit.
		cancelLoad()
		loadWG.Wait()
	}()

	type pendingFault struct {
		f   scenario.Fault
		inj faults.Injector
	}
	var pending []pendingFault
	for _, f := range sc.Faults {
		inj, err := faults.Lookup(f.Kind)
		if err != nil {
			return res, err
		}
		if err := inj.Validate(f); err != nil {
			return res, err
		}
		early := false
		if e, ok := inj.(faults.EarlyStarter); ok {
			early = e.EarlyStart()
		}
		if !early {
			pending = append(pending, pendingFault{f, inj})
			continue
		}
		r, err := inj.Start(ctx, f)
		if err != nil {
			return res, err
		}
		if a, ok := r.(faults.Armable); ok {
			a.Disarm()
		}
		running = append(running, r)
		stopped = append(stopped, false)
		events = append(events, report.FaultEvent{
			Name: f.Name, Kind: f.Kind, Tunables: f.Params, Intensity: r.Intensity(),
		})
	}

	// chaosOn is closed once every fault is armed and chaosOff when the inject
	// window ends. on-chaos probes sample between the two.
	chaosOn := make(chan struct{})
	chaosOff := make(chan struct{})

	startProbes := func() error {
		type job struct {
			p        scenario.Probe
			interval time.Duration
		}
		var jobs []job
		for _, p := range sc.Probes {
			if p.Mode != scenario.ModeContinuous && p.Mode != scenario.ModeOnChaos && p.Mode != "" {
				continue
			}
			var interval time.Duration
			if p.Interval == "" {
				interval = 200 * time.Millisecond
			} else {
				d, err := time.ParseDuration(p.Interval)
				if err != nil {
					return fmt.Errorf("probe %s interval: %w", p.Name, err)
				}
				if d <= 0 {
					interval = 200 * time.Millisecond
				} else {
					interval = d
				}
			}
			jobs = append(jobs, job{p: p, interval: interval})
		}
		if len(jobs) == 0 {
			return nil
		}
		probesOn = true
		for _, j := range jobs {
			j := j
			wg.Add(1)
			go func() {
				defer wg.Done()
				// off stays nil, which never fires, for a probe that runs
				// all the time.
				var off <-chan struct{}
				if j.p.Mode == scenario.ModeOnChaos {
					select {
					case <-ctx.Done():
						return
					case <-stopProbes:
						return
					case <-chaosOn:
					}
					off = chaosOff
					// The window can be shorter than the interval: sample at
					// once so that it is never empty.
					runProbe(j.p)
				}
				t := time.NewTicker(j.interval)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-stopProbes:
						return
					case <-off:
						return
					case <-t.C:
						// A tick and the end of the window can be ready
						// together, and select then picks either: never
						// sample after the window has closed.
						select {
						case <-off:
							return
						default:
						}
						runProbe(j.p)
					}
				}
			}()
		}
		return nil
	}
	if err := startProbes(); err != nil {
		return res, err
	}

	// The generator starts with the fault (the default) or with the probes
	// (load.start: baseline). Its series is aligned to the moment it started.
	var loadSeries []compose.LoadPoint
	var loadStart time.Time
	startLoad := func() {
		if !compose.Active(sc.Load) {
			return
		}
		loadStart = time.Now()
		startOffset := loadStart.Sub(res.StartedAt).Seconds()
		tool := sc.Load.Tool
		if tool == "" {
			tool = "vegaload"
		}
		if opt.Emitter != nil {
			em.LoadStarted(tool, startOffset)
		}
		ropt := compose.RunOptions{RunID: opt.RunID}
		if opt.Emitter != nil {
			ropt.Progress = func(p compose.LivePoint) {
				p.OffsetS += startOffset
				em.LoadPoint(p)
			}
		}
		loadWG.Add(1)
		go func() {
			defer loadWG.Done()
			out, err := compose.RunWith(loadCtx, opt.LoadBin, opt.ScenarioDir, sc.Load, ropt)
			if opt.Emitter != nil {
				shifted := make([]compose.LoadPoint, 0, len(out.Series))
				for _, lp := range out.Series {
					lp.OffsetS += startOffset
					shifted = append(shifted, lp)
				}
				msg := ""
				if err != nil {
					msg = err.Error()
				}
				em.LoadFinished(shifted, msg)
			}
			label := out.Tool
			if label == "" {
				label = "load"
			}
			mu.Lock()
			defer mu.Unlock()
			warn := func(msg string) {
				res.Warnings = append(res.Warnings, msg)
				em.Warning(msg)
			}
			if out.Warn != "" {
				warn(out.Warn)
			}
			if len(out.Raw) > 0 {
				res.Load = out.Raw
			}
			if out.Generator != nil {
				res.LoadGenerator = out.Generator
			}
			if len(out.Series) > 0 {
				loadSeries = out.Series
			}
			if err != nil {
				warn(label + ": " + err.Error())
			}
		}()
	}
	loadAtBaseline := sc.Load != nil && sc.Load.Start == scenario.LoadStartBaseline

	steadyStart := time.Now()
	em.Phase("steady")
	if loadAtBaseline {
		startLoad()
	}
	select {
	case <-ctx.Done():
		return res, ctx.Err()
	case <-time.After(baseline):
	}
	injectStart := time.Now()
	em.Phase("inject")
	for _, p := range pending {
		r, err := p.inj.Start(ctx, p.f)
		if err != nil {
			return res, err
		}
		running = append(running, r)
		stopped = append(stopped, false)
		events = append(events, report.FaultEvent{
			Name: p.f.Name, Kind: p.f.Kind, Start: time.Now(), Tunables: p.f.Params, Intensity: r.Intensity(),
		})
		announceStart(len(events) - 1)
	}
	for i, r := range running {
		if a, ok := r.(faults.Armable); ok {
			a.Arm()
		}
		if events[i].Start.IsZero() {
			events[i].Start = time.Now()
		}
		events[i].Intensity = r.Intensity()
		if !announced[i] {
			announceStart(i)
		} else {
			live.setFaults(events)
		}
	}

	close(chaosOn)

	if !loadAtBaseline {
		startLoad()
	}

	maxDur := time.Duration(0)
	for _, f := range sc.Faults {
		if d := f.DurationTime(); d > maxDur {
			maxDur = d
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(maxDur):
	}

	injectEnd := time.Now()
	close(chaosOff)
	em.Phase("recover")
	for i, r := range running {
		// A fault that sits in the path of requests and saw none changed
		// nothing: say so, because every hypothesis was then judged on a
		// service that was not disturbed.
		if c, ok := r.(faults.TrafficCounter); ok && c.Requests() == 0 {
			msg := noTrafficWarning(events[i])
			mu.Lock()
			res.Warnings = append(res.Warnings, msg)
			mu.Unlock()
			em.Warning(msg)
		}
		if a, ok := r.(faults.Armable); ok {
			a.Disarm()
			events[i].End = injectEnd
			events[i].Rollback = "ok"
			announceEnd(i)
			continue
		}
		if err := r.Stop(); err != nil {
			events[i].Rollback = "fail: " + err.Error()
		} else {
			events[i].Rollback = "ok"
		}
		stopped[i] = true
		events[i].End = time.Now()
		announceEnd(i)
	}

	select {
	case <-ctx.Done():
	case <-time.After(recoverFor):
	}
	recoverEnd := time.Now()
	emittedPoints := live.end()
	haltProbes()
	loadWG.Wait()

	for _, p := range append(append([]scenario.Probe(nil), sc.SteadyState...), sc.Probes...) {
		if p.Mode == scenario.ModeEOT {
			runProbe(p)
		}
	}

	mu.Lock()
	res.Probes = samples
	mu.Unlock()
	res.FaultLog = events
	res.Elapsed = time.Since(res.StartedAt)

	phSteady := report.Summarize("steady", steadyStart, injectStart, res.Probes)
	phInject := report.Summarize("inject", injectStart, injectEnd, res.Probes)
	phRec := report.Summarize("recover", injectEnd, recoverEnd, res.Probes)
	res.Phases = []report.Phase{phSteady, phInject, phRec}
	rec := report.Recovery(res.Probes, injectEnd)
	res.Recovery = rec.Time
	res.TimeSeries = report.BucketSeries(res.StartedAt, res.Probes, events, recoverEnd)
	// The buckets that were not closed when the ticker stopped. They are
	// emitted before the load series is merged, so every live point is
	// computed from probes alone.
	if opt.Emitter != nil {
		for i := emittedPoints; i < len(res.TimeSeries); i++ {
			em.Point(res.TimeSeries[i])
		}
	}
	mu.Lock()
	series := append([]compose.LoadPoint(nil), loadSeries...)
	mu.Unlock()
	if len(series) > 0 {
		shift := injectStart.Sub(res.StartedAt).Seconds()
		if loadAtBaseline {
			shift = loadStart.Sub(res.StartedAt).Seconds()
		}
		res.TimeSeries = compose.MergePoints(res.TimeSeries, series, shift)
	}
	res.ObservedVsInjected = report.ObservedVsInjected(events, phInject)

	for _, h := range sc.Hypotheses {
		v := report.HypothesisEval(h, phInject, rec)
		res.Hypotheses = append(res.Hypotheses, v)
		if !v.Passed {
			res.Passed = false
		}
	}

	if sc.Observability != nil && sc.Observability.Prometheus != nil && sc.Observability.Prometheus.URL != "" {
		pts, err := obs.QueryRange(ctx, sc.Observability.Prometheus.URL, res.StartedAt, recoverEnd)
		if err != nil {
			res.Warnings = append(res.Warnings, "prometheus: "+err.Error())
			em.Warning("prometheus: " + err.Error())
		} else {
			res.Prometheus = pts
		}
	}
	return res, nil
}

// clientErrPrefix is how net/http starts an error: `Get "URL": `. The URL is
// shown without its credentials and query by startFailure, so the one in the
// error text is dropped.
var clientErrPrefix = regexp.MustCompile(`^[A-Za-z]+ "[^"]*": `)

// maxFailureText is how much of a probe's error the failure message carries.
const maxFailureText = 240

// startFailure is the error of a probe that failed before anything was
// injected. It says where the probe looked and why it failed, because that is
// the first failure a new user meets: a service that is not running, or a
// probe that points at the wrong address. A command probe's command is not
// repeated (it can hold a secret); its own output is the reason.
func startFailure(p scenario.Probe, s probe.Sample) error {
	msg := fmt.Sprintf("steady-state probe %q failed", p.Name)
	switch strings.ToLower(p.Type) {
	case "cmd", "command":
	default:
		method := p.Method
		if method == "" {
			method = "GET"
		}
		msg += fmt.Sprintf(" (%s %s)", method, bareURL(p.URL))
	}
	if reason := firstLine(clientErrPrefix.ReplaceAllString(s.Error, "")); reason != "" {
		msg += ": " + reason
	}
	if refused(s.Error) {
		msg += ". Is the service running, and is the address right?"
	}
	return errors.New(msg)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > maxFailureText {
		s = string(r[:maxFailureText]) + "..."
	}
	return s
}

// bareURL is u without credentials or query, which can hold a token. The
// message ends up on stderr, in a stream event and in a CI log.
func bareURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return "(unreadable URL)"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// noTrafficWarning is the warning for a proxy fault that no request went
// through while it was armed.
func noTrafficWarning(e report.FaultEvent) string {
	listen, _ := e.Tunables["listen"].(string)
	where := "its listen address"
	if listen != "" {
		where = listen
	}
	return fmt.Sprintf("fault %q (%s) saw no requests while it was armed: nothing went through %s, so it changed nothing and the hypotheses were judged on an undisturbed service. "+
		"Point the probes, the load generator or your client at that address, not at the upstream", e.Name, e.Kind, where)
}

// refused reports whether a probe's error is a connection nothing accepted:
// "connection refused" on Linux and macOS, and on Windows "connectex: No
// connection could be made because the target machine actively refused it."
func refused(msg string) bool {
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused")
}
