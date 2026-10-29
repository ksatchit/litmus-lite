package stream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/compose"
	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// DefaultQueueDepth bounds how many points and samples a sink may have waiting.
const DefaultQueueDepth = 4096

var validRunID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidRunID reports whether id can be used as a run id. The id ends up in
// file names and URLs, so the set is small.
func ValidRunID(id string) error {
	if !validRunID.MatchString(id) {
		return fmt.Errorf("run id %q: use 1 to 64 characters from A-Z a-z 0-9 . _ -", id)
	}
	return nil
}

// NewRunID returns a random 12-hex-digit run id.
func NewRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to the clock.
		return fmt.Sprintf("%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	return hex.EncodeToString(b[:])
}

// RecorderOptions configures a Recorder.
type RecorderOptions struct {
	RunID  string            // default: NewRunID()
	Labels map[string]string // attached to every event
	// Samples also emits probe.sample and keeps raw samples on run.finished.
	Samples bool
	Version string
	// IncludeScenario adds the redacted scenario YAML to run.started.
	IncludeScenario bool
	// QueueDepth overrides DefaultQueueDepth (tests).
	QueueDepth int
}

// Recorder turns what the engine reports into events and hands each event to
// every sink. It implements engine.Emitter. Sequence numbers and timestamps
// are assigned once, so every sink sees the same events.
//
// A Recorder never blocks the caller on a sink and never returns an error to
// it: a failing sink only adds a warning.
type Recorder struct {
	opt   RecorderOptions
	sinks []*runner

	mu       sync.Mutex
	seq      int64
	start    time.Time
	started  bool
	finished bool
	warnings []string
	now      func() time.Time
}

// NewRecorder creates a Recorder that writes to sinks. With no sinks it still
// numbers events, which is useful in tests.
func NewRecorder(opt RecorderOptions, sinks ...Sink) *Recorder {
	if opt.RunID == "" {
		opt.RunID = NewRunID()
	}
	if opt.QueueDepth <= 0 {
		opt.QueueDepth = DefaultQueueDepth
	}
	r := &Recorder{opt: opt, now: time.Now}
	for _, s := range sinks {
		r.sinks = append(r.sinks, newRunner(s, opt.QueueDepth, r.dropped))
	}
	return r
}

// RunID returns the id used on every event.
func (r *Recorder) RunID() string { return r.opt.RunID }

// Warnings returns what has gone wrong with the sinks so far: a sink that
// stopped accepting events, and events dropped under pressure.
func (r *Recorder) Warnings() []string {
	r.mu.Lock()
	out := append([]string(nil), r.warnings...)
	r.mu.Unlock()
	out = append(out, runnerWarnings(r.sinks)...)
	return out
}

// dropped is called by a sink's goroutine when events were dropped. It tells
// every sink, and is also kept for Warnings.
func (r *Recorder) dropped(sink string, n int) {
	msg := fmt.Sprintf("stream: %d events dropped for %s", n, sink)
	r.mu.Lock()
	r.warnings = append(r.warnings, msg)
	r.mu.Unlock()
	r.emit(TypeWarning, Warning{Message: msg})
}

// Close stops accepting events, waits up to timeout for the sinks to write
// (and, for batching sinks, send) what is queued, and closes them. A sink that
// is still busy at the deadline is aborted. It returns the names of the sinks
// that did not finish.
func (r *Recorder) Close(timeout time.Duration) []string {
	return closeRunners(r.sinks, timeout)
}

// runnerWarnings returns what has gone wrong with the sinks: a sink that
// stopped accepting events, and what a sink reports about itself.
func runnerWarnings(rs []*runner) []string {
	var out []string
	for _, s := range rs {
		if err := s.failed(); err != nil {
			out = append(out, "stream: "+err.Error())
		}
		if w, ok := s.sink.(Warner); ok {
			out = append(out, w.Warnings()...)
		}
	}
	return out
}

// closeRunners tells the sinks the run is over, waits up to timeout for them
// to finish what is queued, and closes them. A sink that is still busy at the
// deadline is aborted. It returns the names of the sinks that did not finish.
func closeRunners(rs []*runner, timeout time.Duration) []string {
	var stuck []string
	deadline := time.After(timeout)
	for _, s := range rs {
		if d, ok := s.sink.(Drainer); ok {
			d.Drain()
		}
		s.q.close()
	}
	expired := false
	for _, s := range rs {
		if !expired {
			select {
			case <-s.done:
				_ = s.sink.Close()
				continue
			case <-deadline:
				expired = true
			}
		} else {
			select {
			case <-s.done:
				_ = s.sink.Close()
				continue
			default:
			}
		}
		// Out of time: give up on this sink.
		if a, ok := s.sink.(Aborter); ok {
			a.Abort()
		}
		select {
		case <-s.done:
			_ = s.sink.Close()
		case <-time.After(300 * time.Millisecond):
		}
		stuck = append(stuck, s.sink.Name())
	}
	return stuck
}

func (r *Recorder) emit(typ string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished || (!r.started && typ != TypeRunStarted) {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		// Event payloads are plain data; a failure here is a bug, and it must
		// not take the run down.
		return
	}
	ts := r.now().UTC()
	r.seq++
	env := Envelope{
		Schema:  Schema,
		RunID:   r.opt.RunID,
		Seq:     r.seq,
		TS:      ts,
		OffsetS: round3(ts.Sub(r.start).Seconds()),
		Labels:  r.opt.Labels,
		Type:    typ,
		Data:    b,
	}
	line, err := json.Marshal(env)
	if err != nil {
		r.seq--
		return
	}
	if typ == TypeRunFinished || typ == TypeRunFailed {
		r.finished = true
	}
	class := classOf(typ)
	for _, s := range r.sinks {
		s.q.push(class, line)
	}
}

// Started implements engine.Emitter.
func (r *Recorder) Started(sc *scenario.Scenario, startedAt time.Time) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.start = startedAt
	r.mu.Unlock()

	rs := RunStarted{
		Name:       sc.Metadata.Name,
		Baseline:   sc.Baseline,
		Recover:    sc.Recover,
		Faults:     []FaultSummary{},
		Hypotheses: []HypothesisSpec{},
		Version:    r.opt.Version,
	}
	if sc.Target.Kind != "" || sc.Target.BaseURL != "" {
		rs.Target = &Target{Kind: sc.Target.Kind, BaseURL: sc.Target.BaseURL}
	}
	for _, f := range sc.Faults {
		rs.Faults = append(rs.Faults, FaultSummary{
			Name: f.Name, Kind: f.Kind, Duration: f.Duration, Tunables: Redact(f.Params),
		})
	}
	seen := map[string]bool{}
	for _, p := range append(append([]scenario.Probe{}, sc.SteadyState...), sc.Probes...) {
		if p.Name != "" && !seen[p.Name] {
			seen[p.Name] = true
			rs.Probes = append(rs.Probes, p.Name)
		}
	}
	for _, h := range sc.Hypotheses {
		rs.Hypotheses = append(rs.Hypotheses, HypothesisSpec{
			Name: h.Name, Metric: h.Metric, Operator: h.Operator, Limit: h.Value,
		})
	}
	if sc.Load != nil && sc.Load.Tool != "" {
		rs.Load = &LoadInfo{Tool: sc.Load.Tool, Start: sc.Load.Start}
	}
	if r.opt.IncludeScenario {
		rs.Scenario = RedactedScenarioYAML(sc)
	}
	r.emit(TypeRunStarted, rs)
}

// Phase implements engine.Emitter.
func (r *Recorder) Phase(name string) { r.emit(TypePhaseChanged, PhaseChanged{Phase: name}) }

// FaultStarted implements engine.Emitter.
func (r *Recorder) FaultStarted(f report.FaultEvent) {
	r.emit(TypeFaultStarted, FaultStarted{
		Name: f.Name, Kind: f.Kind, Intensity: f.Intensity, Tunables: Redact(f.Tunables),
	})
}

// FaultEnded implements engine.Emitter.
func (r *Recorder) FaultEnded(f report.FaultEvent) {
	r.emit(TypeFaultEnded, FaultEnded{Name: f.Name, Kind: f.Kind, Rollback: f.Rollback})
}

// Point implements engine.Emitter.
func (r *Recorder) Point(p report.Point) {
	r.emit(TypePoint, Point{
		OffsetS:      p.OffsetS,
		Phase:        p.Phase,
		P95Ms:        p.P95Ms,
		ErrorRate:    p.ErrorRate,
		Availability: p.Availability,
		RPS:          p.RPS,
		Intensity:    p.Intensity,
	})
}

// Sample implements engine.Emitter. It is a no-op unless Samples was set.
func (r *Recorder) Sample(s probe.Sample) {
	if !r.opt.Samples {
		return
	}
	r.emit(TypeProbeSample, ProbeSample{
		Name: s.Name, OK: s.OK, Status: s.Status, Error: s.Error,
		LatencyMs: float64(s.Latency) / float64(time.Millisecond),
	})
}

// Warning implements engine.Emitter.
func (r *Recorder) Warning(msg string) { r.emit(TypeWarning, Warning{Message: msg}) }

// LoadStarted implements engine.Emitter.
func (r *Recorder) LoadStarted(tool string, startOffsetS float64) {
	r.emit(TypeLoadStarted, LoadStarted{Tool: tool, StartOffsetS: round3(startOffsetS)})
}

// LoadPoint implements engine.Emitter.
func (r *Recorder) LoadPoint(p compose.LivePoint) {
	r.emit(TypeLoadPoint, LoadPoint{
		OffsetS: round3(p.OffsetS), RPS: p.RPS, ErrorRate: p.ErrorRate,
		P50Ms: p.P50Ms, P95Ms: p.P95Ms, P99Ms: p.P99Ms,
		Requests: p.Requests, Failed: p.Failed,
	})
}

// LoadFinished implements engine.Emitter.
func (r *Recorder) LoadFinished(series []compose.LoadPoint, errMsg string) {
	f := LoadFinished{Series: make([]LoadSeriesPoint, 0, len(series)), Error: errMsg}
	for _, p := range series {
		f.Series = append(f.Series, LoadSeriesPoint{OffsetS: round3(p.OffsetS), RPS: p.RPS, ErrorRate: p.ErrorRate})
	}
	r.emit(TypeLoadFinished, f)
}

// Finished implements engine.Emitter.
func (r *Recorder) Finished(res *report.Result) {
	r.emit(TypeRunFinished, BuildFinished(res, r.opt.Samples))
}

// Failed implements engine.Emitter.
func (r *Recorder) Failed(reason string, res *report.Result, started []report.FaultEvent) {
	f := Failed{Reason: reason}
	if res != nil {
		fin := BuildFinished(res, false)
		f.Result = &fin.Result
	}
	for _, ev := range started {
		f.Faults = append(f.Faults, FaultSummary{
			Name: ev.Name, Kind: ev.Kind, Tunables: Redact(ev.Tunables),
		})
	}
	r.emit(TypeRunFailed, f)
}
