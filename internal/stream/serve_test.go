package stream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/report"
)

func newDirServer(t *testing.T, dir string) (*DirServer, string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var warns []string
	s := &DirServer{Dir: dir, Version: "t", Poll: 15 * time.Millisecond, Stale: time.Minute,
		Warn: func(m string) { mu.Lock(); warns = append(warns, m); mu.Unlock() }}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv.URL, &warns
}

// writeRun writes a complete run through the same path a real run uses.
func writeRun(t *testing.T, path, id string, labels map[string]string, points int) {
	t.Helper()
	fs, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecorder(RecorderOptions{RunID: id, Labels: labels}, fs)
	sendRun(rec, points)
	rec.Close(2 * time.Second)
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
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

func TestServeIndexResultAndReplayOfFinishedRuns(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, filepath.Join(dir, "a.ndjson"), "run-a", map[string]string{"service": "checkout"}, 4)
	time.Sleep(10 * time.Millisecond)
	writeRun(t, filepath.Join(dir, "b.ndjson"), "run-b", map[string]string{"service": "search"}, 2)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a run"), 0o644)
	_, base, warns := newDirServer(t, dir)

	var runs []RunSummary
	if getJSON(t, base+"/v1/runs", &runs) != 200 || len(runs) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	if runs[0].RunID != "run-b" || runs[0].Status != "passed" || runs[0].Events != 5 || runs[1].Events != 7 {
		t.Fatalf("index %+v", runs)
	}
	if getJSON(t, base+"/v1/runs?label=service=checkout", &runs) != 200 || len(runs) != 1 || runs[0].RunID != "run-a" {
		t.Fatalf("label filter %+v", runs)
	}
	if getJSON(t, base+"/v1/runs?status=running", &runs) != 200 || len(runs) != 0 {
		t.Fatalf("status filter %+v", runs)
	}
	var res map[string]any
	if getJSON(t, base+"/v1/runs/run-a/result", &res) != 200 || res["name"] != "checkout" {
		t.Fatalf("result %v", res)
	}
	var info map[string]any
	if getJSON(t, base+"/v1/info", &info) != 200 || info["mode"] != "serve" {
		t.Fatalf("info %v", info)
	}

	resp, _ := http.Get(base + "/v1/runs/run-a/events")
	seqs, types := readSSE(t, resp)
	if len(seqs) != 7 || seqs[0] != "1" || seqs[6] != "7" || types[6] != "run.finished" {
		t.Fatalf("replay %v %v", seqs, types)
	}
	req, _ := http.NewRequest("GET", base+"/v1/runs/run-a/events", nil)
	req.Header.Set("Last-Event-ID", "5")
	resp, _ = http.DefaultClient.Do(req)
	seqs, _ = readSSE(t, resp)
	if strings.Join(seqs, ",") != "6,7" {
		t.Fatalf("resume %v", seqs)
	}
	req.Header.Set("Last-Event-ID", "7")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("past the end: %d", resp.StatusCode)
	}
	if getJSON(t, base+"/v1/runs/nope/events", nil) != 404 || getJSON(t, base+"/v1/runs/..%2Fa/events", nil) != 404 {
		t.Fatal("unknown or hostile run id was served")
	}
	if len(*warns) != 0 {
		t.Fatalf("warnings %v", *warns)
	}
}

// The run is written by one process and watched by another that it knows
// nothing about.
func TestServeFollowsARunInProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "live.ndjson")
	_, base, _ := newDirServer(t, dir)
	if getJSON(t, base+"/v1/runs", nil) != 200 {
		t.Fatal("no server before any run")
	}

	fs, _ := NewFileSink(path)
	rec := NewRecorder(RecorderOptions{RunID: "live"}, fs)
	rec.Started(testScenario(), time.Now())
	rec.Phase("steady")
	rec.Point(report.Point{OffsetS: 0})
	time.Sleep(60 * time.Millisecond)

	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "running" || runs[0].Events != 3 || runs[0].FinishedAt != nil {
		t.Fatalf("running index %+v", runs)
	}
	if getJSON(t, base+"/v1/runs/live/result", nil) != 404 {
		t.Fatal("a running run has a result")
	}

	resp, err := http.Get(base + "/v1/runs/live/events")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp)
	}
	type out struct{ seqs, types []string }
	done := make(chan out, 1)
	go func() { s, ty := readSSE(t, resp); done <- out{s, ty} }()
	time.Sleep(60 * time.Millisecond)
	rec.Point(report.Point{OffsetS: 1})
	rec.Phase("inject")
	time.Sleep(60 * time.Millisecond)
	rec.Finished(&report.Result{Name: "checkout", Passed: true})
	rec.Close(time.Second)

	select {
	case o := <-done:
		if strings.Join(o.seqs, ",") != "1,2,3,4,5,6" || o.types[5] != "run.finished" {
			t.Fatalf("followed %v %v", o.seqs, o.types)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not end with the run")
	}
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "passed" {
		t.Fatalf("after the run %+v", runs)
	}
	if getJSON(t, base+"/v1/runs/live/result", nil) != 200 {
		t.Fatal("no result after the run")
	}
}

func TestServeNeverServesAPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "torn.ndjson")
	writeRun(t, filepath.Join(dir, "whole.ndjson"), "whole", nil, 3)
	whole, _ := os.ReadFile(filepath.Join(dir, "whole.ndjson"))
	lines := strings.SplitAfter(strings.TrimRight(string(whole), "\n"), "\n")
	rewrite := func(id string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.Replace(l, `"runId":"whole"`, `"runId":"`+id+`"`, 1)
		}
		return out
	}
	torn := rewrite("torn")
	_ = os.Remove(filepath.Join(dir, "whole.ndjson"))

	// First line complete, second line half-written.
	half := torn[1][:len(torn[1])/2]
	if err := os.WriteFile(path, []byte(torn[0]+"\n"+half), 0o644); err != nil {
		t.Fatal(err)
	}
	_, base, warns := newDirServer(t, dir)
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Events != 1 {
		t.Fatalf("a half line was counted: %+v", runs)
	}
	resp, _ := http.Get(base + "/v1/runs/torn/events")
	got := make(chan []string, 1)
	go func() { s, _ := readSSE(t, resp); got <- s }()
	time.Sleep(80 * time.Millisecond)

	// The writer finishes the line and the rest.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(torn[1][len(half):] + "\n")
	for _, l := range torn[2:] {
		_, _ = f.WriteString(strings.TrimRight(l, "\n") + "\n")
	}
	f.Close()
	select {
	case s := <-got:
		if len(s) != len(torn) || s[0] != "1" {
			t.Fatalf("seqs %v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the follower did not finish")
	}
	if len(*warns) != 0 {
		t.Fatalf("a half-written line produced warnings: %v", *warns)
	}
}

func TestServeSkipsBadFilesOnceAndServesTheRest(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, filepath.Join(dir, "good.ndjson"), "good", nil, 1)
	_ = os.WriteFile(filepath.Join(dir, "garbage.ndjson"), []byte("this is not json\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "wrongschema.ndjson"), []byte(`{"schema":"other/v9","runId":"x","seq":1,"type":"run.started","data":{}}`+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "empty.ndjson"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "../outside.ndjson"), []byte("x"), 0o644)
	_, base, warns := newDirServer(t, dir)
	var runs []RunSummary
	for i := 0; i < 3; i++ {
		getJSON(t, base+"/v1/runs", &runs)
	}
	if len(runs) != 1 || runs[0].RunID != "good" {
		t.Fatalf("runs %+v", runs)
	}
	if len(*warns) != 2 {
		t.Fatalf("want one warning per bad file, got %v", *warns)
	}
}

func TestServeDuplicateRunIDServesTheNewerFile(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, filepath.Join(dir, "old.ndjson"), "same", nil, 1)
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "old.ndjson"), old, old)
	writeRun(t, filepath.Join(dir, "new.ndjson"), "same", nil, 5)
	_, base, warns := newDirServer(t, dir)
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Events != 8 {
		t.Fatalf("%+v", runs)
	}
	if len(*warns) != 1 || !strings.Contains((*warns)[0], "same run id") {
		t.Fatalf("warnings %v", *warns)
	}
}

// A crashed run never writes its last event. It must not stay "running"
// forever or hold a client open.
func TestServeReportsAnAbandonedRunAsIncomplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crashed.ndjson")
	fs, _ := NewFileSink(path)
	// The "dead" process never closes its file; the test must, or Windows
	// cannot remove the folder.
	t.Cleanup(func() { fs.Close() })
	rec := NewRecorder(RecorderOptions{RunID: "crashed"}, fs)
	rec.Started(testScenario(), time.Now())
	rec.Phase("steady")
	rec.Point(report.Point{})
	time.Sleep(50 * time.Millisecond) // written, and then the process "dies": no Finished

	s, base, _ := newDirServer(t, dir)
	s.Stale = 300 * time.Millisecond
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "running" || runs[0].Incomplete {
		t.Fatalf("a fresh run %+v", runs)
	}
	resp, _ := http.Get(base + "/v1/runs/crashed/events")
	done := make(chan []string, 1)
	go func() { seqs, _ := readSSE(t, resp); done <- seqs }()
	select {
	case seqs := <-done:
		if len(seqs) != 3 {
			t.Fatalf("seqs %v", seqs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client of a dead run was never released")
	}
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "errored" || !runs[0].Incomplete || runs[0].FinishedAt != nil {
		t.Fatalf("abandoned run %+v", runs)
	}
	req, _ := http.NewRequest("GET", base+"/v1/runs/crashed/events", nil)
	req.Header.Set("Last-Event-ID", "3")
	r2, _ := http.DefaultClient.Do(req)
	r2.Body.Close()
	if r2.StatusCode != 204 {
		t.Fatalf("resume of an abandoned run: %d", r2.StatusCode)
	}
	rec.Close(time.Second)
}

func TestServeReleasesAClientWhenTheFileIsReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.ndjson")
	fs, _ := NewFileSink(path)
	rec := NewRecorder(RecorderOptions{RunID: "first"}, fs)
	rec.Started(testScenario(), time.Now())
	rec.Phase("steady")
	time.Sleep(40 * time.Millisecond)
	_, base, _ := newDirServer(t, dir)
	resp, _ := http.Get(base + "/v1/runs/first/events")
	done := make(chan struct{})
	go func() { readSSE(t, resp); close(done) }()
	time.Sleep(60 * time.Millisecond)
	// A new run with the same -stream-file truncates it.
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the client of a replaced file was not released")
	}
	rec.Close(time.Second)
}

func TestServeIgnoresSymlinksAndCapsClients(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	writeRun(t, filepath.Join(outside, "secret.ndjson"), "linked", nil, 1)
	if err := os.Symlink(filepath.Join(outside, "secret.ndjson"), filepath.Join(dir, "link.ndjson")); err != nil {
		t.Skip("no symlinks here")
	}
	path := filepath.Join(dir, "open.ndjson")
	fs, _ := NewFileSink(path)
	rec := NewRecorder(RecorderOptions{RunID: "open"}, fs)
	rec.Started(testScenario(), time.Now())
	time.Sleep(40 * time.Millisecond)
	s, base, _ := newDirServer(t, dir)
	s.MaxClients = 2
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].RunID != "open" {
		t.Fatalf("a symlink was followed: %+v", runs)
	}
	var open []*http.Response
	for i := 0; i < 2; i++ {
		r, err := http.Get(base + "/v1/runs/open/events")
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("client %d: %v", i, err)
		}
		open = append(open, r)
	}
	r3, _ := http.Get(base + "/v1/runs/open/events")
	r3.Body.Close()
	if r3.StatusCode != 503 || r3.Header.Get("Retry-After") == "" {
		t.Fatalf("third client: %d", r3.StatusCode)
	}
	for _, r := range open {
		r.Body.Close()
	}
	rec.Close(time.Second)
}

func TestServeTokenAndMissingDirectory(t *testing.T) {
	s := &DirServer{Dir: filepath.Join(t.TempDir(), "not-yet"), Token: "tok", Version: "t"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if getJSON(t, srv.URL+"/v1/runs", nil) != 401 {
		t.Fatal("no token was accepted")
	}
	if getJSON(t, srv.URL+"/v1/health", nil) != 200 {
		t.Fatal("health needs no token")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("a missing directory: %d %s", resp.StatusCode, b)
	}
	// The directory appears later and its runs show up without a restart.
	writeRun(t, filepath.Join(s.Dir, "late.ndjson"), "late", nil, 1)
	req, _ = http.NewRequest("GET", srv.URL+"/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, _ = http.DefaultClient.Do(req)
	var runs []RunSummary
	_ = json.NewDecoder(resp.Body).Decode(&runs)
	resp.Body.Close()
	if len(runs) != 1 || runs[0].RunID != "late" {
		t.Fatalf("runs %+v", runs)
	}
}

// The fixtures are real recordings: serve must give the same answers for them
// as stream replay does, which the consumers were built against.
func TestServeAgreesWithReplayOnTheFixtures(t *testing.T) {
	fixtures := "../../docs/stream/fixtures"
	_, base, warns := newDirServer(t, fixtures)
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	entries, _ := filepath.Glob(filepath.Join(fixtures, "*.ndjson"))
	if len(runs) != len(entries) || len(runs) == 0 {
		t.Fatalf("%d runs for %d files", len(runs), len(entries))
	}
	for _, e := range entries {
		rec, err := ReadFile(e)
		if err != nil {
			t.Fatal(err)
		}
		want := Summary(rec)
		var got RunSummary
		for _, r := range runs {
			if r.RunID == want.RunID {
				got = r
			}
		}
		if got.Status != want.Status || got.Events != want.Events || got.Name != want.Name {
			t.Errorf("%s: serve %+v, replay %+v", e, got, want)
		}
		resp, _ := http.Get(base + "/v1/runs/" + want.RunID + "/events")
		seqs, _ := readSSE(t, resp)
		if len(seqs) != len(rec.Events) {
			t.Errorf("%s: %d events, want %d", e, len(seqs), len(rec.Events))
		}
	}
	if len(*warns) != 0 {
		t.Fatalf("warnings %v", *warns)
	}
}

func TestPushReplayLandsAsAFileThatServeReads(t *testing.T) {
	rx := t.TempDir()
	rcv, _ := NewReceiver(rx)
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	rec, err := ReadFile("../../docs/stream/fixtures/ignite-load.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := ValidatePushURL(srv.URL+"/v1/ingest", false)
	sink := NewPushSink(PushOptions{URL: u, MaxDelay: 20 * time.Millisecond, BackoffMin: 5 * time.Millisecond})
	p := NewPusher(sink)
	ids, err := ReplayPush(context.Background(), []*Recording{rec}, p, ReplayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stuck := p.Close(5 * time.Second); len(stuck) != 0 {
		t.Fatalf("stuck %v", stuck)
	}
	if d, l := sink.Delivered(); d != len(rec.Events) || l != 0 {
		t.Fatalf("delivered %d lost %d of %d", d, l, len(rec.Events))
	}
	want, _ := os.ReadFile("../../docs/stream/fixtures/ignite-load.ndjson")
	got, err := os.ReadFile(filepath.Join(rx, ids[0]+".ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("what the receiver stored is not what was recorded, byte for byte")
	}
	// ... and serve reads the receiver's directory.
	_, base, warns := newDirServer(t, rx)
	var runs []RunSummary
	getJSON(t, base+"/v1/runs", &runs)
	if len(runs) != 1 || runs[0].RunID != rec.RunID() || runs[0].Status != "passed" || runs[0].Events != len(rec.Events) {
		t.Fatalf("serve over the receiver's directory: %+v", runs)
	}
	var res map[string]any
	if getJSON(t, base+"/v1/runs/"+rec.RunID()+"/result", &res) != 200 {
		t.Fatal("no result")
	}
	if len(*warns) != 0 {
		t.Fatal(*warns)
	}
}

func TestPushReplayRunIDs(t *testing.T) {
	rec, err := ReadFile("../../docs/stream/fixtures/cashier-charge.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	push := func(opt ReplayOptions, rcv *Receiver) []string {
		srv := httptest.NewServer(rcv.Handler())
		defer srv.Close()
		u, _ := ValidatePushURL(srv.URL+"/v1/ingest", false)
		sink := NewPushSink(PushOptions{URL: u, MaxDelay: 10 * time.Millisecond})
		p := NewPusher(sink)
		ids, err := ReplayPush(context.Background(), []*Recording{rec}, p, opt)
		if err != nil {
			t.Fatal(err)
		}
		p.Close(5 * time.Second)
		return ids
	}
	dir := t.TempDir()
	rcv, _ := NewReceiver(dir)

	// Same id twice: the receiver stores it once.
	a := push(ReplayOptions{}, rcv)
	b := push(ReplayOptions{}, rcv)
	if a[0] != b[0] {
		t.Fatal("ids differ without -fresh")
	}
	if n := countLines(t, filepath.Join(dir, a[0]+".ndjson")); n != len(rec.Events) {
		t.Fatalf("%d lines after pushing twice, want %d", n, len(rec.Events))
	}
	// Fresh ids: stored again, as a different run.
	f := push(ReplayOptions{Fresh: true}, rcv)
	if f[0] == a[0] || ValidRunID(f[0]) != nil {
		t.Fatalf("fresh id %q", f[0])
	}
	// A chosen id changes the run id and nothing else.
	c := push(ReplayOptions{RunID: "chosen-1"}, rcv)
	if c[0] != "chosen-1" {
		t.Fatal(c)
	}
	moved, err := ReadFile(filepath.Join(dir, "chosen-1.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	for i, ev := range moved.Events {
		orig := rec.Events[i]
		if ev.RunID != "chosen-1" || ev.Seq != orig.Seq || !ev.TS.Equal(orig.TS) || ev.Type != orig.Type || string(ev.Data) != string(orig.Data) {
			t.Fatalf("event %d changed beyond the run id", i)
		}
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func TestPushReplayPacingAndCancel(t *testing.T) {
	rec, err := ReadFile("../../docs/stream/fixtures/ignite-load.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	span := rec.Events[len(rec.Events)-1].TS.Sub(rec.Events[0].TS)
	if span < 5*time.Second {
		t.Skipf("fixture too short (%v) to time", span)
	}
	// At speed 0 everything is sent at once.
	start := time.Now()
	p := NewPusher(&memSink{})
	if _, err := ReplayPush(context.Background(), []*Recording{rec}, p, ReplayOptions{}); err != nil {
		t.Fatal(err)
	}
	p.Close(time.Second)
	if time.Since(start) > time.Second {
		t.Fatalf("speed 0 took %v", time.Since(start))
	}
	// MaxGap bounds the pauses: speed 1 with a 1ms cap is quick too.
	start = time.Now()
	p = NewPusher(&memSink{})
	if _, err := ReplayPush(context.Background(), []*Recording{rec}, p, ReplayOptions{Speed: 1, MaxGap: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	p.Close(time.Second)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("max-gap took %v", time.Since(start))
	}
	// Real pace is honoured and Ctrl-C stops it.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	sink := &memSink{}
	p = NewPusher(sink)
	start = time.Now()
	_, err = ReplayPush(ctx, []*Recording{rec}, p, ReplayOptions{Speed: 1})
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("cancel: %v after %v", err, time.Since(start))
	}
	p.Close(time.Second)
	sink.mu.Lock()
	n := len(sink.lines)
	sink.mu.Unlock()
	if n == 0 || n >= len(rec.Events) {
		t.Fatalf("%d of %d events sent before the cancel", n, len(rec.Events))
	}
}
