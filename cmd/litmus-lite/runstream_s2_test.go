package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/stream"
)

func clearStreamEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"LITMUS_LITE_STREAM_FILE", "LITMUS_LITE_STREAM_PUSH", "LITMUS_LITE_STREAM_PULL", "LITMUS_LITE_STREAM_TOKEN", "LITMUS_LITE_RUN_ID"} {
		t.Setenv(k, "")
	}
}

// captureStderr runs f and returns what it wrote to stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	out := make(chan string)
	go func() { b, _ := io.ReadAll(r); out <- string(b) }()
	f()
	w.Close()
	os.Stderr = old
	return <-out
}

func TestRunStreamPushEndToEnd(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	rcv, err := stream.NewReceiver(filepath.Join(dir, "rx"))
	if err != nil {
		t.Fatal(err)
	}
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		rcv.Token = "tok-123"
		rcv.Handler().ServeHTTP(w, r)
	}))
	defer srv.Close()
	t.Setenv("LITMUS_LITE_STREAM_TOKEN", "tok-123")

	out := filepath.Join(dir, "report.json")
	code := run([]string{"litmus-lite", "run", "-out", out, "-stream-push", srv.URL + "/v1/ingest", "-run-id", "pushed-1", "-label", "service=checkout", scen})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if auth.Load() != "Bearer tok-123" {
		t.Fatalf("authorization %v", auth.Load())
	}
	got := filepath.Join(dir, "rx", "pushed-1.ndjson")
	if c := run([]string{"litmus-lite", "stream", "validate", got}); c != 0 {
		t.Fatalf("what the receiver stored does not validate: %d", c)
	}
	rec, _ := stream.ReadFile(got)
	if last, ok := rec.Terminal(); !ok || last.Type != stream.TypeRunFinished {
		t.Fatalf("no run.finished at the receiver: %+v", last)
	}
	// The token and the secret fault param are in neither the report nor the stream.
	raw, _ := os.ReadFile(got)
	rep, _ := os.ReadFile(out)
	for name, b := range map[string][]byte{"stored stream": raw, "report": rep} {
		if strings.Contains(string(b), "tok-123") {
			t.Errorf("the push token is in the %s", name)
		}
	}
	if strings.Contains(string(raw), "SECRET-TOKEN") {
		t.Error("the fault token is in the stored stream")
	}
}

func TestRunStreamPushRefusesPlainHTTPToARemoteHost(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	var stderr string
	var code int
	stderr = captureStderr(t, func() {
		code = run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-push", "http://collector.example.com/v1/ingest", scen})
	})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "https") {
		t.Fatalf("stderr %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "r.json")); err == nil {
		t.Fatal("the run went ahead after a refused destination")
	}
}

func TestRunStreamPullRefusesANonLoopbackBindWithoutAToken(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	_ = captureStderr(t, func() {
		if code := run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-pull", "0.0.0.0:0", scen}); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
	})
}

// A destination that is down changes nothing about the run.
func TestRunStreamDeadPushServerLeavesTheRunAlone(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	out := filepath.Join(dir, "report.json")
	start := time.Now()
	var code int
	stderr := captureStderr(t, func() {
		code = run([]string{"litmus-lite", "run", "-out", out, "-stream-push", url + "/v1/ingest", scen})
	})
	if code != 0 {
		t.Fatalf("exit %d: a dead destination changed the exit code", code)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatalf("the dead destination held the run for %v", time.Since(start))
	}
	rep := readJSON(t, out)
	if rep["passed"] != true {
		t.Fatalf("passed %v", rep["passed"])
	}
	w, _ := json.Marshal(rep["warnings"])
	if !strings.Contains(string(w), "stream:") {
		t.Fatalf("no stream warning in the report: %s", w)
	}
	if !strings.Contains(stderr, "stream:") {
		t.Fatalf("no stream warning on stderr: %q", stderr)
	}
}

func TestRunStreamPullServesTheRun(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	addr := freePort(t)
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-pull", addr, "-stream-pull-linger", "3s", "-run-id", "pulled-1", scen})
	}()
	var resp *http.Response
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/v1/runs/pulled-1/events")
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || resp == nil || resp.StatusCode != 200 {
		t.Fatalf("could not connect: %v %v", err, resp)
	}
	defer resp.Body.Close()
	var types []string
	var lastID string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "event: ") {
			types = append(types, strings.TrimPrefix(l, "event: "))
		}
		if strings.HasPrefix(l, "id: ") {
			lastID = strings.TrimPrefix(l, "id: ")
		}
	}
	if len(types) < 5 || types[0] != "run.started" || types[len(types)-1] != "run.finished" {
		t.Fatalf("types %v", types)
	}
	// During the linger a client can still ask for the result, and one that
	// reconnects with everything gets 204.
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/runs/pulled-1/events", nil)
	req.Header.Set("Last-Event-ID", lastID)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("not serving during the linger: %v", err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("resume after the end: %d", r2.StatusCode)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("run did not return after the linger")
	}
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("still listening after the run returned")
	}
}

func TestRunStreamAuditNamesTheDestinationsOnly(t *testing.T) {
	clearStreamEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	rcv, _ := stream.NewReceiver(filepath.Join(dir, "rx"))
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	t.Setenv("LITMUS_LITE_STREAM_TOKEN", "tok-audit")
	if code := run([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-push", srv.URL + "/v1/ingest?key=QUERYSECRET", "-stream-file", filepath.Join(dir, "f.ndjson"), scen}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	b, err := os.ReadFile(filepath.Join(home, ".litmus-lite", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"stream":`) || !strings.Contains(s, "push:127.0.0.1") {
		t.Fatalf("audit %s", s)
	}
	for _, leak := range []string{"tok-audit", "QUERYSECRET", "/v1/ingest"} {
		if strings.Contains(s, leak) {
			t.Fatalf("the audit log contains %q: %s", leak, s)
		}
	}
}

func TestRunStreamScenarioIsIncludedOnlyWhenAsked(t *testing.T) {
	clearStreamEnv(t)
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	read := func(extra ...string) string {
		nd := filepath.Join(dir, "s.ndjson")
		args := append([]string{"litmus-lite", "run", "-out", filepath.Join(dir, "r.json"), "-stream-file", nd}, extra...)
		if code := run(append(args, scen)); code != 0 {
			t.Fatalf("exit %d", code)
		}
		b, _ := os.ReadFile(nd)
		return string(b)
	}
	if strings.Contains(read(), `"scenario"`) {
		t.Fatal("the scenario was streamed without -stream-scenario")
	}
	with := read("-stream-scenario")
	if !strings.Contains(with, `"scenario"`) || strings.Contains(with, "SECRET-TOKEN") {
		t.Fatal("-stream-scenario missing, or it leaked the fault token")
	}
}

func TestStreamReceiveFlagValidation(t *testing.T) {
	clearStreamEnv(t)
	_ = captureStderr(t, func() {
		if c := run([]string{"litmus-lite", "stream", "receive", "-fail-percent", "101"}); c != 2 {
			t.Errorf("fail-percent 101: exit %d", c)
		}
		if c := run([]string{"litmus-lite", "stream", "receive", "-listen", "0.0.0.0:0", "-dir", t.TempDir()}); c != 2 {
			t.Errorf("non-loopback without a token: exit %d", c)
		}
	})
}

func TestDoctorStream(t *testing.T) {
	clearStreamEnv(t)
	rcv, _ := stream.NewReceiver(t.TempDir())
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	if c := run([]string{"litmus-lite", "doctor", "-stream", srv.URL + "/v1/ingest"}); c != 0 {
		t.Errorf("a working receiver: exit %d", c)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer bad.Close()
	_ = captureStderr(t, func() {
		if c := run([]string{"litmus-lite", "doctor", "-stream", bad.URL + "/v1/ingest"}); c == 0 {
			t.Error("a receiver that rejects us passed doctor")
		}
		if c := run([]string{"litmus-lite", "doctor", "-stream", "http://collector.example.com/x"}); c == 0 {
			t.Error("plain http to a remote host passed doctor")
		}
	})
	if c := run([]string{"litmus-lite", "doctor", "-stream", "127.0.0.1:0"}); c != 0 {
		t.Errorf("a free pull address: exit %d", c)
	}
}

var _ = context.Background
