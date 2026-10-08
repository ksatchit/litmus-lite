package k8s

import (
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestJobYAML(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: Demo_App }
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
	y := JobYAML(s)
	if !strings.Contains(y, "kind: Job") || strings.Contains(y, "ChaosEngine") {
		t.Fatal(y)
	}
}
