package stream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/diagnose"
	"github.com/litmuschaos/litmus-lite/internal/report"
)

// FromReportOptions controls FromReport.
type FromReportOptions struct {
	// RunID overrides the derived run id.
	RunID string
	// Labels are attached to every event.
	Labels map[string]string
	// Samples adds one probe.sample event per probe sample and keeps the raw
	// samples on run.finished.
	Samples bool
	// Version is written to run.started.
	Version string
}

// DeriveRunID returns a stable id for a finished report: the same report
// always gives the same id.
func DeriveRunID(res *report.Result) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", res.Name, res.StartedAt.UnixNano())))
	return hex.EncodeToString(h[:6])
}

type pending struct {
	ts   time.Time
	typ  string
	data any
}

// FromReport turns a finished report into the events a live run would have
// produced. It is for showing old runs and for building consumers.
//
// The report holds less than a live run knows, so the events are marked
// synthetic and run.started carries only what can be recovered: the target,
// the probe interval and the scenario text are not in a report. No load.*
// events are produced; the merged load series is in run.finished.timeseries.
func FromReport(res *report.Result, opt FromReportOptions) []Envelope {
	var ps []pending
	add := func(ts time.Time, typ string, data any) {
		ps = append(ps, pending{ts: ts, typ: typ, data: data})
	}

	started := RunStarted{
		Name:       res.Name,
		Version:    opt.Version,
		Faults:     []FaultSummary{},
		Hypotheses: []HypothesisSpec{},
	}
	for _, ph := range res.Phases {
		switch ph.Name {
		case "steady":
			started.Baseline = durationString(ph.To.Sub(ph.From))
		case "recover":
			started.Recover = durationString(ph.To.Sub(ph.From))
		}
	}
	for _, f := range res.FaultLog {
		started.Faults = append(started.Faults, FaultSummary{
			Name:     f.Name,
			Kind:     f.Kind,
			Duration: durationString(f.End.Sub(f.Start)),
			Tunables: Redact(f.Tunables),
		})
	}
	for _, h := range res.Hypotheses {
		started.Hypotheses = append(started.Hypotheses, HypothesisSpec{
			Name: h.Name, Metric: h.Metric, Operator: h.Operator, Limit: h.Limit,
		})
	}
	started.Probes = probeNames(res)
	if len(res.Load) > 0 {
		started.Load = &LoadInfo{}
	}
	add(res.StartedAt, TypeRunStarted, started)

	for _, ph := range res.Phases {
		add(ph.From, TypePhaseChanged, PhaseChanged{Phase: ph.Name})
	}
	for _, f := range res.FaultLog {
		add(f.Start, TypeFaultStarted, FaultStarted{
			Name: f.Name, Kind: f.Kind, Intensity: f.Intensity, Tunables: Redact(f.Tunables),
		})
		add(f.End, TypeFaultEnded, FaultEnded{Name: f.Name, Kind: f.Kind, Rollback: f.Rollback})
	}
	if opt.Samples {
		for _, s := range res.Probes {
			add(s.At, TypeProbeSample, ProbeSample{
				Name: s.Name, OK: s.OK, Status: s.Status, Error: s.Error,
				LatencyMs: float64(s.Latency) / float64(time.Millisecond),
			})
		}
	}
	for _, pt := range res.TimeSeries {
		// A point is complete when its one-second bucket closes.
		add(pt.T.Add(time.Second), TypePoint, Point{
			OffsetS:      pt.OffsetS,
			Phase:        pt.Phase,
			P95Ms:        pt.P95Ms,
			ErrorRate:    pt.ErrorRate,
			Availability: pt.Availability,
			RPS:          pt.RPS,
			Intensity:    pt.Intensity,
		})
	}

	end := res.StartedAt.Add(res.Elapsed)
	for _, p := range ps {
		if p.ts.After(end) {
			end = p.ts
		}
	}
	for _, w := range res.Warnings {
		add(end, TypeWarning, Warning{Message: w})
	}

	fin := BuildFinished(res, opt.Samples)
	add(end, TypeRunFinished, fin)

	sort.SliceStable(ps, func(i, j int) bool { return ps[i].ts.Before(ps[j].ts) })

	runID := opt.RunID
	if runID == "" {
		runID = DeriveRunID(res)
	}
	out := make([]Envelope, 0, len(ps))
	for i, p := range ps {
		b, err := json.Marshal(p.data)
		if err != nil {
			// The payload types above are plain data, so this cannot happen.
			panic(err)
		}
		out = append(out, Envelope{
			Schema:    Schema,
			RunID:     runID,
			Seq:       int64(i + 1),
			TS:        p.ts.UTC(),
			OffsetS:   round3(p.ts.Sub(res.StartedAt).Seconds()),
			Labels:    opt.Labels,
			Synthetic: true,
			Type:      p.typ,
			Data:      b,
		})
	}
	return out
}

// BuildFinished is the data of run.finished: the report content plus the
// diagnose text, with credentials redacted. Raw probe samples are kept only
// when samples is true.
func BuildFinished(res *report.Result, samples bool) Finished {
	fin := Finished{Result: *res, Diagnosis: diagnose.Text(res)}
	fin.Result.Probes = nil
	// The file a generator was run from is a local path: it stays in
	// report.json and does not travel with the stream.
	if res.LoadGenerator != nil {
		g := *res.LoadGenerator
		g.Path = ""
		fin.Result.LoadGenerator = &g
	}
	// report.json keeps fault parameters as they were written, credentials
	// included. The stream never does.
	fin.Result.FaultLog = redactFaultLog(res.FaultLog)
	if samples {
		fin.Probes = res.Probes
	}
	return fin
}

func redactFaultLog(in []report.FaultEvent) []report.FaultEvent {
	if in == nil {
		return nil
	}
	out := make([]report.FaultEvent, len(in))
	for i, f := range in {
		f.Tunables = Redact(f.Tunables)
		out[i] = f
	}
	return out
}

func probeNames(res *report.Result) []string {
	seen := map[string]bool{}
	var names []string
	for _, s := range res.Probes {
		if !seen[s.Name] {
			seen[s.Name] = true
			names = append(names, s.Name)
		}
	}
	return names
}

func round3(v float64) float64 {
	if v < 0 {
		v = 0
	}
	return float64(int64(v*1000+0.5)) / 1000
}
