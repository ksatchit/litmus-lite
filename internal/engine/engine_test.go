package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/faults"
	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestHTTPLatencyRun(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer upstream.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()

	u := upstream.URL[len("http://"):]
	yaml := fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: lat }
baseline: 400ms
recover: 400ms
steadyState:
  - name: healthz
    type: http
    url: %s/healthz
    expect: { status: 200 }
faults:
  - name: slow
    kind: http.latency
    duration: 800ms
    params:
      listen: %q
      upstream: %q
      delay: 50ms
probes:
  - name: list
    type: http
    url: http://%s/
    interval: 80ms
    timeout: 2s
    expect: { status: 200 }
hypotheses:
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
`, upstream.URL, listen, u, listen)

	sc, err := scenario.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	res, err := Run(ctx, sc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TimeSeries) == 0 {
		t.Fatal("expected timeseries")
	}
	if len(res.FaultLog) != 1 || res.FaultLog[0].Rollback != "ok" {
		t.Fatalf("fault log %+v", res.FaultLog)
	}
	html := report.RenderHTML(res)
	if html == "" || len(html) < 100 {
		t.Fatal("html")
	}
	var inject report.Phase
	for _, p := range res.Phases {
		if p.Name == "inject" {
			inject = p
		}
	}
	if inject.Samples == 0 {
		t.Fatal("no inject samples")
	}
}

func TestBadPhaseDurations(t *testing.T) {
	_, err := Run(context.Background(), &scenario.Scenario{Baseline: "nope", Recover: "1s"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), &scenario.Scenario{Baseline: "1s", Recover: "nope"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "recover") {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), &scenario.Scenario{
		Baseline: "1ms",
		Recover:  "1ms",
		Probes: []scenario.Probe{{
			Name: "p", Type: "http", URL: "http://127.0.0.1:1/", Interval: "nope", Mode: "continuous",
		}},
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatal(err)
	}
}

func TestRollbackAttributedToFailingFault(t *testing.T) {
	var runs []*fakeRun
	scriptedStart = func(f scenario.Fault) (faults.Running, error) {
		fr := &fakeRun{}
		if scenario.StringParam(f.Params, "stop") == "fail" {
			fr.stopErr = fmt.Errorf("disk busy")
		}
		runs = append(runs, fr)
		return fr, nil
	}
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "rb"},
		Baseline: "10ms",
		Recover:  "10ms",
		Faults: []scenario.Fault{
			{Name: "bad", Kind: "test.scripted", Duration: "20ms", Params: map[string]any{"stop": "fail"}},
			{Name: "good", Kind: "test.scripted", Duration: "20ms"},
		},
	}
	res, err := Run(context.Background(), sc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FaultLog) != 2 {
		t.Fatalf("fault log %+v", res.FaultLog)
	}
	if res.FaultLog[0].Rollback != "fail: disk busy" {
		t.Fatalf("bad rollback %q", res.FaultLog[0].Rollback)
	}
	if res.FaultLog[1].Rollback != "ok" {
		t.Fatalf("good rollback %q", res.FaultLog[1].Rollback)
	}
	for i, r := range runs {
		if r.stops != 1 {
			t.Fatalf("fault %d stopped %d times", i, r.stops)
		}
	}
}

func TestProbeStopOnStartFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	var calls atomic.Int32
	scriptedStart = func(scenario.Fault) (faults.Running, error) {
		if calls.Add(1) == 2 {
			return nil, fmt.Errorf("boom")
		}
		return &fakeRun{}, nil
	}
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "leak"},
		Baseline: "40ms",
		Recover:  "10ms",
		Faults: []scenario.Fault{
			{Name: "a", Kind: "test.scripted", Duration: "1s"},
			{Name: "b", Kind: "test.scripted", Duration: "1s"},
		},
		Probes: []scenario.Probe{{
			Name: "p", Type: "http", URL: srv.URL, Interval: "15ms", Mode: "continuous",
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := Run(ctx, sc, Options{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	n := hits.Load()
	time.Sleep(120 * time.Millisecond)
	if hits.Load() != n {
		t.Fatalf("probes still running: %d -> %d", n, hits.Load())
	}
}

var scriptedStart func(scenario.Fault) (faults.Running, error)

type scriptedFault struct{}

func (scriptedFault) Name() string                  { return "test.scripted" }
func (scriptedFault) OS() []string                  { return nil }
func (scriptedFault) Validate(scenario.Fault) error { return nil }
func (scriptedFault) Start(_ context.Context, f scenario.Fault) (faults.Running, error) {
	if scriptedStart == nil {
		return nil, fmt.Errorf("scriptedStart not set")
	}
	return scriptedStart(f)
}

type fakeRun struct {
	stopErr error
	stops   int
}

func (f *fakeRun) Intensity() float64 { return 1 }
func (f *fakeRun) Stop() error {
	f.stops++
	if f.stops > 1 {
		return fmt.Errorf("stopped twice")
	}
	return f.stopErr
}

func init() {
	faults.Register(scriptedFault{})
}

func TestLoadHelperProcess(t *testing.T) {
	if os.Getenv("LITMUS_LITE_LOAD_HELPER") != "1" {
		return
	}
	fmt.Print(`{"time_series":[{"offset_s":0,"rps":12.5,"error_rate":0.25}]}`)
	os.Exit(0)
}

func TestLoadCommandMergesRPS(t *testing.T) {
	t.Setenv("LITMUS_LITE_LOAD_HELPER", "1")
	scriptedStart = func(scenario.Fault) (faults.Running, error) {
		return &fakeRun{}, nil
	}
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "load"},
		Baseline: "15ms",
		Recover:  "15ms",
		Faults: []scenario.Fault{{
			Name: "a", Kind: "test.scripted", Duration: "40ms",
		}},
		Load: &scenario.LoadSpec{
			Tool:    "command",
			Command: os.Args[0],
			Args:    []string{"-test.run=^TestLoadHelperProcess$"},
		},
	}
	res, err := Run(context.Background(), sc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) > 0 {
		t.Fatalf("warnings %v", res.Warnings)
	}
	for _, p := range res.TimeSeries {
		if p.RPS == 12.5 && p.ErrorRate == 0.25 {
			return
		}
	}
	t.Fatalf("load series missing from %+v", res.TimeSeries)
}

func TestHypothesisFail(t *testing.T) {
	h := scenario.Hypothesis{Name: "e", Metric: "error_rate", Operator: "<", Value: "1%"}
	ph := report.Phase{ErrorRate: 0.5}
	v := report.HypothesisEval(h, ph, 0)
	if v.Passed {
		t.Fatal("should fail")
	}
}
