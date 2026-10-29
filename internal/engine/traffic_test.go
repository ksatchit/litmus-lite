package engine

import (
	"fmt"
	"strings"
	"testing"
)

func trafficScenarioFor(t *testing.T, probeURL func(listen string) string) (listen string, run func() []string) {
	t.Helper()
	up := healthy(t)
	listen = freeAddr(t)
	probes := fmt.Sprintf("  - { name: p, type: http, url: %q, interval: 50ms }", probeURL(listen))
	sc := modesScenario(t, up.URL, probes, `  - { name: ok, metric: error_rate, operator: "<", value: 50% }`)
	sc.Faults[0].Params["listen"] = listen
	return listen, func() []string {
		res, err := runModes(t, sc)
		if err != nil {
			t.Fatal(err)
		}
		return res.Warnings
	}
}

func hasNoTraffic(warnings []string) bool {
	for _, w := range warnings {
		if strings.Contains(w, "saw no requests while it was armed") {
			return true
		}
	}
	return false
}

// A probe that calls the upstream directly never meets the proxy. The run
// passed and said nothing: the fault had touched no request.
func TestAProxyThatSawNoRequestsIsReported(t *testing.T) {
	up := healthy(t)
	_, run := trafficScenarioFor(t, func(string) string { return up.URL + "/" })
	warnings := run()
	if !hasNoTraffic(warnings) {
		t.Fatalf("no warning that the fault saw no requests: %v", warnings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "saw no requests") && !strings.Contains(w, `"slow"`) {
			t.Errorf("the warning does not name the fault: %s", w)
		}
	}
}

func TestAProxyThatSawRequestsIsNotReported(t *testing.T) {
	_, run := trafficScenarioFor(t, func(listen string) string { return "http://" + listen + "/" })
	if warnings := run(); hasNoTraffic(warnings) {
		t.Fatalf("a probe went through the proxy, but: %v", warnings)
	}
}
