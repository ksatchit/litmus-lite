package diagnose

import (
	"strings"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/report"
)

func TestTextDoesNotTreatEveryIntensityAsLatency(t *testing.T) {
	res := &report.Result{
		Name:     "mix",
		Passed:   false,
		Recovery: 9 * time.Second,
		Phases: []report.Phase{{
			Name: "inject", P95: 12 * time.Millisecond, ErrorRate: 0.2,
		}},
		FaultLog: []report.FaultEvent{
			{Name: "fill", Kind: "disk.fill", Intensity: 16 << 20},
			{Name: "freeze", Kind: "process.pause", Intensity: 100},
			{Name: "slow", Kind: "http.latency", Intensity: 800},
		},
		Hypotheses: []report.HypothesisVerdict{{
			Name: "recovers", Metric: "recovery", Operator: "<=", Limit: "5s", Observed: "9s", Passed: false,
		}},
	}
	text := Text(res)
	if strings.Contains(text, "LaunchPad") {
		t.Fatalf("hard-coded demo path:\n%s", text)
	}
	if strings.Contains(text, "missing a fast timeout") {
		t.Fatalf("fixed recovery threshold:\n%s", text)
	}
	if strings.Count(text, "may not have landed") != 1 || !strings.Contains(text, "Fault slow may not have landed") {
		t.Fatalf("latency landing check:\n%s", text)
	}
	if !strings.Contains(text, "disk.fill") || !strings.Contains(text, "bytes") {
		t.Fatalf("disk.fill line:\n%s", text)
	}
	if !strings.Contains(text, "process.pause") {
		t.Fatalf("pause line:\n%s", text)
	}
	if !strings.Contains(text, "path under test") {
		t.Fatalf("error-rate line:\n%s", text)
	}
	if !strings.Contains(text, "Hypothesis recovers failed") {
		t.Fatalf("hypothesis:\n%s", text)
	}
}

func TestLatencyFaultLanded(t *testing.T) {
	res := &report.Result{
		Name:   "lat",
		Passed: true,
		Phases: []report.Phase{{
			Name: "inject", P95: 700 * time.Millisecond,
		}},
		FaultLog: []report.FaultEvent{
			{Name: "slow", Kind: "http.latency", Intensity: 800},
		},
	}
	text := Text(res)
	if !strings.Contains(text, "appears to have landed") {
		t.Fatal(text)
	}
	if strings.Contains(text, "may not have landed") {
		t.Fatal(text)
	}
}
