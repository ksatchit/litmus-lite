package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/compose"
	"github.com/ksatchit/litmus-lite/internal/faults"
	"github.com/ksatchit/litmus-lite/internal/probe"
	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// recorder is an Emitter that remembers the calls it got, in order.
type recorder struct {
	mu       sync.Mutex
	log      []string
	points   []report.Point
	samples  int
	finished *report.Result
	failed   *failedCall
	started  *scenario.Scenario
	loadPts  []compose.LivePoint
	loadFin  []compose.LoadPoint
	loadErr  string
	loadTool string
	loadAt   float64
}

type failedCall struct {
	reason  string
	res     *report.Result
	started []report.FaultEvent
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.log = append(r.log, s)
	r.mu.Unlock()
}

func (r *recorder) Started(sc *scenario.Scenario, _ time.Time) { r.started = sc; r.add("started") }
func (r *recorder) Phase(n string)                             { r.add("phase:" + n) }
func (r *recorder) FaultStarted(f report.FaultEvent)           { r.add("fault.started:" + f.Name) }
func (r *recorder) FaultEnded(f report.FaultEvent)             { r.add("fault.ended:" + f.Name + ":" + f.Rollback) }
func (r *recorder) Warning(m string)                           { r.add("warning:" + m) }
func (r *recorder) Point(p report.Point) {
	r.mu.Lock()
	r.points = append(r.points, p)
	r.log = append(r.log, "point")
	r.mu.Unlock()
}
func (r *recorder) Sample(probe.Sample) {
	r.mu.Lock()
	r.samples++
	r.mu.Unlock()
}
func (r *recorder) LoadStarted(tool string, at float64) {
	r.mu.Lock()
	r.loadTool, r.loadAt = tool, at
	r.mu.Unlock()
	r.add("load.started")
}
func (r *recorder) LoadPoint(p compose.LivePoint) {
	r.mu.Lock()
	r.loadPts = append(r.loadPts, p)
	r.mu.Unlock()
}
func (r *recorder) LoadFinished(series []compose.LoadPoint, errMsg string) {
	r.mu.Lock()
	r.loadFin, r.loadErr = series, errMsg
	r.mu.Unlock()
	r.add("load.finished")
}
func (r *recorder) Finished(res *report.Result) { r.finished = res; r.add("finished") }
func (r *recorder) Failed(reason string, res *report.Result, started []report.FaultEvent) {
	r.failed = &failedCall{reason, res, started}
	r.add("failed")
}

// without drops the point entries, whose number depends on timing.
func (r *recorder) without(skip string) []string {
	var out []string
	for _, s := range r.log {
		if s != skip {
			out = append(out, s)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmitterSeesTheWholeRunAndPointsMatchTheReport(t *testing.T) {
	srv := okServer(t)
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "live"},
		Baseline: "1500ms",
		Recover:  "1200ms",
		Faults:   []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "1500ms"}},
		Probes: []scenario.Probe{{
			Name: "p", Type: "http", URL: srv.URL, Interval: "25ms", Mode: "continuous",
		}},
		SteadyState: []scenario.Probe{{Name: "s", Type: "http", URL: srv.URL}},
	}
	em := &recorder{}
	res, err := Run(context.Background(), sc, Options{Emitter: em})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"started", "phase:steady", "phase:inject", "fault.started:a", "phase:recover", "fault.ended:a:ok", "finished",
	}
	if got := em.without("point"); !equalStrings(got, want) {
		t.Fatalf("calls\n got %v\nwant %v", got, want)
	}
	if em.failed != nil || em.finished != res {
		t.Fatalf("terminal calls: failed=%v finished==res %v", em.failed, em.finished == res)
	}
	if em.samples == 0 {
		t.Fatal("no samples emitted")
	}
	// Every bucket of the final series is emitted once, in order, and a live
	// point carries the same values as the final one.
	if len(em.points) != len(res.TimeSeries) {
		t.Fatalf("%d points emitted, final series has %d", len(em.points), len(res.TimeSeries))
	}
	for i, p := range em.points {
		f := res.TimeSeries[i]
		if p.OffsetS != f.OffsetS || p.Phase != f.Phase || p.P95Ms != f.P95Ms || p.ErrorRate != f.ErrorRate ||
			p.Availability != f.Availability || p.RPS != f.RPS || p.Intensity != f.Intensity {
			t.Fatalf("point %d: live %+v, final %+v", i, p, f)
		}
	}
	// Points are emitted while the run is still going, not all at the end.
	firstPoint, lastPhase := -1, -1
	for i, s := range em.log {
		if s == "point" && firstPoint < 0 {
			firstPoint = i
		}
		if s == "phase:recover" {
			lastPhase = i
		}
	}
	if firstPoint < 0 || firstPoint > lastPhase {
		t.Fatalf("no point before the recover phase: %v", em.log)
	}
}

func TestEmitterGetsRunFailedOnEveryEarlyExit(t *testing.T) {
	srv := okServer(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()

	t.Run("steady-state probe fails", func(t *testing.T) {
		sc := &scenario.Scenario{
			Metadata: scenario.Metadata{Name: "x"}, Baseline: "10ms", Recover: "10ms",
			SteadyState: []scenario.Probe{{Name: "healthz", Type: "http", URL: bad.URL}},
		}
		em := &recorder{}
		if _, err := Run(context.Background(), sc, Options{Emitter: em}); err == nil {
			t.Fatal("want error")
		}
		if got := em.without("point"); !equalStrings(got, []string{"started", "failed"}) {
			t.Fatalf("calls %v", got)
		}
		if !strings.Contains(em.failed.reason, `steady-state probe "healthz" failed`) || em.failed.res != nil {
			t.Fatalf("failed %+v", em.failed)
		}
	})

	t.Run("bad duration", func(t *testing.T) {
		em := &recorder{}
		_, err := Run(context.Background(), &scenario.Scenario{Baseline: "nope", Recover: "1s"}, Options{Emitter: em})
		if err == nil {
			t.Fatal("want error")
		}
		if got := em.without("point"); !equalStrings(got, []string{"started", "failed"}) {
			t.Fatalf("calls %v", got)
		}
	})

	t.Run("second fault fails to start: the first is rolled back and announced", func(t *testing.T) {
		var calls int
		var first *fakeRun
		scriptedStart = func(scenario.Fault) (faults.Running, error) {
			calls++
			if calls == 2 {
				return nil, fmt.Errorf("boom")
			}
			first = &fakeRun{}
			return first, nil
		}
		sc := &scenario.Scenario{
			Metadata: scenario.Metadata{Name: "x"}, Baseline: "30ms", Recover: "10ms",
			Faults: []scenario.Fault{
				{Name: "a", Kind: "test.scripted", Duration: "1s"},
				{Name: "b", Kind: "test.scripted", Duration: "1s"},
			},
		}
		em := &recorder{}
		if _, err := Run(context.Background(), sc, Options{Emitter: em}); err == nil {
			t.Fatal("want error")
		}
		want := []string{"started", "phase:steady", "phase:inject", "fault.started:a", "fault.ended:a:ok", "failed"}
		if got := em.without("point"); !equalStrings(got, want) {
			t.Fatalf("calls\n got %v\nwant %v", got, want)
		}
		if first.stops != 1 {
			t.Fatalf("first fault stopped %d times", first.stops)
		}
		if len(em.failed.started) != 1 || em.failed.started[0].Name != "a" {
			t.Fatalf("started faults %+v", em.failed.started)
		}
	})

	t.Run("cancel during inject: rolled back, then run.failed interrupted", func(t *testing.T) {
		scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
		sc := &scenario.Scenario{
			Metadata: scenario.Metadata{Name: "x"}, Baseline: "30ms", Recover: "30ms",
			Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "30s"}},
			Probes: []scenario.Probe{{Name: "p", Type: "http", URL: srv.URL, Interval: "20ms", Mode: "continuous"}},
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(300 * time.Millisecond); cancel() }()
		start := time.Now()
		em := &recorder{}
		_, _ = Run(ctx, sc, Options{Emitter: em})
		if time.Since(start) > 5*time.Second {
			t.Fatal("run did not stop on cancel")
		}
		if em.failed == nil || em.failed.reason != "interrupted" || em.finished != nil {
			t.Fatalf("failed=%+v finished=%v", em.failed, em.finished)
		}
		got := em.without("point")
		if len(got) < 2 || got[len(got)-1] != "failed" || got[len(got)-2] != "fault.ended:a:ok" {
			t.Fatalf("calls %v", got)
		}
	})
}

func TestNoEmitterIsTheSameRun(t *testing.T) {
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
	sc := func() *scenario.Scenario {
		return &scenario.Scenario{
			Metadata: scenario.Metadata{Name: "x"}, Baseline: "20ms", Recover: "20ms",
			Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "20ms"}},
		}
	}
	a, err := Run(context.Background(), sc(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(context.Background(), sc(), Options{Emitter: &recorder{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Phases) != len(b.Phases) || len(a.FaultLog) != len(b.FaultLog) || len(a.TimeSeries) != len(b.TimeSeries) {
		t.Fatalf("results differ: %+v vs %+v", a, b)
	}
}

// helperLoad starts this test binary as a load generator.
func helperLoad(mode string) *scenario.LoadSpec {
	return &scenario.LoadSpec{
		Tool:    "command",
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestLoadHelperProcess$"},
	}
}

func TestLoadStartBaselineAlignsSeriesToGeneratorStart(t *testing.T) {
	t.Setenv("LITMUS_LITE_LOAD_HELPER", "1") // prints one point at offset 0
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }

	rpsBucket := func(start string) int {
		sc := &scenario.Scenario{
			Metadata: scenario.Metadata{Name: "x"}, Baseline: "2s", Recover: "100ms",
			Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "100ms"}},
			Load:   helperLoad("1"),
		}
		sc.Load.Start = start
		res, err := Run(context.Background(), sc, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range res.TimeSeries {
			if p.RPS == 12.5 {
				return i
			}
		}
		t.Fatalf("load point missing (start=%s) in %+v", start, res.TimeSeries)
		return -1
	}
	if got := rpsBucket(scenario.LoadStartInject); got < 2 {
		t.Fatalf("inject: load point landed in bucket %d, want the inject start (2)", got)
	}
	if got := rpsBucket(scenario.LoadStartBaseline); got != 0 {
		t.Fatalf("baseline: load point landed in bucket %d, want the generator start (0)", got)
	}
}

func TestLoadSleepHelperProcess(t *testing.T) {
	if os.Getenv("LITMUS_LITE_LOAD_HELPER") != "sleep" {
		return
	}
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

func TestBaselineLoadDoesNotOutliveAnEarlyExit(t *testing.T) {
	t.Setenv("LITMUS_LITE_LOAD_HELPER", "sleep")
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return nil, fmt.Errorf("boom") }
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "x"}, Baseline: "100ms", Recover: "10ms",
		Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "1s"}},
		Load: &scenario.LoadSpec{
			Tool: "command", Command: os.Args[0], Start: scenario.LoadStartBaseline,
			Args: []string{"-test.run=^TestLoadSleepHelperProcess$"},
		},
	}
	start := time.Now()
	em := &recorder{}
	if _, err := Run(context.Background(), sc, Options{Emitter: em}); err == nil {
		t.Fatal("want error")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("Run took %s: the generator was left running", d)
	}
	if em.failed == nil {
		t.Fatal("no run.failed")
	}
}

// A probe that takes longer than a bucket records its sample after the bucket
// has closed. The live point must wait for it, or it would disagree with the
// final series.
func TestLivePointWaitsForSlowProbes(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer slow.Close()
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "slow"}, Baseline: "1500ms", Recover: "1500ms",
		Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "1s"}},
		Probes: []scenario.Probe{{
			Name: "p", Type: "http", URL: slow.URL, Interval: "100ms", Timeout: "3s", Mode: "continuous",
		}},
	}
	em := &recorder{}
	res, err := Run(context.Background(), sc, Options{Emitter: em})
	if err != nil {
		t.Fatal(err)
	}
	if len(em.points) != len(res.TimeSeries) {
		t.Fatalf("%d points, final series has %d", len(em.points), len(res.TimeSeries))
	}
	for i, p := range em.points {
		f := res.TimeSeries[i]
		if p.P95Ms != f.P95Ms || p.RPS != f.RPS || p.ErrorRate != f.ErrorRate {
			t.Fatalf("point %d: live %+v, final %+v", i, p, f)
		}
	}
}

// fakeVegaLoad writes a stand-in for the vegaload binary. It records its
// arguments and a few variables of its environment in notes, and when it is
// given -stream-file it writes three point events (one per 100 ms) in
// vegaload.io/stream/v1 before it writes its -out result.
func fakeVegaLoad(t *testing.T, notes string) string {
	t.Helper()
	// A developer's own setting must not decide what the test sees.
	t.Setenv("VEGALOAD_LIVE", "on")
	t.Setenv("VEGALOAD_RUN_ID", "")
	t.Setenv("LITMUS_LITE_RUN_ID", "")
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	script := `#!/bin/sh
if [ "$1" = "version" ]; then
  echo "vegaload 1.2.3"
  exit 0
fi
if [ "$1" = "run" ] && [ "$2" = "-h" ]; then
  printf 'Usage: vegaload run [flags]\n  -no-open\n  -out string\n  -report string\n  -stream-file string\n'
  exit 2
fi
sf=""; out=""; ALLARGS="$*"
while [ $# -gt 0 ]; do
  case "$1" in
    -stream-file) sf="$2"; shift ;;
    -out) out="$2"; shift ;;
  esac
  shift
done
echo "live=$VEGALOAD_LIVE run=$LITMUS_LITE_RUN_ID vrun=$VEGALOAD_RUN_ID sf=$sf args=$ALLARGS" >> ` + notes + `
if [ -n "$sf" ]; then
  for i in 0 1 2; do
    printf '{"schema":"vegaload.io/stream/v1","runId":"x","seq":%d,"type":"point","data":{"offsetS":%d,"rps":%d.5,"requests":%d,"failed":0,"errorRate":0.0%d,"p50Ms":10.5,"p95Ms":2%d.0,"p99Ms":30.0}}\n' $((i+2)) $i $((100+i)) $((100+i)) $i $i >> "$sf"
    sleep 0.1
  done
fi
printf '{"time_series":[{"offset_ns":0,"rps":100.5,"error_rate":0},{"offset_ns":1000000000,"rps":101.5,"error_rate":0.01},{"offset_ns":2000000000,"rps":102.5,"error_rate":0.02}],"total":304,"failed":3,"error_rate":0.01}' > "$out"
`
	path := filepath.Join(t.TempDir(), "vegaload")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadScenario(srv *httptest.Server) *scenario.Scenario {
	return &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "with-load"}, Baseline: "300ms", Recover: "300ms",
		Faults: []scenario.Fault{{Name: "a", Kind: "test.scripted", Duration: "600ms"}},
		Probes: []scenario.Probe{{
			Name: "p", Type: "http", URL: srv.URL, Interval: "100ms", Timeout: "3s", Mode: "continuous",
		}},
		Load: &scenario.LoadSpec{Tool: "vegaload", Scenario: "x.vl.js", Start: scenario.LoadStartInject},
	}
}

// A streamed run asks vegaload for its points, relays them in order with the
// offsets moved to the run's clock, and ends the load with its series.
func TestStreamedRunRelaysTheLoadGeneratorsPoints(t *testing.T) {
	srv := okServer(t)
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
	notes := filepath.Join(t.TempDir(), "notes")
	em := &recorder{}
	res, err := Run(context.Background(), loadScenario(srv), Options{
		Emitter: em, RunID: "run-42", LoadBin: fakeVegaLoad(t, notes),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(notes)
	line := string(b)
	for _, want := range []string{"live=off", "run=run-42", "vrun=run-42", "sf=", "-no-open", "-report "} {
		if !strings.Contains(line, want) {
			t.Fatalf("the generator's environment lacks %q: %s", want, line)
		}
	}
	if strings.Contains(line, "sf= ") || strings.HasSuffix(strings.TrimSpace(line), "sf=") {
		t.Fatalf("no -stream-file was passed: %s", line)
	}
	if em.loadTool != "vegaload" || em.loadAt < 0.25 {
		t.Fatalf("load.started: %q at %.2f, want vegaload at about the start of inject", em.loadTool, em.loadAt)
	}
	if len(em.loadPts) != 3 {
		t.Fatalf("%d live load points, want 3: %+v", len(em.loadPts), em.loadPts)
	}
	for i, p := range em.loadPts {
		if want := em.loadAt + float64(i); p.OffsetS != want {
			t.Fatalf("point %d offset %.2f, want %.2f (generator offset moved to the run's clock)", i, p.OffsetS, want)
		}
		if p.P95Ms == nil || *p.P95Ms != float64(20+i) || p.RPS != float64(100+i)+0.5 {
			t.Fatalf("point %d lost data: %+v", i, p)
		}
	}
	if len(em.loadFin) != 3 || em.loadFin[1].OffsetS != em.loadAt+1 || em.loadErr != "" {
		t.Fatalf("load.finished: %+v err %q", em.loadFin, em.loadErr)
	}
	log := strings.Join(em.log, ",")
	if i, j, k := strings.Index(log, "load.started"), strings.Index(log, "load.finished"), strings.Index(log, "finished"); !(i >= 0 && i < j && j < k) {
		t.Fatalf("order: %s", log)
	}
	if len(res.Load) == 0 {
		t.Fatal("the generator's own result is still kept")
	}
	// The run records which generator ran: tool, version, and the file.
	if g := res.LoadGenerator; g == nil || g.Tool != "vegaload" || g.Version != "1.2.3" || g.Path == "" || g.Via != "explicit path" {
		t.Fatalf("loadGenerator in the result: %+v", g)
	}
}

// Without an emitter the generator is not asked for a stream file and the run
// id is not made up. It is still kept quiet: -no-open and a temporary report,
// as in every run.
func TestRunWithoutAnEmitterDoesNotStreamTheGenerator(t *testing.T) {
	srv := okServer(t)
	scriptedStart = func(scenario.Fault) (faults.Running, error) { return &fakeRun{}, nil }
	notes := filepath.Join(t.TempDir(), "notes")
	if _, err := Run(context.Background(), loadScenario(srv), Options{LoadBin: fakeVegaLoad(t, notes)}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(notes)
	if strings.Contains(string(b), "sf=/") {
		t.Fatalf("a stream file was requested without a stream: %s", b)
	}
	if !strings.Contains(string(b), "live=off") {
		t.Fatalf("the generator must never open its own live view under litmus-lite: %s", b)
	}
	if !strings.Contains(string(b), "-report ") || !strings.Contains(string(b), "-no-open") {
		t.Fatalf("an unstreamed run must keep the generator's own report out of the browser and the working directory: %s", b)
	}
	if strings.Contains(string(b), "-stream-file") {
		t.Fatalf("a stream file was requested without a stream: %s", b)
	}
}
