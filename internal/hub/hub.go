package hub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Item is one importable hub entry (a scenario template plus metadata).
//
// Two sources exist, and the difference matters for safety:
//
//   - Embedded (this file, the catalog var below): native fault kinds only,
//     shipped inside the binary. The embedded catalog must NEVER contain an
//     "exec" fault or an exec/cmd probe.
//   - Git-pinned remote hub (remote.go, pinned by .litmus-lite/hub.lock): the
//     only distribution channel for "exec" templates. A pin selects what can be
//     imported; it never grants permission to run anything.
//
// TODO(exec-templates): add a unit test that fails if any embedded catalog
// template contains kind: exec, type: exec, type: cmd, or "shell: true".
// TODO(exec-templates): add a Requires field (binaries, docker, os, privileges)
// so import/doctor can show what a template needs. See the spec's FR-XC-08.
type Item struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind"`
	OS       []string `json:"os"`
	Summary  string   `json:"summary"`
	Source   string   `json:"source,omitempty"`
	Template string   `json:"-"`
}

var catalog = []Item{
	{
		ID: "http.latency", Title: "HTTP latency", Kind: "http.latency", OS: []string{"linux", "darwin", "windows"},
		Summary: "Userspace reverse proxy adds delay (and optional status injection) in front of an HTTP service.",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: http-latency
target:
  kind: http
  baseUrl: http://127.0.0.1:8080
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
    mode: SOT
faults:
  - name: slow-api
    kind: http.latency
    duration: 12s
    params:
      listen: "127.0.0.1:18080"
      upstream: "127.0.0.1:8080"
      delay: 800ms
      jitter: 100ms
      statusOverride: { percent: 5, code: 500 }
probes:
  - name: list
    type: http
    url: http://127.0.0.1:18080/api/launches
    interval: 200ms
    mode: continuous
    expect: { status: 200 }
hypotheses:
  - name: errors-bounded
    metric: error_rate
    operator: <
    value: "25%"
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always
`,
	},
	{
		ID: "http.status-inject", Title: "HTTP status inject", Kind: "http.status-inject", OS: []string{"linux", "darwin", "windows"},
		Summary: "Inject HTTP error status codes through a userspace proxy.",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: http-status-inject
target:
  kind: http
  baseUrl: http://127.0.0.1:8080
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: five-hundreds
    kind: http.status-inject
    duration: 10s
    params:
      listen: "127.0.0.1:18080"
      upstream: "127.0.0.1:8080"
      percent: 20
      code: 500
probes:
  - name: list
    type: http
    url: http://127.0.0.1:18080/api/launches
    interval: 200ms
    expect: { status: 200 }
hypotheses:
  - name: still-mostly-ok
    metric: error_rate
    operator: <
    value: "50%"
rollback: always
`,
	},
	{
		ID: "http.timeout", Title: "HTTP timeout", Kind: "http.timeout", OS: []string{"linux", "darwin", "windows"},
		Summary: "Stall HTTP responses at a userspace proxy.",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: http-timeout
target:
  kind: http
  baseUrl: http://127.0.0.1:8080
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: stall
    kind: http.timeout
    duration: 8s
    params:
      listen: "127.0.0.1:18080"
      upstream: "127.0.0.1:8080"
      timeout: 3s
probes:
  - name: list
    type: http
    url: http://127.0.0.1:18080/api/launches
    interval: 500ms
    timeout: 1s
    expect: { status: 200 }
hypotheses:
  - name: recovers
    metric: recovery
    operator: <=
    value: 8s
rollback: always
`,
	},
	{
		ID: "process.pause", Title: "Process pause", Kind: "process.pause", OS: []string{"linux", "darwin"},
		Summary: "SIGSTOP then SIGCONT a PID listed in -allow-pid (not supported on Windows).",
		Template: `# Run with -allow-pid set to the PID this scenario may signal.
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: process-pause
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: freeze-engine
    kind: process.pause
    duration: 8s
    params:
      command: engine
probes:
  - name: engine-status
    type: http
    url: http://127.0.0.1:8080/api/engine/status
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: recovers
    metric: recovery
    operator: <=
    value: 8s
rollback: always
`,
	},
	{
		ID: "process.kill", Title: "Process kill", Kind: "process.kill", OS: []string{"linux", "darwin"},
		Summary: "SIGTERM then SIGKILL a PID listed in -allow-pid. Always requires -yes (not supported on Windows).",
		Template: `# Run with -yes and -allow-pid set to the PID this scenario may signal.
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: process-kill
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: stop-engine
    kind: process.kill
    duration: 8s
    params:
      command: engine
      sigkillAfter: 2s
probes:
  - name: engine-status
    type: http
    url: http://127.0.0.1:8080/api/engine/status
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: recovers
    metric: recovery
    operator: <=
    value: 8s
rollback: always
`,
	},
	{
		ID: "docker.pause", Title: "Docker pause", Kind: "docker.pause", OS: []string{"linux", "darwin", "windows"},
		Summary: "Pause a local Docker container via the docker CLI. Reversible (unpause). Skips if docker is missing.",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: docker-pause
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: freeze-container
    kind: docker.pause
    duration: 8s
    params:
      name: my-container
probes:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: recovers
    metric: recovery
    operator: <=
    value: 8s
rollback: always
`,
	},
	{
		ID: "cpu.hog", Title: "CPU hog", Kind: "cpu.hog", OS: []string{"linux", "darwin", "windows"},
		Summary: "Burn CPU in-process with a bounded worker count (max 16).",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: cpu-hog
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: burn
    kind: cpu.hog
    duration: 8s
    params:
      workers: 2
probes:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: stays-healthy
    metric: error_rate
    operator: "<"
    value: 5%
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always
`,
	},
	{
		ID: "memory.hog", Title: "Memory hog", Kind: "memory.hog", OS: []string{"linux", "darwin", "windows"},
		Summary: "Allocate a bounded heap block (cap 256MB) and release on rollback.",
		Template: `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: memory-hog
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: eat-ram
    kind: memory.hog
    duration: 8s
    params:
      size: 32MB
probes:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: stays-healthy
    metric: error_rate
    operator: "<"
    value: 5%
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always
`,
	},
	{
		ID: "disk.fill", Title: "Disk fill", Kind: "disk.fill", OS: []string{"linux", "darwin", "windows"},
		Summary: "Write a new file (refuses to overwrite), capped at 256MB, then delete it. Always requires -yes.",
		Template: `# Refuses to overwrite an existing file. Requires -yes.
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: disk-fill
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: fill-tmp
    kind: disk.fill
    duration: 8s
    params:
      path: /tmp/litmus-lite-fill.bin
      size: 16MB
probes:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    interval: 300ms
    expect: { status: 200 }
hypotheses:
  - name: stays-healthy
    metric: error_rate
    operator: "<"
    value: 5%
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always
`,
	},
	{
		ID: "failpoint", Title: "Failpoint", Kind: "failpoint", OS: []string{"linux", "darwin", "windows"},
		Summary: "Arm a named Go failpoint for the inject window, then disarm it. The process must allowlist the name and share the token.",
		Template: `# Start examples/cashier with LITMUS_LITE_FAILPOINT=charge,charge-slow and LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo.
# The token below must match that process. Hooks stay off until this run arms them.
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: cashier-charge
target:
  kind: http
  baseUrl: http://127.0.0.1:8091
baseline: 3s
recover: 4s
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8091/healthz
    expect: { status: 200 }
    mode: SOT
faults:
  - name: charge-fails
    kind: failpoint
    duration: 8s
    params:
      url: http://127.0.0.1:8091/_litmus/failpoint
      name: charge
      token: cashier-demo
probes:
  - name: charge
    type: http
    url: http://127.0.0.1:8091/charge
    interval: 200ms
    timeout: 1s
    mode: continuous
    expect: { status: 200 }
hypotheses:
  - name: hook-landed
    metric: error_rate
    operator: ">"
    value: "50%"
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always
`,
	},
}

func List() []Item {
	out := make([]Item, len(catalog))
	for i, it := range catalog {
		it.Source = "embedded"
		out[i] = it
	}
	return out
}

func Get(id string) (Item, error) {
	for _, it := range catalog {
		if it.ID == id {
			it.Source = "embedded"
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("unknown hub id %q", id)
}

func Search(q string) []Item {
	q = strings.ToLower(q)
	var out []Item
	for _, it := range List() {
		if strings.Contains(strings.ToLower(it.ID+" "+it.Title+" "+it.Kind+" "+it.Summary), q) {
			out = append(out, it)
		}
	}
	return out
}

func Import(id, beside, name string) (string, error) {
	it, err := Get(id)
	if err != nil {
		return "", err
	}
	return writeImport(it, beside, name)
}

func writeImport(it Item, beside, name string) (string, error) {
	if name == "" {
		name = strings.ReplaceAll(it.ID, ".", "-")
	}
	// name is a file stem: it must not walk out of the directory.
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("import name %q must be a file name, not a path", name)
	}
	if !strings.HasSuffix(name, ".chaos.yaml") {
		name += ".chaos.yaml"
	}
	if err := os.MkdirAll(beside, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(beside, name)
	// O_EXCL: an import never replaces a scenario that is already there (it may
	// hold hand-made changes), nor follows a link planted at that name.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s already exists; pass another -name or remove it", path)
		}
		return "", err
	}
	if _, err := f.WriteString(it.Template); err != nil {
		f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
