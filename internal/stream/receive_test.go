package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evLine(run string, seq int64, typ string) string {
	data := `{"x":1}`
	switch typ {
	case TypeRunStarted:
		data = `{"name":"n","faults":[],"hypotheses":[]}`
	case TypeRunFinished:
		data = `{"name":"n","passed":true}`
	}
	return fmt.Sprintf(`{"schema":%q,"runId":%q,"seq":%d,"ts":"2026-10-14T09:00:%02dZ","offsetS":%d,"type":%q,"data":%s}`,
		Schema, run, seq, seq%60, seq, typ, data)
}

func batchOf(run string, from, to int64) string {
	var lines []string
	for s := from; s <= to; s++ {
		typ := TypePoint
		if s == 1 {
			typ = TypeRunStarted
		}
		lines = append(lines, evLine(run, s, typ))
	}
	return strings.Join(lines, "\n") + "\n"
}

func post(t *testing.T, url, token, body string, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/ingest", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

func newReceiver(t *testing.T) (*Receiver, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := NewReceiver(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r.Handler())
	t.Cleanup(srv.Close)
	return r, srv, dir
}

func TestReceiverStoresAndDeduplicates(t *testing.T) {
	_, srv, dir := newReceiver(t)
	code, out := post(t, srv.URL, "", batchOf("r1", 1, 5), nil)
	if code != 200 || out["accepted"] != float64(5) || out["duplicates"] != float64(0) {
		t.Fatalf("first batch: %d %v", code, out)
	}
	// A retried batch, and one that overlaps it.
	code, out = post(t, srv.URL, "", batchOf("r1", 1, 5), nil)
	if code != 200 || out["accepted"] != float64(0) || out["duplicates"] != float64(5) {
		t.Fatalf("retry: %d %v", code, out)
	}
	code, out = post(t, srv.URL, "", batchOf("r1", 4, 8), nil)
	if code != 200 || out["accepted"] != float64(3) || out["duplicates"] != float64(2) {
		t.Fatalf("overlap: %d %v", code, out)
	}
	rec, err := ReadFile(filepath.Join(dir, "r1.ndjson"))
	if err != nil {
		t.Fatalf("the stored run is not a valid recording: %v", err)
	}
	if len(rec.Events) != 8 {
		t.Fatalf("%d events stored, want 8 (each once, in order)", len(rec.Events))
	}
	// An empty batch (what doctor sends) is fine and stores nothing.
	if code, out = post(t, srv.URL, "", "", nil); code != 200 || out["accepted"] != float64(0) {
		t.Fatalf("empty batch: %d %v", code, out)
	}
}

func TestReceiverRunsLandInSeparateFiles(t *testing.T) {
	_, srv, dir := newReceiver(t)
	post(t, srv.URL, "", batchOf("a", 1, 2), nil)
	post(t, srv.URL, "", batchOf("b", 1, 3), nil)
	for run, n := range map[string]int{"a": 2, "b": 3} {
		rec, err := ReadFile(filepath.Join(dir, run+".ndjson"))
		if err != nil || len(rec.Events) != n {
			t.Fatalf("%s: %v %v", run, err, rec)
		}
	}
}

func TestReceiverRejects(t *testing.T) {
	r, srv, dir := newReceiver(t)
	r.Token = "secret"
	if code, _ := post(t, srv.URL, "", batchOf("r", 1, 1), nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := post(t, srv.URL, "wrong", batchOf("r", 1, 1), nil); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	good := batchOf("r", 1, 2)
	cases := map[string]string{
		"not json":            good + "{nope}\n",
		"wrong schema":        strings.Replace(good, Schema, "other/v9", 1),
		"no seq":              strings.Replace(good, `"seq":1`, `"seq":0`, 1),
		"no data":             strings.Replace(good, `"data":{"x":1}`, `"data":`, 1),
		"run id is a path":    strings.ReplaceAll(good, `"runId":"r"`, `"runId":"../escape"`),
		"run id with a slash": strings.ReplaceAll(good, `"runId":"r"`, `"runId":"a/b"`),
	}
	for name, body := range cases {
		code, _ := post(t, srv.URL, "secret", body, nil)
		if code != 400 {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	// A bad line rejects the whole batch: nothing before it is stored.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("a rejected batch left files behind: %v", entries)
	}
	// The run id header must agree with the events.
	if code, _ := post(t, srv.URL, "secret", good, map[string]string{"X-Litmus-Lite-Run-Id": "other"}); code != 400 {
		t.Fatalf("header mismatch: %d", code)
	}
	if code, _ := post(t, srv.URL, "secret", good, map[string]string{"X-Litmus-Lite-Run-Id": "r"}); code != 200 {
		t.Fatalf("matching header: %d", code)
	}
}

func TestReceiverBodyLimit(t *testing.T) {
	_, srv, _ := newReceiver(t)
	big := strings.Repeat("x", MaxIngestBody+1)
	if code, _ := post(t, srv.URL, "", big, nil); code != 413 {
		t.Fatalf("oversize body: %d, want 413", code)
	}
}

func TestReceiverIgnoresEventsAfterTheEnd(t *testing.T) {
	_, srv, dir := newReceiver(t)
	body := evLine("e", 1, TypeRunStarted) + "\n" + evLine("e", 2, TypeRunFinished) + "\n" + evLine("e", 3, TypePoint) + "\n"
	code, out := post(t, srv.URL, "", body, nil)
	if code != 200 || out["accepted"] != float64(2) || out["ignored"] != float64(1) {
		t.Fatalf("%d %v", code, out)
	}
	rec, err := ReadFile(filepath.Join(dir, "e.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.Terminal(); !ok || len(rec.Events) != 2 {
		t.Fatalf("recording: %d events", len(rec.Events))
	}
}

func TestReceiverRemembersAcrossRestarts(t *testing.T) {
	r, srv, dir := newReceiver(t)
	_ = r
	post(t, srv.URL, "", batchOf("keep", 1, 4), nil)
	r2, err := NewReceiver(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(r2.Handler())
	defer srv2.Close()
	_, out := post(t, srv2.URL, "", batchOf("keep", 3, 6), nil)
	if out["accepted"] != float64(2) || out["duplicates"] != float64(2) {
		t.Fatalf("after restart: %v", out)
	}
	rec, err := ReadFile(filepath.Join(dir, "keep.ndjson"))
	if err != nil || len(rec.Events) != 6 {
		t.Fatalf("%v %v", err, rec)
	}
}

func TestReceiverDevelopmentSwitches(t *testing.T) {
	r, srv, _ := newReceiver(t)
	r.FailPercent = 100
	if code, _ := post(t, srv.URL, "", batchOf("f", 1, 1), nil); code != 503 {
		t.Fatalf("fail-percent 100: %d", code)
	}
	// Authentication comes first: a wrong token is never answered with a status
	// the sender would retry.
	r.Token = "t"
	if code, _ := post(t, srv.URL, "bad", batchOf("f", 1, 1), nil); code != 401 {
		t.Fatalf("wrong token under fail-percent: %d", code)
	}
	r.FailPercent = 0
	r.Token = ""
	r.Delay = 150 * time.Millisecond
	start := time.Now()
	if code, _ := post(t, srv.URL, "", batchOf("f", 1, 1), nil); code != 200 {
		t.Fatalf("delayed: %d", code)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("-delay did nothing")
	}
}

func TestReceiverPrint(t *testing.T) {
	r, srv, _ := newReceiver(t)
	var lines []string
	r.Print = func(l string) { lines = append(lines, l) }
	post(t, srv.URL, "", batchOf("p", 1, 2), nil)
	post(t, srv.URL, "", batchOf("p", 1, 2), nil)
	if len(lines) != 2 || !bytes.Contains([]byte(lines[0]), []byte("p #1 run.started")) {
		t.Fatalf("print lines %v", lines)
	}
}
