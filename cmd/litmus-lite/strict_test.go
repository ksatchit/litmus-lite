package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/hub"
	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func freeListen(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// A scenario with a typo in a fault param ("dealy"), a probe key ("intervall")
// and a hypothesis key. It is valid, and every one of those did nothing.
func typoScenario(t *testing.T, upstream string) string {
	return writeScenario(t, fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: typos }
baseline: 300ms
recover: 300ms
faults:
  - name: slow
    kind: http.latency
    duration: 300ms
    params: { listen: %q, upstream: %q, dealy: 5ms }
probes:
  - { name: p, type: http, url: "http://%s/", intervall: 50ms, interval: 50ms }
hypotheses:
  - { name: ok, metric: error_rate, operator: "<", value: 50%% }
`, freeListen(t), upstream, upstream))
}

func healthyUpstream(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestValidateWarnsAboutKeysNothingReads(t *testing.T) {
	path := typoScenario(t, healthyUpstream(t))
	code, errText := captureRun(t, "validate", path)
	if code != 0 {
		t.Fatalf("a typo must warn, not fail, by default: %d %s", code, errText)
	}
	for _, want := range []string{"warning:", `"dealy"`, `did you mean "delay"`, "intervall"} {
		if !strings.Contains(errText, want) {
			t.Errorf("missing %q in %q", want, errText)
		}
	}
}

func TestValidateStrictFailsOnThem(t *testing.T) {
	path := typoScenario(t, healthyUpstream(t))
	code, errText := captureRun(t, "validate", "-strict", path)
	if code != 1 || !strings.Contains(errText, "-strict") || !strings.Contains(errText, "dealy") {
		t.Fatalf("code %d err %q", code, errText)
	}
}

func TestRunStrictFailsBeforeInjectingAnything(t *testing.T) {
	path := typoScenario(t, healthyUpstream(t))
	out := filepath.Join(t.TempDir(), "r.json")
	code, errText := captureRun(t, "run", "-strict", "-out", out, path)
	if code != 1 || !strings.Contains(errText, "dealy") {
		t.Fatalf("code %d err %q", code, errText)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a strict refusal must not have run anything")
	}
}

func TestRunShowsTheWarningsAndKeepsThemInTheReport(t *testing.T) {
	path := typoScenario(t, healthyUpstream(t))
	out := filepath.Join(t.TempDir(), "r.json")
	code, errText := captureRun(t, "run", "-out", out, path)
	if code != 0 {
		t.Fatalf("code %d err %q", code, errText)
	}
	if !strings.Contains(errText, `"dealy"`) {
		t.Errorf("the warning was not shown before the run: %q", errText)
	}
	res, err := report.LoadJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), `"dealy"`) {
		t.Errorf("the warning is not in the report: %v", res.Warnings)
	}
	if res.SchemaVersion != report.SchemaVersion {
		t.Errorf("schemaVersion %d", res.SchemaVersion)
	}
}

// What ships must be clean, or -strict would refuse the project's own
// examples and templates.
func TestShippedScenariosHaveNoWarnings(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../examples/*/*.chaos.yaml", "../../docs/stream/fixtures/src/*.chaos.yaml"} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Fatal("found no example scenarios")
	}
	for _, f := range files {
		sc, err := scenario.Load(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if w := lintScenario(sc); len(w) != 0 {
			t.Errorf("%s: %v", f, w)
		}
	}
	for _, it := range hub.List() {
		sc, err := scenario.Parse([]byte(it.Template))
		if err != nil {
			t.Errorf("hub %s: %v", it.ID, err)
			continue
		}
		if w := lintScenario(sc); len(w) != 0 {
			t.Errorf("hub %s: %v", it.ID, w)
		}
	}
}
