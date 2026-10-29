package stream

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
)

func fastPush(t *testing.T, raw, token string) *PushSink {
	t.Helper()
	u, err := ValidatePushURL(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	return NewPushSink(PushOptions{
		URL: u, Token: token, Version: "t",
		MaxDelay: 20 * time.Millisecond, BackoffMin: 5 * time.Millisecond, BackoffMax: 40 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
}

// sendRun drives a recorder with n points between run.started and run.finished.
func sendRun(r *Recorder, points int) {
	r.Started(testScenario(), time.Now())
	r.Phase("steady")
	for i := 0; i < points; i++ {
		r.Point(report.Point{OffsetS: float64(i), Phase: "steady"})
	}
	r.Finished(&report.Result{Name: "checkout", Passed: true})
}

func TestValidatePushURL(t *testing.T) {
	ok := []struct {
		url      string
		insecure bool
	}{
		{"https://app.example.com/ingest", false},
		{"http://127.0.0.1:9000/v1/ingest", false},
		{"http://localhost:9000/v1/ingest", false},
		{"http://[::1]:9000/x", false},
		{"http://app.example.com/ingest", true},
	}
	for _, c := range ok {
		if _, err := ValidatePushURL(c.url, c.insecure); err != nil {
			t.Errorf("%s: %v", c.url, err)
		}
	}
	bad := map[string]string{
		"http://app.example.com/ingest":      "https",
		"ftp://x/y":                          "http://",
		"app.example.com/ingest":             "http://",
		"https://user:pw@app.example.com/in": "credentials",
		"https://":                           "host",
		"http://192.168.1.10:9000/v1/ingest": "https",
	}
	for raw, want := range bad {
		_, err := ValidatePushURL(raw, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want mention of %q", raw, err, want)
		}
		if err != nil && (strings.Contains(err.Error(), "pw@") || strings.Contains(err.Error(), "/ingest")) {
			t.Errorf("%s: the message repeats the path or credentials: %v", raw, err)
		}
	}
}

func TestPushDeliversInOrderWithHeaders(t *testing.T) {
	rcv, err := NewReceiver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rcv.Token = "tok"
	var mu sync.Mutex
	var heads []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		heads = append(heads, r.Header.Clone())
		mu.Unlock()
		rcv.Handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	sink := fastPush(t, srv.URL+"/v1/ingest", "tok")
	rec := NewRecorder(RecorderOptions{RunID: "push-1"}, sink)
	sendRun(rec, 250)
	if stuck := rec.Close(3 * time.Second); len(stuck) != 0 {
		t.Fatalf("stuck %v", stuck)
	}
	stored, err := ReadFile(filepath.Join(rcv.Dir, "push-1.ndjson"))
	if err != nil {
		t.Fatalf("pushed run is not a valid recording (order, gaps or duplicates): %v", err)
	}
	if len(stored.Events) != 253 {
		t.Fatalf("%d events stored, want 253", len(stored.Events))
	}
	if d, lost := sink.Delivered(); d != 253 || lost != 0 {
		t.Fatalf("delivered %d lost %d", d, lost)
	}
	// 253 events with 100 per batch: at least three requests, ranges in order.
	prevTo := int64(0)
	for _, h := range heads {
		from, _ := strconv.ParseInt(h.Get("X-Litmus-Lite-Seq-From"), 10, 64)
		to, _ := strconv.ParseInt(h.Get("X-Litmus-Lite-Seq-To"), 10, 64)
		if from != prevTo+1 || to < from || to-from+1 > 100 {
			t.Fatalf("batch %d..%d after %d", from, to, prevTo)
		}
		prevTo = to
		if h.Get("Authorization") != "Bearer tok" || h.Get("X-Litmus-Lite-Run-Id") != "push-1" ||
			h.Get("Content-Type") != "application/x-ndjson" || !strings.HasPrefix(h.Get("User-Agent"), "litmus-lite/") {
			t.Fatalf("headers %v", h)
		}
	}
	if len(heads) < 3 {
		t.Fatalf("%d requests", len(heads))
	}
}

func TestPushBatchesByTimeWhenTheRunIsQuiet(t *testing.T) {
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Add(int32(bytes.Count(b, []byte("\n"))))
		w.WriteHeader(200)
	}))
	defer srv.Close()
	sink := fastPush(t, srv.URL, "")
	rec := NewRecorder(RecorderOptions{RunID: "quiet"}, sink)
	rec.Started(testScenario(), time.Now())
	rec.Phase("steady")
	// Two events, far below 100: they must still arrive without waiting for the end.
	deadline := time.Now().Add(2 * time.Second)
	for got.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 2 {
		t.Fatalf("%d events delivered before the run ended, want 2", got.Load())
	}
	rec.Finished(&report.Result{})
	rec.Close(time.Second)
}

func TestPushRetriesAndTheReceiverStoresEachEventOnce(t *testing.T) {
	cases := map[string]func(rcv *Receiver, srv func() *httptest.Server) http.Handler{
		"flapping server (every other request 503)": func(rcv *Receiver, _ func() *httptest.Server) http.Handler {
			var n atomic.Int32
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if n.Add(1)%2 == 1 {
					w.WriteHeader(503)
					return
				}
				rcv.Handler().ServeHTTP(w, r)
			})
		},
		"ack lost: the receiver stores the batch, then the answer is a 500": func(rcv *Receiver, _ func() *httptest.Server) http.Handler {
			var n atomic.Int32
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if n.Add(1)%3 == 1 {
					rec := httptest.NewRecorder()
					rcv.Handler().ServeHTTP(rec, r)
					w.WriteHeader(500)
					return
				}
				rcv.Handler().ServeHTTP(w, r)
			})
		},
		"429 with Retry-After, 408": func(rcv *Receiver, _ func() *httptest.Server) http.Handler {
			var n atomic.Int32
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch n.Add(1) {
				case 1:
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(429)
				case 2:
					w.WriteHeader(408)
				default:
					rcv.Handler().ServeHTTP(w, r)
				}
			})
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			rcv, _ := NewReceiver(t.TempDir())
			srv := httptest.NewServer(mk(rcv, nil))
			defer srv.Close()
			sink := fastPush(t, srv.URL+"/v1/ingest", "")
			rec := NewRecorder(RecorderOptions{RunID: "retry"}, sink)
			sendRun(rec, 220)
			if stuck := rec.Close(5 * time.Second); len(stuck) != 0 {
				t.Fatalf("stuck %v", stuck)
			}
			stored, err := ReadFile(filepath.Join(rcv.Dir, "retry.ndjson"))
			if err != nil {
				t.Fatalf("stored run invalid: %v", err)
			}
			if len(stored.Events) != 223 {
				t.Fatalf("%d events stored, want 223", len(stored.Events))
			}
		})
	}
}

// The same senders against `stream receive -delay` and `-fail-percent`.
func TestPushAgainstAMisbehavingReceiver(t *testing.T) {
	for name, set := range map[string]func(*Receiver){
		"-fail-percent 60": func(r *Receiver) { r.FailPercent = 60 },
		"-delay 60ms":      func(r *Receiver) { r.Delay = 60 * time.Millisecond },
	} {
		t.Run(name, func(t *testing.T) {
			rcv, _ := NewReceiver(t.TempDir())
			set(rcv)
			srv := httptest.NewServer(rcv.Handler())
			defer srv.Close()
			sink := fastPush(t, srv.URL+"/v1/ingest", "")
			rec := NewRecorder(RecorderOptions{RunID: "mis"}, sink)
			begin := time.Now()
			sendRun(rec, 300)
			if d := time.Since(begin); d > time.Second {
				t.Fatalf("emitting took %s: the sender waited on the receiver", d)
			}
			if stuck := rec.Close(10 * time.Second); len(stuck) != 0 {
				t.Fatalf("stuck %v", stuck)
			}
			stored, err := ReadFile(filepath.Join(rcv.Dir, "mis.ndjson"))
			if err != nil || len(stored.Events) != 303 {
				t.Fatalf("stored: %v, %v events", err, stored)
			}
		})
	}
}

func TestPushDropsABatchTheReceiverWillNeverTake(t *testing.T) {
	var calls atomic.Int32
	var bodies [][]byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		if calls.Add(1) == 1 {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	sink := fastPush(t, srv.URL, "")
	sink.opt.MaxBatch = 5
	rec := NewRecorder(RecorderOptions{RunID: "drop"}, sink)
	sendRun(rec, 20)
	rec.Close(3 * time.Second)

	// The first batch is gone for good (a 401 is not retried), the rest arrive.
	d, lost := sink.Delivered()
	if lost != 5 || d != 18 {
		t.Fatalf("delivered %d lost %d, want 18 and 5", d, lost)
	}
	if calls.Load() != 5 {
		t.Fatalf("%d requests, want 5 (23 events in batches of 5, no retry of a 401)", calls.Load())
	}
	w := strings.Join(sink.Warnings(), "\n")
	if !strings.Contains(w, "401") || !strings.Contains(w, "dropped") {
		t.Fatalf("warnings %q", w)
	}
}

func TestPushRepeatedRejectionIsOneWarningWithACount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	sink := fastPush(t, srv.URL, "")
	sink.opt.MaxBatch = 3
	rec := NewRecorder(RecorderOptions{RunID: "w"}, sink)
	sendRun(rec, 30)
	rec.Close(3 * time.Second)
	ws := sink.Warnings()
	if len(ws) != 1 || !strings.Contains(ws[0], "times") {
		t.Fatalf("warnings %q", ws)
	}
}

func TestPushToADeadServerNeverBlocksTheRunAndGivesUpAtExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens any more
	sink := fastPush(t, url, "")
	rec := NewRecorder(RecorderOptions{RunID: "dead"}, sink)
	begin := time.Now()
	sendRun(rec, 500)
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("emitting took %s against a dead server", d)
	}
	closeStart := time.Now()
	stuck := rec.Close(400 * time.Millisecond)
	if d := time.Since(closeStart); d > 2*time.Second {
		t.Fatalf("Close took %s", d)
	}
	if len(stuck) != 1 {
		t.Fatalf("stuck %v, want the push sink", stuck)
	}
	w := strings.Join(sink.Warnings(), "\n")
	if !strings.Contains(w, "not accepting events") {
		t.Fatalf("warnings %q", w)
	}
	// The warning names the host, never the path.
	if strings.Contains(w, "/v1/") {
		t.Fatalf("warning leaks the URL path: %q", w)
	}
}

func TestPushToASlowServerDoesNotDelayTheRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	sink := fastPush(t, srv.URL, "")
	rec := NewRecorder(RecorderOptions{RunID: "slow", QueueDepth: 50}, sink)
	begin := time.Now()
	sendRun(rec, 5000)
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("emitting took %s against a slow server", d)
	}
	rec.Close(4 * time.Second)
	// Lifecycle events all arrived even though points were dropped.
	d, _ := sink.Delivered()
	if d < 3 || d > 5003 {
		t.Fatalf("delivered %d", d)
	}
}

func TestPushDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(200)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	sink := fastPush(t, srv.URL, "tok")
	rec := NewRecorder(RecorderOptions{RunID: "redir"}, sink)
	sendRun(rec, 1)
	rec.Close(time.Second)
	if elsewhere.Load() != 0 {
		t.Fatal("a redirect was followed: the token could have been sent to another host")
	}
	if _, lost := sink.Delivered(); lost == 0 {
		t.Fatal("a redirect is not an acknowledgement")
	}
}

func TestPushWarningsNeverContainTheTokenOrQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer srv.Close()
	sink := fastPush(t, srv.URL+"/ingest?key=SECRET-QUERY", "SECRET-TOKEN")
	rec := NewRecorder(RecorderOptions{RunID: "leak"}, sink)
	sendRun(rec, 1)
	rec.Close(time.Second)
	all := strings.Join(rec.Warnings(), "\n")
	if all == "" || strings.Contains(all, "SECRET") {
		t.Fatalf("warnings %q", all)
	}
}

func TestCheckPush(t *testing.T) {
	rcv, _ := NewReceiver(t.TempDir())
	rcv.Token = "tok"
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	if err := CheckPush(srv.URL+"/v1/ingest", "tok", false); err != nil {
		t.Fatalf("good endpoint: %v", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(rcv.Dir, "*")); len(entries) != 0 {
		t.Fatalf("the test batch created files: %v", entries)
	}
	if err := CheckPush(srv.URL+"/v1/ingest", "wrong", false); err == nil || !strings.Contains(err.Error(), "LITMUS_LITE_STREAM_TOKEN") {
		t.Fatalf("wrong token: %v", err)
	}
	if err := CheckPush(srv.URL+"/nope", "tok", false); err == nil {
		t.Fatal("a 404 endpoint passed")
	}
	u, _ := url.Parse(srv.URL)
	srv.Close()
	if err := CheckPush("http://"+u.Host+"/v1/ingest", "", false); err == nil {
		t.Fatal("a closed port passed")
	}
	if err := CheckPush("http://example.com/x", "", false); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("insecure remote: %v", err)
	}
}

func TestRecorderSamplesGoThroughPush(t *testing.T) {
	rcv, _ := NewReceiver(t.TempDir())
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	sink := fastPush(t, srv.URL+"/v1/ingest", "")
	rec := NewRecorder(RecorderOptions{RunID: "samp", Samples: true}, sink)
	rec.Started(testScenario(), time.Now())
	rec.Sample(probe.Sample{Name: "p", OK: true})
	rec.Finished(&report.Result{})
	rec.Close(2 * time.Second)
	stored, err := ReadFile(filepath.Join(rcv.Dir, "samp.ndjson"))
	if err != nil || len(stored.Events) != 3 || stored.Events[1].Type != TypeProbeSample {
		t.Fatalf("%v %v", err, stored)
	}
}

// When the run is over the last batch holds run.finished. A streak of
// failures that the doubling backoff would stretch past the exit window must
// not cost that event: at exit the sink retries at the shortest interval.
func TestPushRetriesTightlyOnceTheRunIsOver(t *testing.T) {
	rcv, _ := NewReceiver(t.TempDir())
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 7 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rcv.Handler().ServeHTTP(w, r)
	}))
	defer srv.Close()
	u, _ := ValidatePushURL(srv.URL+"/v1/ingest", false)
	// Doubling from 100 ms, seven failures cost 12.7 s before the eighth try.
	sink := NewPushSink(PushOptions{URL: u, MaxDelay: 20 * time.Millisecond, BackoffMin: 100 * time.Millisecond, BackoffMax: 10 * time.Second, Timeout: 2 * time.Second})
	rec := NewRecorder(RecorderOptions{RunID: "tight"}, sink)
	sendRun(rec, 3)
	start := time.Now()
	if stuck := rec.Close(5 * time.Second); len(stuck) != 0 {
		t.Fatalf("stuck: %v, warnings %v", stuck, sink.Warnings())
	}
	if el := time.Since(start); el > 4*time.Second {
		t.Fatalf("closing took %v", el)
	}
	if d, l := sink.Delivered(); d != 6 || l != 0 {
		t.Fatalf("delivered %d lost %d, want 6 and 0", d, l)
	}
	got, err := ReadFile(filepath.Join(rcv.Dir, "tight.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if last, ok := got.Terminal(); !ok || last.Type != TypeRunFinished {
		t.Fatalf("the receiver never saw run.finished: %+v", last)
	}
}
