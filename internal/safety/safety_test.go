package safety

import (
	"fmt"
	"os"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestLoopbackOK(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: a
    kind: http.latency
    duration: 2s
    params: { listen: "127.0.0.1:18080", upstream: "127.0.0.1:8080", delay: 10ms }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:18080/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteRefused(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: a
    kind: http.latency
    duration: 2s
    params: { listen: "127.0.0.1:18080", upstream: "example.com:80" }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:18080/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestDurationCap(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: a
    kind: http.latency
    duration: 2m
    params: { listen: "127.0.0.1:1", upstream: "127.0.0.1:2" }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err == nil {
		t.Fatal("expected cap")
	}
	if err := Check(s, Options{Yes: true}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessKillRequiresYes(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: k
    kind: process.kill
    duration: 2s
    params: { pid: 1234 }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err == nil {
		t.Fatal("process.kill requires -yes")
	}
	if err := Check(s, Options{Yes: true}); err == nil {
		t.Fatal("process.kill requires -allow-pid")
	}
	if err := Check(s, Options{Yes: true, AllowPIDs: []int{1234}}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessPauseRequiresAllowPID(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: k
    kind: process.pause
    duration: 2s
    params: { command: engine }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err == nil {
		t.Fatal("command match requires -allow-pid")
	}
	if err := Check(s, Options{AllowPIDs: []int{1234}}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRefusesInitAndSelf(t *testing.T) {
	initPID, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: k
    kind: process.kill
    duration: 2s
    params: { pid: 1 }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(initPID, Options{Yes: true, AllowPIDs: []int{1}}); err == nil {
		t.Fatal("pid 1 must be refused")
	}
	selfYAML := fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: k
    kind: process.pause
    duration: 2s
    params: { pid: %d }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`, os.Getpid())
	self, err := scenario.Parse([]byte(selfYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(self, Options{AllowPIDs: []int{os.Getpid()}}); err == nil {
		t.Fatal("own pid must be refused")
	}
}

func TestDiskFillRequiresYes(t *testing.T) {
	s, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: d
    kind: disk.fill
    duration: 2s
    params: { size: 1MB }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(s, Options{}); err == nil {
		t.Fatal("disk.fill requires -yes")
	}
	if err := Check(s, Options{Yes: true}); err != nil {
		t.Fatal(err)
	}
}
