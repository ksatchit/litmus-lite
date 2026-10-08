package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ksatchit/litmus-lite/internal/compose"
	"github.com/ksatchit/litmus-lite/internal/faults"
	"github.com/ksatchit/litmus-lite/internal/obs"
	"github.com/ksatchit/litmus-lite/internal/probe"
	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type Options struct {
	LoadBin     string // vegaload path; empty = look up PATH
	ScenarioDir string // directory of the *.chaos.yaml, for relative load: paths
}

func Run(ctx context.Context, sc *scenario.Scenario, opt Options) (*report.Result, error) {
	res := &report.Result{Name: sc.Metadata.Name, StartedAt: time.Now(), Passed: true}
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

	runProbe := func(p scenario.Probe) {
		s := probe.Once(ctx, p)
		mu.Lock()
		samples = append(samples, s)
		mu.Unlock()
	}

	for _, p := range sc.SteadyState {
		runProbe(p)
		mu.Lock()
		ok := len(samples) > 0 && samples[len(samples)-1].OK
		mu.Unlock()
		if !ok {
			return res, fmt.Errorf("steady-state probe %q failed", p.Name)
		}
	}

	var running []faults.Running
	stopped := make([]bool, 0)
	var events []report.FaultEvent
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
		}
		if res.FaultLog == nil {
			res.FaultLog = events
		}
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

	startProbes := func() error {
		type job struct {
			p        scenario.Probe
			interval time.Duration
		}
		var jobs []job
		for _, p := range sc.Probes {
			if p.Mode != "continuous" && p.Mode != "on-chaos" && p.Mode != "" {
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
				t := time.NewTicker(j.interval)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-stopProbes:
						return
					case <-t.C:
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

	steadyStart := time.Now()
	select {
	case <-ctx.Done():
		return res, ctx.Err()
	case <-time.After(baseline):
	}
	injectStart := time.Now()
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
	}
	for i, r := range running {
		if a, ok := r.(faults.Armable); ok {
			a.Arm()
		}
		if events[i].Start.IsZero() {
			events[i].Start = time.Now()
		}
		events[i].Intensity = r.Intensity()
	}

	var loadWG sync.WaitGroup
	if sc.Load != nil && sc.Load.Scenario != "" {
		loadWG.Add(1)
		go func() {
			defer loadWG.Done()
			raw, warn, err := compose.Run(ctx, opt.LoadBin, opt.ScenarioDir, sc.Load)
			if warn != "" {
				mu.Lock()
				res.Warnings = append(res.Warnings, warn)
				mu.Unlock()
			}
			if err != nil {
				mu.Lock()
				res.Warnings = append(res.Warnings, "vegaload: "+err.Error())
				mu.Unlock()
				return
			}
			if raw != nil {
				mu.Lock()
				res.Load = raw
				mu.Unlock()
			}
		}()
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
	for i, r := range running {
		if a, ok := r.(faults.Armable); ok {
			a.Disarm()
			events[i].End = injectEnd
			events[i].Rollback = "ok"
			continue
		}
		if err := r.Stop(); err != nil {
			events[i].Rollback = "fail: " + err.Error()
		} else {
			events[i].Rollback = "ok"
		}
		stopped[i] = true
		events[i].End = time.Now()
	}

	select {
	case <-ctx.Done():
	case <-time.After(recoverFor):
	}
	recoverEnd := time.Now()
	haltProbes()
	loadWG.Wait()

	for _, p := range append(sc.SteadyState, sc.Probes...) {
		if p.Mode == "EOT" {
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
	res.Recovery = report.RecoveryTime(res.Probes, injectEnd)
	res.TimeSeries = report.BucketSeries(res.StartedAt, res.Probes, events, recoverEnd)
	if len(res.Load) > 0 {
		shift := injectStart.Sub(res.StartedAt).Seconds()
		res.TimeSeries = compose.MergeSeries(res.TimeSeries, res.Load, shift)
	}
	res.ObservedVsInjected = report.ObservedVsInjected(events, phInject)

	for _, h := range sc.Hypotheses {
		v := report.HypothesisEval(h, phInject, res.Recovery)
		res.Hypotheses = append(res.Hypotheses, v)
		if !v.Passed {
			res.Passed = false
		}
	}

	if sc.Observability != nil && sc.Observability.Prometheus != nil && sc.Observability.Prometheus.URL != "" {
		pts, err := obs.QueryRange(ctx, sc.Observability.Prometheus.URL, res.StartedAt, recoverEnd)
		if err != nil {
			res.Warnings = append(res.Warnings, "prometheus: "+err.Error())
		} else {
			res.Prometheus = pts
		}
	}
	return res, nil
}
