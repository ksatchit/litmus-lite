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
	LoadBin string // vegaload path; empty = look up PATH
}

func Run(ctx context.Context, sc *scenario.Scenario, opt Options) (*report.Result, error) {
	res := &report.Result{Name: sc.Metadata.Name, StartedAt: time.Now(), Passed: true}
	baseline, _ := time.ParseDuration(sc.Baseline)
	recoverFor, _ := time.ParseDuration(sc.Recover)

	var mu sync.Mutex
	var samples []probe.Sample
	stopProbes := make(chan struct{})
	var wg sync.WaitGroup

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

	cont := append([]scenario.Probe{}, sc.Probes...)
	for _, p := range cont {
		if p.Mode != "continuous" && p.Mode != "on-chaos" && p.Mode != "" {
			continue
		}
		p := p
		interval, _ := time.ParseDuration(p.Interval)
		if interval <= 0 {
			interval = 200 * time.Millisecond
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopProbes:
					return
				case <-t.C:
					runProbe(p)
				}
			}
		}()
	}

	steadyStart := time.Now()
	select {
	case <-ctx.Done():
		close(stopProbes)
		wg.Wait()
		return res, ctx.Err()
	case <-time.After(baseline):
	}
	injectStart := time.Now()

	var running []faults.Running
	var events []report.FaultEvent
	defer func() {
		for i := range events {
			if events[i].Rollback == "" {
				events[i].Rollback = "ok"
			}
		}
		for _, r := range running {
			if err := r.Stop(); err != nil && len(events) > 0 {
				events[len(events)-1].Rollback = "fail: " + err.Error()
			}
		}
	}()

	for _, f := range sc.Faults {
		inj, err := faults.Lookup(f.Kind)
		if err != nil {
			return res, err
		}
		if err := inj.Validate(f); err != nil {
			return res, err
		}
		r, err := inj.Start(ctx, f)
		if err != nil {
			return res, err
		}
		running = append(running, r)
		events = append(events, report.FaultEvent{
			Name: f.Name, Kind: f.Kind, Start: time.Now(), Tunables: f.Params, Intensity: r.Intensity(),
		})
	}

	var loadWG sync.WaitGroup
	if sc.Load != nil && sc.Load.Scenario != "" {
		loadWG.Add(1)
		go func() {
			defer loadWG.Done()
			raw, warn, err := compose.Run(ctx, opt.LoadBin, sc.Load)
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

	for i := range running {
		if err := running[i].Stop(); err != nil {
			events[i].Rollback = "fail: " + err.Error()
		} else {
			events[i].Rollback = "ok"
		}
		events[i].End = time.Now()
	}
	running = nil
	injectEnd := time.Now()

	select {
	case <-ctx.Done():
	case <-time.After(recoverFor):
	}
	recoverEnd := time.Now()
	close(stopProbes)
	loadWG.Wait()
	wg.Wait()

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
