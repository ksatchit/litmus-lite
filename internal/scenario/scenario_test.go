package scenario

import "testing"

func TestParseMinimal(t *testing.T) {
	s, err := Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: t
faults:
  - name: a
    kind: http.latency
    duration: 2s
    params:
      listen: 127.0.0.1:9
      upstream: 127.0.0.1:8
probes:
  - name: p
    type: http
    url: http://127.0.0.1:9/
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Rollback != "always" {
		t.Fatalf("rollback default %q", s.Rollback)
	}
}

func TestRejectsEmptyFaults(t *testing.T) {
	_, err := Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
probes: [{ name: p, type: http, url: http://127.0.0.1/ }]
`))
	if err == nil {
		t.Fatal("expected error")
	}
}
