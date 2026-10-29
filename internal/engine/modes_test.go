package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// closedURL is a URL nothing listens on: every probe of it fails.
func closedURL(t *testing.T) string { return "http://" + freeAddr(t) + "/" }

func healthy(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	t.Cleanup(srv.Close)
	return srv
}

// modesScenario is a short scenario: 600 ms of baseline, a fault for 800 ms,
// 600 ms of recovery, and whatever probes and hypotheses the test passes in.
func modesScenario(t *testing.T, upstream string, probes, hyps string) *scenario.Scenario {
	t.Helper()
	sc, err := scenario.Parse([]byte(fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: modes }
baseline: 600ms
recover: 600ms
faults:
  - name: slow
    kind: http.latency
    duration: 800ms
    params: { listen: %q, upstream: %q, delay: 10ms }
probes:
%s
hypotheses:
%s
`, freeAddr(t), strings.TrimPrefix(upstream, "http://"), probes, hyps)))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func runModes(t *testing.T, sc *scenario.Scenario) (*report.Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return Run(ctx, sc, Options{})
}

func phaseOf(res *report.Result, name string) report.Phase {
	for _, p := range res.Phases {
		if p.Name == name {
			return p
		}
	}
	return report.Phase{}
}

// An on-chaos probe samples while the faults are armed and at no other time.
// It used to run for the whole run, like a continuous probe, so a scenario
// moved to another tool meant something else there.
func TestOnChaosProbeRunsOnlyWhileTheFaultIsArmed(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: always, type: http, url: %[1]q, interval: 50ms, mode: continuous }
  - { name: during, type: http, url: %[1]q, interval: 50ms, mode: on-chaos }
`, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 50% }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	inject := phaseOf(res, "inject")
	var during, always, outside int
	for _, s := range res.Probes {
		switch s.Name {
		case "during":
			during++
			// A probe that started a moment before the window closed is the
			// probe's own timing; one that started outside it is the bug.
			if s.At.Before(inject.From) || s.At.After(inject.To) {
				outside++
			}
		case "always":
			always++
		}
	}
	if during == 0 {
		t.Fatal("the on-chaos probe never ran")
	}
	if outside != 0 {
		t.Fatalf("%d of %d on-chaos samples were taken outside the inject window [%s, %s]", outside, during, inject.From, inject.To)
	}
	if always <= during {
		t.Fatalf("the continuous probe (%d samples) should have sampled the whole run, the on-chaos one (%d) only the window", always, during)
	}
}

// An inject window shorter than the interval must still be sampled: an
// on-chaos probe with a long interval used to leave the window with no data,
// and "error_rate < 5%" passed on it.
func TestOnChaosProbeSamplesAWindowShorterThanItsInterval(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: during, type: http, url: %q, interval: 30s, mode: on-chaos }
`, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 5% }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	if n := phaseOf(res, "inject").Samples; n != 1 {
		t.Fatalf("want exactly one sample in the window, got %d", n)
	}
	if !res.Passed {
		t.Fatalf("%+v", res.Hypotheses)
	}
}

// SOT in probes runs once before anything is injected, as steadyState does,
// and a failure stops the run. It used to be skipped with no sign of it.
func TestSOTProbeRunsOnceBeforeTheFaultAndStopsTheRunWhenItFails(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: start, type: http, url: %q, mode: SOT }
`, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 5% }
`)
	// Everything but the SOT probe is missing, so the inject window is empty:
	// that is not what this test is about. It only counts the SOT samples.
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range res.Probes {
		if s.Name == "start" {
			n++
			if !s.At.Before(res.FaultLog[0].Start) {
				t.Fatalf("the SOT sample was taken at %s, after the fault started at %s", s.At, res.FaultLog[0].Start)
			}
		}
	}
	if n != 1 {
		t.Fatalf("an SOT probe samples once, got %d", n)
	}

	bad := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: start, type: http, url: %q, mode: SOT, timeout: 300ms }
`, closedURL(t)), `  - { name: ok, metric: error_rate, operator: "<", value: 5% }`)
	if _, err := runModes(t, bad); err == nil || !strings.Contains(err.Error(), `steady-state probe "start" failed`) {
		t.Fatalf("a failing SOT probe must stop the run: %v", err)
	}
}

func TestEOTProbeRunsOnceAfterRecovery(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: end, type: http, url: %q, mode: EOT }
  - { name: always, type: http, url: %q, interval: 100ms }
`, up.URL, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 50% }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range res.Probes {
		if s.Name == "end" {
			n++
			if s.At.Before(res.FaultLog[0].End) {
				t.Fatal("the EOT sample was taken before the fault ended")
			}
		}
	}
	if n != 1 {
		t.Fatalf("an EOT probe samples once, got %d", n)
	}
	// Building the list of EOT probes must not write into the scenario's own
	// steadyState slice.
	if len(sc.SteadyState) != 0 {
		t.Fatalf("steadyState was changed: %+v", sc.SteadyState)
	}
}

// The case from the PRD's baseline: the inject window holds no sample at all.
// The run measured nothing, and a hypothesis about it must not pass.
func TestAnEmptyInjectWindowIsNoDataNotAPass(t *testing.T) {
	up := healthy(t)
	probes := fmt.Sprintf(`
  - { name: end, type: http, url: %q, mode: EOT }
`, up.URL)
	sc := modesScenario(t, up.URL, probes, `  - { name: ok, metric: error_rate, operator: "<", value: 5% }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || len(res.Hypotheses) != 1 || !res.Hypotheses[0].NoData || res.Hypotheses[0].Observed != "no data" {
		t.Fatalf("passed=%v %+v", res.Passed, res.Hypotheses)
	}

	sc = modesScenario(t, up.URL, probes, `  - { name: ok, metric: error_rate, operator: "<", value: 5%, allowNoData: true }`)
	res, err = runModes(t, sc)
	if err != nil || !res.Passed {
		t.Fatalf("allowNoData: err=%v %+v", err, res.Hypotheses)
	}
}

// A service that never comes back must not satisfy "recovery <= 30s" just
// because the recovery window was shorter than 30 s.
func TestAServiceThatNeverRecoversFailsARecoveryHypothesis(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: dead, type: http, url: %q, interval: 50ms, timeout: 100ms }
`, closedURL(t)), `  - { name: back, metric: recovery, operator: "<=", value: 30s }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Hypotheses[0].Observed != "not recovered" {
		t.Fatalf("passed=%v %+v", res.Passed, res.Hypotheses)
	}
}

func TestRunWritesTheReportSchemaVersion(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: p, type: http, url: %q, interval: 100ms }
`, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 50% }`)
	res, err := runModes(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != report.SchemaVersion {
		t.Fatalf("schemaVersion %d", res.SchemaVersion)
	}
}

// Warnings found before the run are in the result, ahead of the run's own, so
// that report.json and the stream's run.finished agree on them.
func TestStartupWarningsLeadTheResultsWarnings(t *testing.T) {
	up := healthy(t)
	sc := modesScenario(t, up.URL, fmt.Sprintf(`
  - { name: p, type: http, url: %q, interval: 100ms }
`, up.URL), `  - { name: ok, metric: error_rate, operator: "<", value: 50% }`)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := Run(ctx, sc, Options{Warnings: []string{"first", "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) < 2 || res.Warnings[0] != "first" || res.Warnings[1] != "second" {
		t.Fatalf("warnings %v", res.Warnings)
	}
}
