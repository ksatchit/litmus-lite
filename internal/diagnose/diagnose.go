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
	if inject != nil && len(res.FaultLog) > 0 {
		inj := res.FaultLog[0].Intensity
		p95 := float64(inject.P95.Milliseconds())
		if inj > 0 && p95 < inj*0.3 {
			fmt.Fprintf(&b, "Fault may not have landed: injected intensity %.0f but inject-phase p95 is %.0f ms. Point probes/load at the proxy listen address, not the upstream.\n", inj, p95)
		} else if inj > 0 {
			fmt.Fprintf(&b, "Fault appears to have landed: inject-phase p95 is %.0f ms versus injected intensity %.0f.\n", p95, inj)
		}
		if inject.ErrorRate > 0.05 {
			fmt.Fprintf(&b, "Error rate during inject is %.1f%%. If this is unexpected, check timeouts, retries, and circuit breakers in the client (LaunchPad ignite path).\n", inject.ErrorRate*100)
		}
	}
	if res.Recovery > 5e9 { // 5s in ns Duration
		fmt.Fprintf(&b, "Recovery took %s. The service may be missing a fast timeout or retry after the fault stops.\n", res.Recovery)
	} else {
		fmt.Fprintf(&b, "Recovery time-to-healthy: %s.\n", res.Recovery)
	}
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
