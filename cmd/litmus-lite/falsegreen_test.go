package main

import (
	"strings"
	"testing"
)

// A scenario with no hypotheses cannot fail, so `run` reported a pass, exit 0,
// whatever the probes saw. The shipped templates for cpu.hog, memory.hog and
// disk.fill were like that.
func TestNoHypothesesIsWarnedAboutAndStrictRefusesIt(t *testing.T) {
	f := writeScenario(t, plainScenario) // has no hypotheses
	code, _, stderr := run2(t, "validate", f)
	if code != 0 || !strings.Contains(stderr, "no hypotheses") {
		t.Fatalf("validate: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = run2(t, "validate", "-strict", f)
	if code != 1 || !strings.Contains(stderr, "no hypotheses") {
		t.Fatalf("validate -strict: exit %d, stderr %q", code, stderr)
	}
	code, out, _ := run2(t, "validate", "-output", "json", f)
	if code != 0 || !strings.Contains(out, "no hypotheses") {
		t.Fatalf("validate -output json: exit %d: %s", code, out)
	}
}

func TestAScenarioWithAHypothesisGetsNoSuchWarning(t *testing.T) {
	f := writeScenario(t, strings.Replace(bypassScenario, `url: "http://127.0.0.1:8080/"`, `url: "http://127.0.0.1:18090/"`, 1))
	code, _, stderr := run2(t, "validate", "-strict", f)
	if code != 0 || strings.Contains(stderr, "no hypotheses") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

// The commonest mistake with an HTTP fault: the probe calls the service, not
// the proxy, so nothing it measures is disturbed and the run passes.
const bypassScenario = `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: bypass }
baseline: 1s
recover: 1s
faults:
  - name: slow
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:18090", upstream: "127.0.0.1:8080", delay: 100ms }
probes:
  - { name: p, type: http, url: "http://127.0.0.1:8080/", interval: 200ms }
hypotheses:
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
`

func TestAProbeThatBypassesTheProxyIsWarnedAboutAndStrictRefusesIt(t *testing.T) {
	f := writeScenario(t, bypassScenario)
	code, _, stderr := run2(t, "validate", f)
	if code != 0 || !strings.Contains(stderr, "no probe calls that address") || !strings.Contains(stderr, "127.0.0.1:18090") {
		t.Fatalf("validate: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := run2(t, "validate", "-strict", f); code != 1 {
		t.Fatalf("validate -strict: exit %d", code)
	}
	through := strings.Replace(bypassScenario, `url: "http://127.0.0.1:8080/"`, `url: "http://127.0.0.1:18090/"`, 1)
	g := writeScenario(t, through)
	if code, _, stderr := run2(t, "validate", "-strict", g); code != 0 {
		t.Fatalf("a probe through the proxy: exit %d, stderr %q", code, stderr)
	}
}
