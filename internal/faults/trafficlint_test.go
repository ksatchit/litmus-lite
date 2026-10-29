package faults

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func proxyScenario(kind, listen string, probes ...scenario.Probe) *scenario.Scenario {
	return &scenario.Scenario{
		Faults: []scenario.Fault{{Name: "slow", Kind: kind, Duration: "2s", Params: map[string]any{
			"listen": listen, "upstream": "127.0.0.1:8080", "delay": "100ms",
		}}},
		Probes: probes,
	}
}

func httpProbe(url string) scenario.Probe { return scenario.Probe{Name: "p", Type: "http", URL: url} }

func TestLintTraffic(t *testing.T) {
	cmd := scenario.Probe{Name: "c", Type: "cmd", Command: "curl -s localhost:18080"}
	cases := []struct {
		name string
		sc   *scenario.Scenario
		warn bool
	}{
		{"a probe goes through the proxy", proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:18080/x")), false},
		{"localhost is the loopback address", proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://localhost:18080/x")), false},
		{"the listen address as localhost", proxyScenario("http.latency", "localhost:18080", httpProbe("http://127.0.0.1:18080/x")), false},
		{"probes call the upstream", proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/x")), true},
		{"the same host, another port", proxyScenario("http.status-inject", "127.0.0.1:18080", httpProbe("http://127.0.0.1:18081/")), true},
		{"the http.timeout kind is checked too", proxyScenario("http.timeout", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/")), true},
		{"a steady-state probe on the upstream does not hide a probe on the proxy", func() *scenario.Scenario {
			s := proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:18080/"))
			s.SteadyState = []scenario.Probe{httpProbe("http://127.0.0.1:8080/healthz")}
			return s
		}(), false},
		{"a command probe may call the proxy: say nothing", proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/"), cmd), false},
		{"a load generator may call the proxy: say nothing", func() *scenario.Scenario {
			s := proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/"))
			s.Load = &scenario.LoadSpec{Tool: "k6", Scenario: "x.js"}
			return s
		}(), false},
		{"a fault that is not a proxy is not checked", proxyScenario("failpoint", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/")), false},
	}
	for _, c := range cases {
		got := lintTraffic(c.sc)
		if c.warn && len(got) != 1 {
			t.Errorf("%s: want one warning, got %v", c.name, got)
		}
		if !c.warn && len(got) != 0 {
			t.Errorf("%s: unexpected %v", c.name, got)
		}
	}
}

func TestLintTrafficNamesTheFaultTheListenAddressAndWhatProbesCall(t *testing.T) {
	got := lintTraffic(proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/x")))
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	for _, want := range []string{`"slow"`, "http.latency", "127.0.0.1:18080", "127.0.0.1:8080", "not at the upstream"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("lacks %q: %s", want, got[0])
		}
	}
}

// Lint is what validate and run show; the traffic check has to be part of it.
func TestLintIncludesTheTrafficCheck(t *testing.T) {
	got := Lint(proxyScenario("http.latency", "127.0.0.1:18080", httpProbe("http://127.0.0.1:8080/x")))
	found := false
	for _, w := range got {
		if strings.Contains(w, "no probe calls that address") {
			found = true
		}
	}
	if !found {
		t.Errorf("%v", got)
	}
}

// Requests counts what went through while the fault was armed, and nothing
// that arrived while it was not.
func TestProxyCountsRequestsOnlyWhileArmed(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()
	f := scenario.Fault{Name: "s", Kind: "http.latency", Duration: "2s", Params: map[string]any{
		"listen": listen, "upstream": up.URL[len("http://"):], "delay": "1ms",
	}}
	r, err := startHTTPProxy(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	tc, ok := r.(TrafficCounter)
	if !ok {
		t.Fatalf("%T does not count traffic", r)
	}
	get := func() {
		resp, err := http.Get("http://" + listen + "/")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	r.(Armable).Disarm()
	get()
	get()
	if n := tc.Requests(); n != 0 {
		t.Fatalf("requests while disarmed were counted: %d", n)
	}
	r.(Armable).Arm()
	get()
	get()
	get()
	if n := tc.Requests(); n != 3 {
		t.Fatalf("want 3, got %d", n)
	}
}
