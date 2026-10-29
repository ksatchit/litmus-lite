package stream

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/litmuschaos/litmus-lite/internal/compose"
	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// memSink collects lines. block, when set, holds every Write until it is closed.
type memSink struct {
	mu    sync.Mutex
	lines [][]byte
	block chan struct{}
	err   error // returned from every Write when set
}

func (m *memSink) Name() string { return "mem" }
func (m *memSink) Write(line []byte) error {
	if m.block != nil {
		<-m.block
	}
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	m.lines = append(m.lines, append([]byte(nil), line...))
	m.mu.Unlock()
	return nil
}
func (m *memSink) Close() error { return nil }
func (m *memSink) all() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append(bytes.Join(m.lines, []byte("\n")), '\n')
}
func (m *memSink) types(t *testing.T) []string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, l := range m.lines {
		var e Envelope
		if err := json.Unmarshal(l, &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e.Type)
	}
	return out
}

func testScenario() *scenario.Scenario {
	return &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "checkout"},
		Target:   scenario.Target{Kind: "http", BaseURL: "http://127.0.0.1:8080"},
		Baseline: "3s", Recover: "5s",
		SteadyState: []scenario.Probe{{Name: "healthz", Type: "http"}},
		Probes:      []scenario.Probe{{Name: "checkout", Type: "http"}},
		Faults: []scenario.Fault{{
			Name: "slow", Kind: "failpoint", Duration: "12s",
			Params: map[string]any{"token": "SECRET-TOKEN", "delay": "800ms"},
		}},
		Hypotheses: []scenario.Hypothesis{{Name: "recovers", Metric: "recovery", Operator: "<=", Value: "5s"}},
		Load:       &scenario.LoadSpec{Tool: "k6", Start: "baseline"},
	}
}

func drive(r *Recorder) *report.Result {
	start := time.Now()
	fault := report.FaultEvent{
		Name: "slow", Kind: "failpoint", Start: start.Add(3 * time.Second), End: start.Add(15 * time.Second),
		Tunables: map[string]any{"token": "SECRET-TOKEN", "delay": "800ms"}, Intensity: 800, Rollback: "ok",
	}
	r.Started(testScenario(), start)
	r.Phase("steady")
	r.Sample(probe.Sample{At: start, Name: "checkout", OK: true, Latency: 12 * time.Millisecond, Status: 200})
	r.Point(report.Point{OffsetS: 0, Phase: "steady", P95Ms: 12, Availability: 1, RPS: 5})
	r.Phase("inject")
	r.FaultStarted(fault)
	r.Warning("k6: not on PATH")
	r.Phase("recover")
	r.FaultEnded(fault)
	res := &report.Result{
		Name: "checkout", StartedAt: start, Passed: true,
		Phases:     []report.Phase{{Name: "steady", From: start, To: start.Add(3 * time.Second)}},
		FaultLog:   []report.FaultEvent{fault},
		Probes:     []probe.Sample{{At: start, Name: "checkout", OK: true}},
		TimeSeries: []report.Point{{T: start, Phase: "steady"}},
		Hypotheses: []report.HypothesisVerdict{{Name: "recovers", Metric: "recovery", Operator: "<=", Limit: "5s", Observed: "0.2s", Passed: true}},
	}
	r.Finished(res)
	return res
}

func TestRecorderWritesAValidStream(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "run-1", Labels: map[string]string{"service": "checkout"}, Version: "x"}, sink)
	drive(r)
	if stuck := r.Close(2 * time.Second); len(stuck) != 0 {
		t.Fatalf("stuck %v", stuck)
	}

	rec, err := Read(bytes.NewReader(sink.all()))
	if err != nil {
		t.Fatalf("not a valid recording: %v", err)
	}
	want := []string{"run.started", "phase.changed", "point", "phase.changed", "fault.started", "warning", "phase.changed", "fault.ended", "run.finished"}
	var got []string
	for _, e := range rec.Events {
		got = append(got, e.Type)
		if e.RunID != "run-1" || e.Labels["service"] != "checkout" || e.Synthetic {
			t.Fatalf("envelope %+v", e)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("types\n got %v\nwant %v", got, want)
	}
	// Probe samples are opt-in.
	for _, e := range rec.Events {
		if e.Type == TypeProbeSample {
			t.Fatal("probe.sample emitted without -stream-samples")
		}
	}

	sch := compileSchema(t)
	for i, raw := range rec.Raw {
		v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err != nil {
			t.Fatalf("event %d (%s) does not match the schema: %v", i+1, rec.Events[i].Type, err)
		}
	}

	var started RunStarted
	if err := json.Unmarshal(rec.Events[0].Data, &started); err != nil {
		t.Fatal(err)
	}
	if started.Name != "checkout" || started.Target == nil || started.Target.BaseURL != "http://127.0.0.1:8080" ||
		started.Baseline != "3s" || started.Recover != "5s" || len(started.Faults) != 1 ||
		len(started.Hypotheses) != 1 || started.Load == nil || started.Load.Start != "baseline" || started.Version != "x" {
		t.Fatalf("run.started %+v", started)
	}
	if strings.Join(started.Probes, ",") != "healthz,checkout" {
		t.Fatalf("probes %v", started.Probes)
	}
}

func TestRecorderSamplesAreOptIn(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "s", Samples: true}, sink)
	drive(r)
	r.Close(2 * time.Second)
	if n := strings.Count(strings.Join(sink.types(t), ","), "probe.sample"); n != 1 {
		t.Fatalf("%d probe.sample events", n)
	}
	rec, err := Read(bytes.NewReader(sink.all()))
	if err != nil {
		t.Fatal(err)
	}
	last := rec.Events[len(rec.Events)-1]
	var fin struct {
		Probes []json.RawMessage `json:"probes"`
	}
	_ = json.Unmarshal(last.Data, &fin)
	if len(fin.Probes) != 1 {
		t.Fatalf("raw samples missing from run.finished: %s", last.Data)
	}
}

func TestRecorderNeverEmitsCredentials(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "c"}, sink)
	drive(r)
	r2sink := &memSink{}
	r2 := NewRecorder(RecorderOptions{RunID: "d"}, r2sink)
	start := time.Now()
	r2.Started(testScenario(), start)
	f := report.FaultEvent{Name: "slow", Kind: "failpoint", Tunables: map[string]any{"token": "SECRET-TOKEN"}}
	r2.FaultStarted(f)
	r2.Failed("interrupted", &report.Result{Name: "x", FaultLog: []report.FaultEvent{f}, Phases: []report.Phase{{Name: "steady"}}}, []report.FaultEvent{f})
	r.Close(time.Second)
	r2.Close(time.Second)
	for name, s := range map[string]*memSink{"finished run": sink, "failed run": r2sink} {
		if bytes.Contains(s.all(), []byte("SECRET-TOKEN")) {
			t.Fatalf("%s leaks the token:\n%s", name, s.all())
		}
		if !bytes.Contains(s.all(), []byte(`"***"`)) {
			t.Fatalf("%s: token was dropped, not masked:\n%s", name, s.all())
		}
	}
}

func TestRecorderEndsExactlyOnce(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "once"}, sink)
	// Nothing before run.started.
	r.Phase("steady")
	r.Warning("early")
	drive(r)
	r.Failed("too late", nil, nil)
	r.Finished(&report.Result{})
	r.Point(report.Point{})
	r.Close(time.Second)
	types := sink.types(t)
	if types[0] != "run.started" {
		t.Fatalf("first event %s", types[0])
	}
	terminals := 0
	for _, ty := range types {
		if ty == TypeRunFinished || ty == TypeRunFailed {
			terminals++
		}
	}
	if terminals != 1 || types[len(types)-1] != TypeRunFinished {
		t.Fatalf("types %v", types)
	}
	if _, err := Read(bytes.NewReader(sink.all())); err != nil {
		t.Fatalf("recording invalid: %v", err)
	}
}

func TestRecorderFailedCarriesReasonAndStartedFaults(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "f"}, sink)
	r.Started(testScenario(), time.Now())
	r.Failed(`steady-state probe "healthz" failed`, nil, nil)
	r.Close(time.Second)
	rec, err := Read(bytes.NewReader(sink.all()))
	if err != nil {
		t.Fatal(err)
	}
	var f Failed
	_ = json.Unmarshal(rec.Events[1].Data, &f)
	if f.Reason != `steady-state probe "healthz" failed` || f.Result != nil || len(f.Faults) != 0 {
		t.Fatalf("run.failed %+v", f)
	}
}

func TestQueueDropsSamplesThenPointsAndNeverLifecycle(t *testing.T) {
	q := newQueue(3)
	for i := 0; i < 5; i++ {
		q.push(classSample, []byte("s"))
	}
	if q.samples != 3 || q.totalDropped != 2 {
		t.Fatalf("samples=%d dropped=%d", q.samples, q.totalDropped)
	}
	// A point makes room by evicting a sample.
	if dropped := q.push(classPoint, []byte("p")); dropped {
		t.Fatal("point dropped while samples were waiting")
	}
	if q.samples != 2 || q.points != 1 {
		t.Fatalf("samples=%d points=%d", q.samples, q.points)
	}
	q.push(classPoint, []byte("p"))
	q.push(classPoint, []byte("p")) // samples gone, queue full of points
	if q.samples != 0 || q.points != 3 {
		t.Fatalf("samples=%d points=%d", q.samples, q.points)
	}
	if dropped := q.push(classPoint, []byte("p")); !dropped {
		t.Fatal("a point should be dropped when the queue holds only points")
	}
	// Lifecycle events always get in.
	for i := 0; i < 10; i++ {
		if q.push(classLifecycle, []byte("l")) {
			t.Fatal("lifecycle event dropped")
		}
	}
	if len(q.items) != 13 {
		t.Fatalf("%d items queued", len(q.items))
	}
}

func TestRecorderDoesNotWaitForASinkThatIsStuck(t *testing.T) {
	stuck := &memSink{block: make(chan struct{})}
	good := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "stuck", QueueDepth: 50}, stuck, good)
	r.Started(testScenario(), time.Now())
	begin := time.Now()
	for i := 0; i < 20000; i++ {
		r.Sample(probe.Sample{Name: "p"}) // no-op: samples are off
		r.Point(report.Point{OffsetS: float64(i)})
	}
	r.Phase("inject")
	r.Finished(&report.Result{Name: "x"})
	if d := time.Since(begin); d > 3*time.Second {
		t.Fatalf("emitting took %s with a stuck sink", d)
	}
	// The healthy sink is unaffected.
	r2 := r.Close(200 * time.Millisecond)
	if len(r2) != 1 || r2[0] != "mem" {
		t.Fatalf("Close reported %v, want the stuck sink", r2)
	}
	if got := good.types(t); got[0] != "run.started" || got[len(got)-1] != "run.finished" {
		t.Fatalf("healthy sink got %d events, first %s last %s", len(got), got[0], got[len(got)-1])
	}
	close(stuck.block)
}

func TestStuckSinkLifecycleSurvivesAndDropsAreReported(t *testing.T) {
	stuck := &memSink{block: make(chan struct{})}
	r := NewRecorder(RecorderOptions{RunID: "drops", QueueDepth: 10}, stuck)
	r.Started(testScenario(), time.Now())
	for i := 0; i < 100; i++ {
		r.Point(report.Point{OffsetS: float64(i)})
	}
	r.Phase("inject")
	r.Finished(&report.Result{Name: "x"})
	close(stuck.block)
	r.Close(2 * time.Second)

	types := stuck.types(t)
	count := map[string]int{}
	for _, ty := range types {
		count[ty]++
	}
	if count["run.started"] != 1 || count["phase.changed"] != 1 || count["run.finished"] != 1 {
		t.Fatalf("lifecycle events lost: %v", count)
	}
	if count["point"] > 11 { // the queue holds 10, plus the one being written
		t.Fatalf("%d points were kept with a queue of 10", count["point"])
	}
	found := false
	for _, w := range r.Warnings() {
		if strings.Contains(w, "dropped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("drops were not reported: %v", r.Warnings())
	}
}

func TestDeadSinkOnlyAddsAWarning(t *testing.T) {
	dead := &memSink{err: errors.New("disk full")}
	good := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "dead"}, dead, good)
	drive(r)
	r.Close(time.Second)
	if len(good.types(t)) != 9 {
		t.Fatalf("healthy sink got %v", good.types(t))
	}
	w := strings.Join(r.Warnings(), "\n")
	if !strings.Contains(w, "disk full") {
		t.Fatalf("warnings %q", w)
	}
}

func TestFileSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "run.ndjson")
	fs, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(RecorderOptions{RunID: "file"}, fs)
	drive(r)
	r.Close(time.Second)
	rec, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.Terminal(); !ok || rec.RunID() != "file" {
		t.Fatalf("recording %+v", rec.Events)
	}
	// Starting again on the same path starts a new file.
	fs2, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	fs2.Close()
	if b, _ := os.ReadFile(path); len(b) != 0 {
		t.Fatal("file was not truncated")
	}
	// A path that cannot be written is an error, not a panic.
	blocker := filepath.Join(dir, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	if _, err := NewFileSink(filepath.Join(blocker, "run.ndjson")); err == nil {
		t.Fatal("want an error for an unwritable path")
	}
}

func TestRunIDs(t *testing.T) {
	for _, ok := range []string{"a", "demo-42", "9f2c4e1a7b3d", "run_1.2"} {
		if err := ValidRunID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a/b", "../x", "has space", strings.Repeat("a", 65), "é"} {
		if err := ValidRunID(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	a, b := NewRunID(), NewRunID()
	if len(a) != 12 || a == b || ValidRunID(a) != nil {
		t.Fatalf("ids %q %q", a, b)
	}
}

// The load events a recorder writes validate against the published schema,
// leave out what the generator did not report, and keep the order they came in.
func TestRecorderWritesLoadEventsThatMatchTheSchema(t *testing.T) {
	sink := &memSink{}
	rec := NewRecorder(RecorderOptions{RunID: "load-1"}, sink)
	rec.Started(&scenario.Scenario{Metadata: scenario.Metadata{Name: "n"}}, time.Now())
	p95 := 180.5
	rec.LoadStarted("vegaload", 3.0004)
	rec.LoadPoint(compose.LivePoint{OffsetS: 4, RPS: 118, ErrorRate: 0.01, P95Ms: &p95, Requests: 118, Failed: 1})
	rec.LoadPoint(compose.LivePoint{OffsetS: 5, RPS: 120}) // a tool that reports no percentiles
	rec.LoadFinished([]compose.LoadPoint{{OffsetS: 3, RPS: 120}, {OffsetS: 4, RPS: 118, ErrorRate: 0.01}}, "")
	rec.Close(time.Second)

	if got, want := strings.Join(sink.types(t), ","), "run.started,load.started,load.point,load.point,load.finished"; got != want {
		t.Fatalf("types %s, want %s", got, want)
	}
	sch := compileSchema(t)
	for i, l := range sink.lines {
		v, err := jsonschema.UnmarshalJSON(bytes.NewReader(l))
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err != nil {
			t.Fatalf("event %d: %v\n%s", i+1, err, l)
		}
	}
	var started, second Envelope
	_ = json.Unmarshal(sink.lines[1], &started)
	_ = json.Unmarshal(sink.lines[3], &second)
	if !strings.Contains(string(started.Data), `"startOffsetS":3`) || strings.Contains(string(started.Data), "3.0004") {
		t.Fatalf("offsets are rounded to milliseconds: %s", started.Data)
	}
	for _, key := range []string{"p50Ms", "p95Ms", "p99Ms", "requests", "failed"} {
		if strings.Contains(string(second.Data), key) {
			t.Fatalf("%s should be left out when not reported: %s", key, second.Data)
		}
	}
}
