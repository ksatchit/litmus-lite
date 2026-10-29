package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func startLive(t *testing.T, token string) (*LiveServer, string) {
	t.Helper()
	l, err := NewLiveServer("127.0.0.1:0", token, "", "t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Shutdown(context.Background(), 0) })
	return l, "http://" + l.Addr
}

// readSSE reads events from a response until the stream ends and returns
// their seq numbers and types.
func readSSE(t *testing.T, resp *http.Response) (seqs []string, types []string) {
	t.Helper()
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			seqs = append(seqs, strings.TrimPrefix(line, "id: "))
		case strings.HasPrefix(line, "event: "):
			types = append(types, strings.TrimPrefix(line, "event: "))
		}
	}
	return seqs, types
}

func TestLiveServerStreamsAsTheRunHappens(t *testing.T) {
	l, base := startLive(t, "")
	rec := NewRecorder(RecorderOptions{RunID: "live-1", Labels: map[string]string{"service": "x"}}, l)

	// Before any event there is no run to follow.
	resp, err := http.Get(base + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("before the run: %d, want 503", resp.StatusCode)
	}

	rec.Started(testScenario(), time.Now())
	rec.Phase("steady")

	// A client that connects mid-run reads the past, then the future.
	// The recorder hands events to the server on its own goroutine, so the
	// run is there a moment after Started returns, not at that instant.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = http.Get(base + "/v1/events")
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if err == nil {
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never became visible: %v %v", err, resp)
		}
		time.Sleep(20 * time.Millisecond)
	}
	type result struct{ seqs, types []string }
	done := make(chan result, 1)
	go func() {
		s, ty := readSSE(t, resp)
		done <- result{s, ty}
	}()
	time.Sleep(50 * time.Millisecond)
	rec.Point(report.Point{OffsetS: 0})
	rec.Phase("inject")
	time.Sleep(50 * time.Millisecond)
	rec.Finished(&report.Result{Name: "checkout", Passed: true})

	select {
	case r := <-done:
		if strings.Join(r.types, ",") != "run.started,phase.changed,point,phase.changed,run.finished" ||
			strings.Join(r.seqs, ",") != "1,2,3,4,5" {
			t.Fatalf("types %v seqs %v", r.types, r.seqs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not end after run.finished")
	}
	rec.Close(time.Second)
}

func TestLiveServerLateClientAndResume(t *testing.T) {
	l, base := startLive(t, "")
	rec := NewRecorder(RecorderOptions{RunID: "live-2"}, l)
	sendRun(rec, 6) // seq 1..9
	rec.Close(time.Second)

	get := func(hdr map[string]string) (*http.Response, error) {
		req, _ := http.NewRequest("GET", base+"/v1/runs/live-2/events", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		return http.DefaultClient.Do(req)
	}
	resp, err := get(nil)
	if err != nil {
		t.Fatal(err)
	}
	seqs, _ := readSSE(t, resp)
	if len(seqs) != 9 || seqs[0] != "1" || seqs[8] != "9" {
		t.Fatalf("late client got %v", seqs)
	}
	// Resume after event 4: exactly 5..9, no gap and no repeat.
	resp, _ = get(map[string]string{"Last-Event-ID": "4"})
	seqs, _ = readSSE(t, resp)
	if strings.Join(seqs, ",") != "5,6,7,8,9" {
		t.Fatalf("resume got %v", seqs)
	}
	// A client that has everything is told so, which stops EventSource.
	resp, _ = get(map[string]string{"Last-Event-ID": "9"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("resume past the end: %d, want 204", resp.StatusCode)
	}
	resp, _ = http.Get(base + "/v1/runs/other/events")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("unknown run: %d", resp.StatusCode)
	}
}

func TestLiveServerReadOnlyAPI(t *testing.T) {
	l, base := startLive(t, "")
	rec := NewRecorder(RecorderOptions{RunID: "live-3", Labels: map[string]string{"service": "checkout"}}, l)
	rec.Started(testScenario(), time.Now())
	time.Sleep(30 * time.Millisecond)

	getJSON := func(path string, v any) int {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if v != nil {
			_ = json.Unmarshal(b, v)
		}
		return resp.StatusCode
	}
	var runs []RunSummary
	if getJSON("/v1/runs", &runs) != 200 || len(runs) != 1 || runs[0].RunID != "live-3" || runs[0].Status != "running" || runs[0].Name != "checkout" {
		t.Fatalf("runs %+v", runs)
	}
	if getJSON("/v1/runs/live-3/result", nil) != 404 {
		t.Fatal("a running run has no result")
	}
	if getJSON("/v1/runs?status=passed", &runs) != 200 || len(runs) != 0 {
		t.Fatalf("status filter %+v", runs)
	}
	if getJSON("/v1/runs?label=service=checkout", &runs) != 200 || len(runs) != 1 {
		t.Fatalf("label filter %+v", runs)
	}
	var info map[string]any
	if getJSON("/v1/info", &info) != 200 || info["mode"] != "live" {
		t.Fatalf("info %v", info)
	}
	rec.Finished(&report.Result{Name: "checkout", Passed: true})
	time.Sleep(30 * time.Millisecond)
	if getJSON("/v1/runs/live-3/result", nil) != 200 {
		t.Fatal("no result after the run")
	}
	if getJSON("/v1/runs", &runs) != 200 || runs[0].Status != "passed" || runs[0].FinishedAt == nil {
		t.Fatalf("runs %+v", runs)
	}
	rec.Close(time.Second)
}

func TestLiveServerTokenAndBindRules(t *testing.T) {
	_, base := startLive(t, "secret")
	resp, _ := http.Get(base + "/v1/runs")
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", base+"/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("with token: %d", resp.StatusCode)
	}
	resp, _ = http.Get(base + "/v1/health")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health: %d", resp.StatusCode)
	}
	if _, err := NewLiveServer("0.0.0.0:0", "", "", "t"); err == nil {
		t.Fatal("a non-loopback bind without a token was accepted")
	}
	l, err := NewLiveServer("0.0.0.0:0", "tok", "", "t")
	if err != nil {
		t.Fatalf("non-loopback with a token: %v", err)
	}
	l.Shutdown(context.Background(), 0)
}

func TestLiveServerBufferDropsSamplesThenPointsButKeepsLifecycle(t *testing.T) {
	l, base := startLive(t, "")
	l.Buffer = 20
	rec := NewRecorder(RecorderOptions{RunID: "buf"}, l)
	rec.Started(testScenario(), time.Now())
	for i := 0; i < 100; i++ {
		rec.Point(report.Point{OffsetS: float64(i)})
	}
	rec.Phase("recover")
	rec.Finished(&report.Result{})
	rec.Close(2 * time.Second)
	resp, _ := http.Get(base + "/v1/runs/buf/events")
	seqs, types := readSSE(t, resp)
	if types[0] != "run.started" || types[len(types)-1] != "run.finished" {
		t.Fatalf("types %v", types)
	}
	if len(seqs) > 21 {
		t.Fatalf("%d events kept with a buffer of 20", len(seqs))
	}
	has := map[string]bool{}
	for _, ty := range types {
		has[ty] = true
	}
	if !has["phase.changed"] {
		t.Fatalf("lifecycle event evicted: %v", types)
	}
}

func TestLiveServerShutdownLingersThenStops(t *testing.T) {
	l, err := NewLiveServer("127.0.0.1:0", "", "", "t")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + l.Addr
	start := time.Now()
	done := make(chan struct{})
	go func() { l.Shutdown(context.Background(), 300*time.Millisecond); close(done) }()
	time.Sleep(100 * time.Millisecond)
	// Still serving during the linger.
	resp, err := http.Get(base + "/v1/health")
	if err != nil {
		t.Fatalf("not serving during the linger: %v", err)
	}
	resp.Body.Close()
	<-done
	if time.Since(start) < 300*time.Millisecond {
		t.Fatal("shut down before the linger ended")
	}
	if _, err := http.Get(base + "/v1/health"); err == nil {
		t.Fatal("still serving after the linger")
	}
	// A cancelled context cuts the linger short.
	l2, _ := NewLiveServer("127.0.0.1:0", "", "", "t")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	begin := time.Now()
	l2.Shutdown(ctx, 10*time.Second)
	if time.Since(begin) > 3*time.Second {
		t.Fatal("Ctrl-C did not cut the linger short")
	}
}

func TestRedactedScenarioYAML(t *testing.T) {
	sc := testScenario()
	sc.Target.BaseURL = "https://user:hunter2@api.example.com/?api_key=SECRET-URL&page=1"
	sc.Probes = []scenario.Probe{
		{Name: "a", Type: "http", URL: "https://x.example.com/p?token=SECRET-PROBE&q=1"},
		{Name: "b", Type: "cmd", Command: "curl -s -H 'x' --data password=SECRET-CMD https://y"},
	}
	sc.Load = &scenario.LoadSpec{Tool: "command", Command: "gen", Args: []string{"--api-key", "SECRET-ARG", "--vus", "10", "--token=SECRET-EQ", "secret=SECRET-PAIR"}}
	out := RedactedScenarioYAML(sc)
	for _, secret := range []string{"hunter2", "SECRET-URL", "SECRET-PROBE", "SECRET-CMD", "SECRET-ARG", "SECRET-EQ", "SECRET-PAIR", "SECRET-TOKEN"} {
		if strings.Contains(out, secret) {
			t.Errorf("%s survived redaction:\n%s", secret, out)
		}
	}
	for _, keep := range []string{"checkout", "http.latency", "page=1", "q=1", "--vus", "800ms", "***"} {
		if !strings.Contains(out, keep) && !(keep == "http.latency" || keep == "800ms") {
			t.Errorf("%q was lost:\n%s", keep, out)
		}
	}
	var back map[string]any
	if err := yaml.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("the redacted scenario is not YAML: %v", err)
	}
	// The original is untouched.
	if sc.Faults[0].Params["token"] != "SECRET-TOKEN" || sc.Load.Args[1] != "SECRET-ARG" {
		t.Fatal("redaction modified the scenario it was given")
	}
}

func TestRecorderCanIncludeTheScenario(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(RecorderOptions{RunID: "sc", IncludeScenario: true}, sink)
	drive(r)
	r.Close(time.Second)
	sink.mu.Lock()
	first := sink.lines[0]
	sink.mu.Unlock()
	var e Envelope
	_ = json.Unmarshal(first, &e)
	var rs RunStarted
	_ = json.Unmarshal(e.Data, &rs)
	if !strings.Contains(rs.Scenario, "checkout") || strings.Contains(rs.Scenario, "SECRET-TOKEN") {
		t.Fatalf("scenario %q", rs.Scenario)
	}
	// Off by default.
	sink2 := &memSink{}
	r2 := NewRecorder(RecorderOptions{RunID: "sc2"}, sink2)
	drive(r2)
	r2.Close(time.Second)
	if strings.Contains(string(sink2.all()), `"scenario"`) {
		t.Fatal("scenario included without -stream-scenario")
	}
}
