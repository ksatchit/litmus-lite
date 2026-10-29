package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/litmuschaos/litmus-lite/failpoint"
	"github.com/litmuschaos/litmus-lite/internal/faults"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
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

func TestFailpointArmsDuringInject(t *testing.T) {
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "cashier-demo")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	// What the server answered, and when, so that a failure in the steady
	// phase can be told from a probe that never reached the server.
	var seenMu sync.Mutex
	var seen []string
	started := time.Now()
	mux.HandleFunc("/charge", func(w http.ResponseWriter, r *http.Request) {
		code := 200
		if err := failpoint.Fail("charge"); err != nil {
			code = http.StatusServiceUnavailable
		}
		seenMu.Lock()
		seen = append(seen, fmt.Sprintf("%dms:%d", time.Since(started).Milliseconds(), code))
		seenMu.Unlock()
		w.WriteHeader(code)
	})
	mux.Handle("/_litmus/failpoint", failpoint.Handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	yaml := fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: cashier-charge }
# The probe interval is 80ms, so a baseline of 360ms ends 40ms from the
# nearest probe. A multiple of 80ms would put a probe on the boundary where
# the hook is armed, and CI showed that probe seeing the fault (503) while it
# still belongs to the steady phase.
baseline: 360ms
recover: 800ms
steadyState:
  - name: healthz
    type: http
    url: %s/healthz
    expect: { status: 200 }
faults:
  - name: charge-fails
    kind: failpoint
    duration: 1s
    params:
      url: %s/_litmus/failpoint
      name: charge
      token: cashier-demo
probes:
  - name: charge
    type: http
    url: %s/charge
    interval: 80ms
    timeout: 1s
    expect: { status: 200 }
hypotheses:
  - name: hook-landed
    metric: error_rate
    operator: ">"
    value: "50%%"
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
`, srv.URL, srv.URL, srv.URL)
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
	if !res.Passed {
		t.Fatalf("hypotheses %+v", res.Hypotheses)
	}
	var steady, inject, rec report.Phase
	for _, p := range res.Phases {
		switch p.Name {
		case "steady":
			steady = p
		case "inject":
			inject = p
		case "recover":
			rec = p
		}
	}
	if steady.Samples == 0 || steady.ErrorRate > 0 {
		seenMu.Lock()
		defer seenMu.Unlock()
		t.Fatalf("steady %+v\nthe server saw /charge at: %v", steady, seen)
	}
	if inject.ErrorRate < 0.5 {
		t.Fatalf("inject %+v", inject)
	}
	if rec.Samples == 0 || rec.ErrorRate > 0.5 {
		t.Fatalf("recover %+v", rec)
	}
	if len(res.FaultLog) != 1 || res.FaultLog[0].Rollback != "ok" || res.FaultLog[0].Kind != "failpoint" {
		t.Fatalf("fault log %+v", res.FaultLog)
	}
}

func TestHypothesisFail(t *testing.T) {
	h := scenario.Hypothesis{Name: "e", Metric: "error_rate", Operator: "<", Value: "1%"}
	ph := report.Phase{Samples: 10, Failed: 5, ErrorRate: 0.5}
	v := report.HypothesisEval(h, ph, report.RecoveryInfo{})
	if v.Passed {
		t.Fatal("should fail")
	}
}
