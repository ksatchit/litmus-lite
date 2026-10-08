package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestHypothesisFail(t *testing.T) {
	h := scenario.Hypothesis{Name: "e", Metric: "error_rate", Operator: "<", Value: "1%"}
	ph := report.Phase{ErrorRate: 0.5}
	v := report.HypothesisEval(h, ph, 0)
	if v.Passed {
		t.Fatal("should fail")
	}
}
