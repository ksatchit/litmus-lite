package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
)

// sampleResult is a small finished run: 1s steady, 2s inject, 1s recover.
func sampleResult() *report.Result {
	t0 := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	res := &report.Result{
		Name:      "unit",
		StartedAt: t0,
		Elapsed:   4500 * time.Millisecond,
		Passed:    false,
		Phases: []report.Phase{
			{Name: "steady", From: at(0.1), To: at(1)},
			{Name: "inject", From: at(1), To: at(3)},
			{Name: "recover", From: at(3), To: at(4)},
		},
		FaultLog: []report.FaultEvent{{
			Name: "slow", Kind: "failpoint", Start: at(1), End: at(3), Intensity: 1, Rollback: "ok",
			Tunables: map[string]any{"url": "http://x", "token": "s3cret", "nested": map[string]any{"apiKey": "k"}},
		}},
		Probes: []probe.Sample{
			{At: at(0.5), Name: "p", OK: true, Latency: 10 * time.Millisecond, Status: 200},
			{At: at(1.5), Name: "p", OK: false, Latency: 900 * time.Millisecond, Status: 500, Error: "500"},
		},
		Hypotheses: []report.HypothesisVerdict{
			{Name: "recovers", Metric: "recovery", Operator: "<=", Limit: "1s", Observed: "2.00s", Passed: false},
		},
		Recovery: 2 * time.Second,
		Warnings: []string{"vegaload not on PATH"},
	}
	for i := 0; i <= 4; i++ {
		res.TimeSeries = append(res.TimeSeries, report.Point{
			T: at(float64(i)), OffsetS: float64(i), P95Ms: float64(10 * i), Phase: "steady", Availability: 1,
		})
	}
	return res
}

func TestFromReportOrderAndTerminal(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{Version: "test"})
	if len(evs) == 0 || evs[0].Type != TypeRunStarted {
		t.Fatalf("first event: %+v", evs)
	}
	last := evs[len(evs)-1]
	if last.Type != TypeRunFinished {
		t.Fatalf("last event type %s", last.Type)
	}
	var terminals int
	for i, e := range evs {
		if e.Seq != int64(i+1) {
			t.Fatalf("seq %d at index %d", e.Seq, i)
		}
		if i > 0 && e.TS.Before(evs[i-1].TS) {
			t.Fatalf("events out of order at %d", i)
		}
		if !e.Synthetic {
			t.Fatalf("event %d not synthetic", i)
		}
		if e.Schema != Schema {
			t.Fatalf("schema %q", e.Schema)
		}
		if e.Terminal() {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("terminal events: %d", terminals)
	}
	types := map[string]int{}
	for _, e := range evs {
		types[e.Type]++
	}
	want := map[string]int{
		TypeRunStarted: 1, TypePhaseChanged: 3, TypeFaultStarted: 1, TypeFaultEnded: 1,
		TypePoint: 5, TypeWarning: 1, TypeRunFinished: 1,
	}
	for k, v := range want {
		if types[k] != v {
			t.Errorf("%s: got %d want %d (%v)", k, types[k], v, types)
		}
	}
	if types[TypeProbeSample] != 0 {
		t.Errorf("probe samples emitted without -samples")
	}
}

func TestFromReportPointClosesAfterBucket(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{})
	for _, e := range evs {
		if e.Type != TypePoint {
			continue
		}
		var p Point
		if err := json.Unmarshal(e.Data, &p); err != nil {
			t.Fatal(err)
		}
		if got, want := e.OffsetS, p.OffsetS+1; got != want {
			t.Errorf("point bucket %v emitted at offset %v, want %v", p.OffsetS, got, want)
		}
	}
}

func TestFromReportFinishedMatchesReport(t *testing.T) {
	res := sampleResult()
	evs := FromReport(res, FromReportOptions{})
	var fin map[string]any
	if err := json.Unmarshal(evs[len(evs)-1].Data, &fin); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var rep map[string]any
	_ = json.Unmarshal(b, &rep)
	for k, v := range rep {
		if k == "probes" || k == "faultLog" {
			// Probes are left out without -samples. faultLog differs only by
			// redacted tunables, which TestFromReportNeverLeaksSecretsAnywhere covers.
			continue
		}
		fv, ok := fin[k]
		if !ok {
			t.Errorf("run.finished is missing %q", k)
			continue
		}
		fb, _ := json.Marshal(fv)
		vb, _ := json.Marshal(v)
		if !bytes.Equal(fb, vb) {
			t.Errorf("%s differs: %s vs %s", k, fb, vb)
		}
	}
	if _, has := fin["probes"]; has {
		t.Errorf("probes on run.finished without -samples")
	}
	if d, _ := fin["diagnosis"].(string); !strings.Contains(d, "Run unit failed hypotheses") {
		t.Errorf("diagnosis: %q", d)
	}
}

func TestFromReportSamples(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{Samples: true})
	n := 0
	for _, e := range evs {
		if e.Type == TypeProbeSample {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("probe.sample events: %d", n)
	}
	var fin map[string]any
	_ = json.Unmarshal(evs[len(evs)-1].Data, &fin)
	if _, has := fin["probes"]; !has {
		t.Errorf("run.finished lost probes with -samples")
	}
}

func TestFromReportRedactsSecrets(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{})
	for _, e := range evs {
		if e.Type != TypeRunStarted && e.Type != TypeFaultStarted {
			continue
		}
		if strings.Contains(string(e.Data), "s3cret") {
			t.Errorf("%s leaks the token: %s", e.Type, e.Data)
		}
		if strings.Contains(string(e.Data), `"apiKey":"k"`) {
			t.Errorf("%s leaks a nested key: %s", e.Type, e.Data)
		}
		if !strings.Contains(string(e.Data), `"token":"***"`) {
			t.Errorf("%s does not show a redacted token: %s", e.Type, e.Data)
		}
	}
}

func TestFromReportNeverLeaksSecretsAnywhere(t *testing.T) {
	res := sampleResult()
	for _, samples := range []bool{false, true} {
		evs := FromReport(res, FromReportOptions{Samples: samples})
		for _, e := range evs {
			if strings.Contains(string(e.Data), "s3cret") || strings.Contains(string(e.Data), `"apiKey":"k"`) {
				t.Errorf("samples=%v: %s leaks a credential: %s", samples, e.Type, e.Data)
			}
		}
	}
	if res.FaultLog[0].Tunables["token"] != "s3cret" {
		t.Fatalf("FromReport changed the report it was given")
	}
}

func TestRedactKeepsOtherValuesAndDoesNotMutate(t *testing.T) {
	in := map[string]any{"url": "http://x", "Password": "p", "list": []any{map[string]any{"secret": 1}}}
	out := Redact(in)
	if out["url"] != "http://x" || out["Password"] != "***" {
		t.Fatalf("out: %v", out)
	}
	if in["Password"] != "p" {
		t.Fatalf("input mutated")
	}
	inner := out["list"].([]any)[0].(map[string]any)
	if inner["secret"] != "***" {
		t.Fatalf("list not redacted: %v", out)
	}
}

func TestDeriveRunIDIsStable(t *testing.T) {
	a := FromReport(sampleResult(), FromReportOptions{})
	b := FromReport(sampleResult(), FromReportOptions{})
	if a[0].RunID == "" || a[0].RunID != b[0].RunID {
		t.Fatalf("run ids: %q %q", a[0].RunID, b[0].RunID)
	}
	c := FromReport(sampleResult(), FromReportOptions{RunID: "mine", Labels: map[string]string{"service": "x"}})
	if c[0].RunID != "mine" || c[0].Labels["service"] != "x" {
		t.Fatalf("overrides ignored: %+v", c[0])
	}
}

func roundTrip(t *testing.T, evs []Envelope) *Recording {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, evs); err != nil {
		t.Fatal(err)
	}
	rec, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestReadRoundTrip(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{})
	rec := roundTrip(t, evs)
	if len(rec.Events) != len(evs) || len(rec.Raw) != len(evs) {
		t.Fatalf("events %d raw %d want %d", len(rec.Events), len(rec.Raw), len(evs))
	}
	if _, ok := rec.Terminal(); !ok {
		t.Fatalf("no terminal event")
	}
	s := Summary(rec)
	if s.Status != "failed" || s.Name != "unit" || s.FinishedAt == nil {
		t.Fatalf("summary: %+v", s)
	}
}

func TestReadRejectsBadRecordings(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{})
	line := func(e Envelope) string {
		b, _ := json.Marshal(e)
		return string(b)
	}
	cases := map[string]string{
		"empty":             "",
		"not json":          "{nope}\n",
		"wrong schema":      strings.Replace(line(evs[0]), Schema, "other/v9", 1) + "\n",
		"seq gap":           line(evs[0]) + "\n" + line(evs[2]) + "\n",
		"no data":           `{"schema":"` + Schema + `","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","type":"run.started"}` + "\n",
		"after end":         line(evs[0]) + "\n" + terminalLine(evs[0].RunID, 2) + "\n" + line(Envelope{Schema: Schema, RunID: evs[0].RunID, Seq: 3, TS: evs[0].TS, Type: TypeWarning, Data: json.RawMessage(`{}`)}) + "\n",
		"first not started": line(Envelope{Schema: Schema, RunID: "r", Seq: 1, TS: evs[0].TS, Type: TypePoint, Data: json.RawMessage(`{}`)}) + "\n",
	}
	for name, in := range cases {
		if _, err := Read(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func terminalLine(runID string, seq int64) string {
	b, _ := json.Marshal(Envelope{
		Schema: Schema, RunID: runID, Seq: seq, TS: time.Date(2026, 10, 14, 8, 0, 1, 0, time.UTC),
		Type: TypeRunFailed, Data: json.RawMessage(`{"reason":"x"}`),
	})
	return string(b)
}

func TestReadKeepsUnknownFields(t *testing.T) {
	evs := FromReport(sampleResult(), FromReportOptions{})
	var buf bytes.Buffer
	_ = Write(&buf, evs[:1])
	line := strings.TrimSpace(buf.String())
	line = strings.TrimSuffix(line, "}") + `,"future":"yes"}`
	rec, err := Read(strings.NewReader(line + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec.Raw[0]), `"future":"yes"`) {
		t.Fatalf("raw line lost an unknown field: %s", rec.Raw[0])
	}
}

func newTestServer(t *testing.T, mutate func(*Server)) (*httptest.Server, *Recording) {
	t.Helper()
	rec := roundTrip(t, FromReport(sampleResult(), FromReportOptions{Labels: map[string]string{"service": "checkout"}}))
	srv := &Server{Runs: []*Recording{rec}, Version: "test"}
	if mutate != nil {
		mutate(srv)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, rec
}

func get(t *testing.T, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func sseIDs(body string) []string {
	var ids []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "id: ") {
			ids = append(ids, strings.TrimPrefix(l, "id: "))
		}
	}
	return ids
}

func TestServerRunsInfoResultHealth(t *testing.T) {
	ts, rec := newTestServer(t, nil)

	resp, body := get(t, ts.URL+"/v1/runs", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("runs: %d", resp.StatusCode)
	}
	var runs []RunSummary
	if err := json.Unmarshal([]byte(body), &runs); err != nil || len(runs) != 1 {
		t.Fatalf("runs body %s err %v", body, err)
	}
	if runs[0].RunID != rec.RunID() || runs[0].Status != "failed" {
		t.Fatalf("run summary: %+v", runs[0])
	}

	_, body = get(t, ts.URL+"/v1/runs?status=passed", nil)
	if strings.TrimSpace(body) != "[]" {
		t.Fatalf("status filter: %s", body)
	}
	_, body = get(t, ts.URL+"/v1/runs?label=service=checkout", nil)
	if !strings.Contains(body, rec.RunID()) {
		t.Fatalf("label filter: %s", body)
	}
	_, body = get(t, ts.URL+"/v1/runs?label=service=other", nil)
	if strings.TrimSpace(body) != "[]" {
		t.Fatalf("label filter miss: %s", body)
	}

	resp, body = get(t, ts.URL+"/v1/runs/"+rec.RunID()+"/result", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `"timeseries"`) {
		t.Fatalf("result: %d %s", resp.StatusCode, body)
	}
	if resp, _ = get(t, ts.URL+"/v1/runs/nope/result", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown run: %d", resp.StatusCode)
	}
	if resp, body = get(t, ts.URL+"/v1/info", nil); resp.StatusCode != 200 || !strings.Contains(body, Schema) {
		t.Fatalf("info: %d %s", resp.StatusCode, body)
	}
	if resp, _ = get(t, ts.URL+"/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("health: %d", resp.StatusCode)
	}
}

func TestServerEventsReplayAndResume(t *testing.T) {
	ts, rec := newTestServer(t, nil)
	url := ts.URL + "/v1/runs/" + rec.RunID() + "/events"

	resp, body := get(t, url, nil) // Speed 0: no pacing
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	ids := sseIDs(body)
	if len(ids) != len(rec.Events) || ids[0] != "1" {
		t.Fatalf("ids %v for %d events", ids, len(rec.Events))
	}
	if !strings.Contains(body, "event: run.finished") {
		t.Fatalf("no terminal event in stream")
	}

	resp, body = get(t, url, map[string]string{"Last-Event-ID": "3"})
	ids = sseIDs(body)
	if resp.StatusCode != 200 || len(ids) != len(rec.Events)-3 || ids[0] != "4" {
		t.Fatalf("resume: %d ids %v", resp.StatusCode, ids)
	}

	last := len(rec.Events)
	resp, body = get(t, url, map[string]string{"Last-Event-ID": itoa(last)})
	if resp.StatusCode != http.StatusNoContent || body != "" {
		t.Fatalf("finished run should answer 204 so EventSource stops: %d %q", resp.StatusCode, body)
	}
	if resp, _ = get(t, ts.URL+"/v1/runs/nope/events", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown run: %d", resp.StatusCode)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestServerPacing(t *testing.T) {
	ts, rec := newTestServer(t, func(s *Server) { s.Speed = 20; s.MaxGap = 100 * time.Millisecond })
	start := time.Now()
	_, body := get(t, ts.URL+"/v1/runs/"+rec.RunID()+"/events", nil)
	took := time.Since(start)
	if len(sseIDs(body)) != len(rec.Events) {
		t.Fatalf("incomplete stream")
	}
	// The recording spans 4.5s; at 20x that is about 225ms. It must neither
	// be instant nor take real time.
	if took < 100*time.Millisecond || took > 3*time.Second {
		t.Fatalf("paced replay took %v", took)
	}
}

func TestServerCORSAndToken(t *testing.T) {
	ts, rec := newTestServer(t, func(s *Server) { s.CORSOrigin = "http://app.test"; s.Token = "tok" })

	resp, _ := get(t, ts.URL+"/v1/runs", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://app.test" {
		t.Fatalf("CORS header missing on a 401")
	}
	resp, _ = get(t, ts.URL+"/v1/runs", map[string]string{"Authorization": "Bearer wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	resp, _ = get(t, ts.URL+"/v1/runs/"+rec.RunID()+"/result", map[string]string{"Authorization": "Bearer tok"})
	if resp.StatusCode != 200 {
		t.Fatalf("right token: %d", resp.StatusCode)
	}
	if resp, _ = get(t, ts.URL+"/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("health needs no token: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/v1/runs", nil)
	pre, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	pre.Body.Close()
	if pre.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight: %d", pre.StatusCode)
	}
}

func TestCheckBind(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:7070", "localhost:1", "[::1]:9"} {
		if err := CheckBind(ok, ""); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:7070", ":7070", "192.168.1.5:80"} {
		if err := CheckBind(bad, ""); err == nil {
			t.Errorf("%s: want a refusal without a token", bad)
		}
		if err := CheckBind(bad, "tok"); err != nil {
			t.Errorf("%s with token: %v", bad, err)
		}
	}
	if err := CheckBind("nonsense", ""); err == nil {
		t.Errorf("want an error for a malformed address")
	}
}

// Where the load generator was run from is a local path. It stays in the
// result (report.json) and does not travel with the stream; the tool and
// version do.
func TestFinishedLeavesTheGeneratorPathOut(t *testing.T) {
	res := sampleResult()
	res.LoadGenerator = &report.LoadGenerator{Tool: "vegaload", Version: "0.9.0", Path: "/Users/someone/bin/vegaload", Via: "PATH"}
	fin := BuildFinished(res, false)
	b, _ := json.Marshal(fin)
	if strings.Contains(string(b), "/Users/someone") {
		t.Fatalf("a local path went into the event: %s", b)
	}
	g := fin.Result.LoadGenerator
	if g == nil || g.Tool != "vegaload" || g.Version != "0.9.0" || g.Via != "PATH" {
		t.Fatalf("generator in the event: %+v", g)
	}
	if res.LoadGenerator.Path == "" {
		t.Fatal("the caller's result was changed; report.json must keep the path")
	}
}
