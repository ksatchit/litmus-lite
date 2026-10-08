package report

import (
	"strings"
	"testing"
	"time"
)

func TestObservedVsInjectedUnits(t *testing.T) {
	rows := ObservedVsInjected([]FaultEvent{
		{Name: "fill", Kind: "disk.fill", Intensity: 4096},
		{Name: "slow", Kind: "http.latency", Intensity: 800},
	}, Phase{P95: 50 * time.Millisecond})
	if strings.Contains(rows[0].Name, "p95") || !strings.Contains(rows[0].Injected, "bytes") {
		t.Fatalf("disk row %+v", rows[0])
	}
	if !strings.Contains(rows[1].Name, "p95") || rows[1].Injected != "800 ms" {
		t.Fatalf("latency row %+v", rows[1])
	}
}

func TestRenderHTMLStructure(t *testing.T) {
	start := time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)
	res := &Result{
		Name:      "launchpad-http-latency",
		StartedAt: start,
		Elapsed:   20 * time.Second,
		Passed:    true,
		Recovery:  600 * time.Millisecond,
		Phases: []Phase{
			{Name: "steady", Samples: 3, Availability: 1},
			{Name: "inject", Samples: 3, P95: 800 * time.Millisecond},
		},
		Hypotheses: []HypothesisVerdict{
			{Name: "errors-bounded", Metric: "error_rate", Operator: "<", Limit: "15%", Observed: "0%", Passed: true},
		},
		FaultLog: []FaultEvent{{Name: "slow-api", Kind: "http.latency", Start: start.Add(3 * time.Second), End: start.Add(15 * time.Second), Intensity: 800, Rollback: "ok"}},
		TimeSeries: []Point{
			{OffsetS: 0, P95Ms: 2, Availability: 1, Phase: "steady"},
			{OffsetS: 5, P95Ms: 820, Intensity: 800, Availability: 1, Phase: "inject"},
			{OffsetS: 12, P95Ms: 3, Availability: 1, Phase: "recover"},
		},
		Warnings: []string{"prometheus: down"},
	}
	h := RenderHTML(res)
	for _, want := range []string{"PASS", "Fault vs impact", "p95 (ms)", "clipPath", "0s", "Hypotheses", "prometheus: down"} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(h, "p95 ms / error") {
		t.Fatal("old overflowing svg title still present")
	}
	if !strings.Contains(h, "PASS") {
		t.Fatal("verdict words required")
	}
}
