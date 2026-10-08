package diagnose

import (
	"fmt"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/report"
)

func Text(res *report.Result) string {
	var b strings.Builder
	if res.Passed {
		fmt.Fprintf(&b, "Run %s passed.\n", res.Name)
	} else {
		fmt.Fprintf(&b, "Run %s failed hypotheses.\n", res.Name)
	}
	var inject *report.Phase
	for i := range res.Phases {
		if res.Phases[i].Name == "inject" {
			inject = &res.Phases[i]
		}
	}
	if inject != nil {
		p95 := float64(inject.P95.Milliseconds())
		for _, f := range res.FaultLog {
			if report.LatencyKind(f.Kind) && f.Intensity > 0 {
				if p95 < f.Intensity*0.3 {
					fmt.Fprintf(&b, "Fault %s may not have landed: injected %.0f ms but inject-phase p95 is %.0f ms. Point probes and load at the proxy listen address, not the upstream.\n", f.Name, f.Intensity, p95)
				} else {
					fmt.Fprintf(&b, "Fault %s appears to have landed: inject-phase p95 is %.0f ms versus injected %.0f ms.\n", f.Name, p95, f.Intensity)
				}
				continue
			}
			fmt.Fprintf(&b, "Fault %s (%s) injected %s. Probe p95 is not used to judge whether this kind landed.\n", f.Name, f.Kind, report.IntensityText(f))
		}
		if inject.ErrorRate > 0.05 {
			fmt.Fprintf(&b, "Error rate during inject is %.1f%%. If this is unexpected, check timeouts, retries, and circuit breakers on the path under test.\n", inject.ErrorRate*100)
		}
	}
	fmt.Fprintf(&b, "Recovery time-to-healthy: %s.\n", res.Recovery)
	for _, h := range res.Hypotheses {
		if !h.Passed {
			fmt.Fprintf(&b, "Hypothesis %s failed: %s %s %s (observed %s).\n", h.Name, h.Metric, h.Operator, h.Limit, h.Observed)
		}
	}
	if len(res.Load) > 0 {
		fmt.Fprintf(&b, "VegaLoad JSON was merged into the overlay (RPS and error rate from time_series; probe p95 kept for latency).\n")
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", w)
	}
	return b.String()
}
