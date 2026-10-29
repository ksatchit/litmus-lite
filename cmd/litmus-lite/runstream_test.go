package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/litmuschaos/litmus-lite/internal/engine"
	"github.com/litmuschaos/litmus-lite/internal/stream"
)

var _ engine.Emitter = (*stream.Recorder)(nil)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// writeStreamScenario writes a short http.latency scenario against a local server.
// The fault carries a credential-looking parameter that must never be streamed.
func writeStreamScenario(t *testing.T, dir string) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	t.Cleanup(upstream.Close)
	listen := freePort(t)
	yaml := fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: streamed }
target: { kind: http, baseUrl: %s }
baseline: 600ms
recover: 600ms
steadyState:
  - { name: healthz, type: http, url: %s/healthz, expect: { status: 200 } }
faults:
  - name: slow
    kind: http.latency
    duration: 1200ms
    params:
      listen: %q
      upstream: %q
      delay: 30ms
      token: SECRET-TOKEN
probes:
  - { name: list, type: http, url: "http://%s/", interval: 50ms, timeout: 2s, expect: { status: 200 } }
hypotheses:
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
`, upstream.URL, upstream.URL, listen, upstream.URL[len("http://"):], listen)
	path := filepath.Join(dir, "streamed.chaos.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRunStreamFileEndToEnd(t *testing.T) {
	t.Setenv("LITMUS_LITE_STREAM_FILE", "")
	t.Setenv("LITMUS_LITE_RUN_ID", "")
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	out := filepath.Join(dir, "report.json")
	nd := filepath.Join(dir, "run.ndjson")

	code := run([]string{"litmus-lite", "run", "-out", out, "-stream-file", nd, "-run-id", "demo-1", "-label", "service=checkout", "-label", "env=test", scen})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if code := run([]string{"litmus-lite", "stream", "validate", nd}); code != 0 {
		t.Fatalf("validate exit %d", code)
	}
	rec, err := stream.ReadFile(nd)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RunID() != "demo-1" || rec.Events[0].Labels["service"] != "checkout" || rec.Events[0].Labels["env"] != "test" {
		t.Fatalf("envelope %+v", rec.Events[0])
	}
	for _, e := range rec.Events {
		if e.Synthetic {
			t.Fatal("a live run must not be marked synthetic")
		}
	}
	last, ok := rec.Terminal()
	if !ok || last.Type != stream.TypeRunFinished {
		t.Fatalf("last event %+v", last)
	}
	types := map[string]int{}
	for _, e := range rec.Events {
		types[e.Type]++
	}
	for _, want := range []string{"run.started", "fault.started", "fault.ended", "run.finished"} {
		if types[want] != 1 {
			t.Fatalf("%s x%d, types %v", want, types[want], types)
		}
	}
	if types["phase.changed"] != 3 || types["point"] < 2 {
		t.Fatalf("types %v", types)
	}

	// run.finished is report.json plus the diagnosis, minus raw probe samples.
	var fin map[string]any
	if err := json.Unmarshal(last.Data, &fin); err != nil {
		t.Fatal(err)
	}
	rep := readJSON(t, out)
	delete(fin, "diagnosis")
	delete(rep, "probes")
	// report.json keeps the fault parameters as written; the stream masks them.
	for _, f := range rep["faultLog"].([]any) {
		f.(map[string]any)["tunables"].(map[string]any)["token"] = "***"
	}
	for _, k := range keys(rep) {
		if !reflect.DeepEqual(fin[k], rep[k]) {
			t.Errorf("run.finished.%s differs from report.json\n stream: %v\n report: %v", k, fin[k], rep[k])
		}
	}
	if len(fin) != len(rep) {
		t.Errorf("keys differ: stream %v, report %v", keys(fin), keys(rep))
	}

	// The credential is in report.json as written, and never in the stream.
	raw, _ := os.ReadFile(nd)
	if strings.Contains(string(raw), "SECRET-TOKEN") {
		t.Fatal("the stream contains the fault token")
	}
	if !strings.Contains(string(raw), `"token":"***"`) {
		t.Fatal("the token should be masked, not removed")
	}
}

func TestRunWithoutStreamFlagsWritesNoStream(t *testing.T) {
	t.Setenv("LITMUS_LITE_STREAM_FILE", "")
	t.Setenv("LITMUS_LITE_RUN_ID", "")
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	wd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(wd)
	out := filepath.Join(dir, "report.json")
	if code := run([]string{"litmus-lite", "run", "-out", out, scen}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".litmus-lite")); err == nil {
		t.Fatal("a run without a stream flag created .litmus-lite")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ndjson") {
			t.Fatalf("unexpected stream file %s", e.Name())
		}
	}
}

func TestRunStreamFromEnvironmentAndAuto(t *testing.T) {
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	wd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(wd)
	t.Setenv("LITMUS_LITE_STREAM_FILE", "auto")
	t.Setenv("LITMUS_LITE_RUN_ID", "from-env")
	if code := run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), scen}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := stream.ReadFile(filepath.Join(dir, ".litmus-lite", "runs", "from-env.ndjson")); err != nil {
		t.Fatal(err)
	}
}

func TestRunStreamFlagBeatsEnvironment(t *testing.T) {
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	t.Setenv("LITMUS_LITE_STREAM_FILE", filepath.Join(dir, "env.ndjson"))
	t.Setenv("LITMUS_LITE_RUN_ID", "env-id")
	flagFile := filepath.Join(dir, "flag.ndjson")
	if code := run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-file", flagFile, "-run-id", "flag-id", scen}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	rec, err := stream.ReadFile(flagFile)
	if err != nil || rec.RunID() != "flag-id" {
		t.Fatalf("flag file: %v %v", err, rec)
	}
	if _, err := os.Stat(filepath.Join(dir, "env.ndjson")); err == nil {
		t.Fatal("the environment variable won over the flag")
	}
}

func TestRunStreamRejectsABadRunID(t *testing.T) {
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	for _, id := range []string{"../escape", "has space"} {
		if code := run([]string{"litmus-lite", "run", "-stream-file", filepath.Join(dir, "x.ndjson"), "-run-id", id, scen}); code != 2 {
			t.Fatalf("run id %q: exit %d, want 2", id, code)
		}
	}
}

// A sink that cannot be written never changes the run: it finishes, the report
// is written, and the problem is a warning.
func TestRunStreamUnwritableFileDoesNotAffectTheRun(t *testing.T) {
	t.Setenv("LITMUS_LITE_STREAM_FILE", "")
	t.Setenv("LITMUS_LITE_RUN_ID", "")
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	blocker := filepath.Join(dir, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	out := filepath.Join(dir, "report.json")
	code := run([]string{"litmus-lite", "run", "-out", out, "-stream-file", filepath.Join(blocker, "run.ndjson"), scen})
	if code != 0 {
		t.Fatalf("exit %d: an unwritable stream file changed the exit code", code)
	}
	rep := readJSON(t, out)
	// The scenario also has a warning of its own (its "token" param is read
	// by nothing); the stream's is among them.
	w, _ := rep["warnings"].([]any)
	if !strings.Contains(fmt.Sprint(w), "stream:") {
		t.Fatalf("warnings %v", rep["warnings"])
	}
}
