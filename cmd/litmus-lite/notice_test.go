package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const commandScenario = `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: runs-commands }
baseline: 1s
recover: 1s
steadyState:
  - name: ready
    type: cmd
    command: "echo ready"
    mode: SOT
faults:
  - name: slow
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:0", upstream: "127.0.0.1:1", delay: 10ms }
probes:
  - name: db
    type: command
    command: "echo \x1b[2Ahidden; echo ok"
    interval: 200ms
    mode: continuous
`

const plainScenario = `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: plain }
baseline: 1s
recover: 1s
faults:
  - name: slow
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:0", upstream: "127.0.0.1:1", delay: 10ms }
probes:
  - { name: p, type: http, url: "http://127.0.0.1:1/", interval: 200ms }
`

func TestValidateNamesTheCommandsAScenarioRuns(t *testing.T) {
	f := writeScenario(t, commandScenario)
	code, stdout, stderr := run2(t, "validate", f)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	for _, want := range []string{"notice:", `probe "ready": sh -c "echo ready"`, `probe "db"`, "Read them before you run"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	// A command is shown quoted: a raw escape sequence would let a scenario
	// rewrite what the terminal shows.
	if strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "\033[2A") {
		t.Errorf("an escape sequence reached the terminal:\n%q", stderr)
	}
	if strings.TrimSpace(stdout) != "valid" {
		t.Errorf("stdout should stay exactly %q, got %q", "valid", stdout)
	}
}

func TestValidateJSONListsTheCommands(t *testing.T) {
	f := writeScenario(t, commandScenario)
	code, stdout, stderr := run2(t, "validate", "-output", "json", f)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	var doc struct {
		Valid        bool     `json:"valid"`
		RunsCommands []string `json:"runsCommands"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if !doc.Valid || len(doc.RunsCommands) != 2 {
		t.Fatalf("want two commands listed, got %+v", doc)
	}
}

func TestAScenarioWithoutCommandsGetsNoNotice(t *testing.T) {
	f := writeScenario(t, plainScenario)
	code, stdout, stderr := run2(t, "validate", "-output", "json", f)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "notice:") || strings.Contains(stdout, "runsCommands") {
		t.Errorf("no notice expected:\nstdout %s\nstderr %s", stdout, stderr)
	}
}

func TestRunPrintsTheCommandNoticeBeforeItRuns(t *testing.T) {
	f := writeScenario(t, commandScenario)
	_, _, stderr := run2(t, "run", "-no-live", "-out", t.TempDir(), f)
	if !strings.Contains(stderr, "notice: running this scenario runs these commands") {
		t.Errorf("run did not print the notice:\n%s", stderr)
	}
}

func TestLongCommandsAreCutInTheNotice(t *testing.T) {
	long := strings.Repeat("x", 500)
	lines := quoteShort(long, maxNoticeCommand)
	if len(lines) > maxNoticeCommand+10 || !strings.HasSuffix(lines, `..."`) {
		t.Errorf("not cut: %d chars, ends %q", len(lines), lines[len(lines)-6:])
	}
	if got := quoteShort("short", maxNoticeCommand); got != `"short"` {
		t.Errorf("short command altered: %s", got)
	}
}

func TestLoadToolCommandIsInTheNotice(t *testing.T) {
	body := strings.Replace(plainScenario, "probes:\n", "load:\n  tool: command\n  command: ./gen.sh\n  args: [\"--rps\", \"10\"]\nprobes:\n", 1)
	f := writeScenario(t, body)
	code, _, stderr := run2(t, "validate", f)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, `load: "./gen.sh" with "--rps 10"`) {
		t.Errorf("load command missing:\n%s", stderr)
	}
}
