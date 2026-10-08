package moveto

import (
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestIR(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: n }
faults:
  - name: a
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:1", upstream: "127.0.0.1:2" }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	ex := FromScenario(s)
	j := string(JSON(ex))
	if !strings.Contains(j, `"kind": "Experiment"`) {
		t.Fatal(j)
	}
	if err := Push("harness", ex); err == nil {
		t.Fatal("stub should error")
	}
	if !strings.Contains(Push("harness", ex).Error(), "not implemented") {
		t.Fatal(Push("harness", ex))
	}
	if err := Push("ir", ex); err != nil {
		t.Fatal(err)
	}
	if err := Push("litmus", ex); err != nil {
		t.Fatal(err)
	}
}
