package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultLiveBuffer is how many events a LiveServer keeps in memory.
const DefaultLiveBuffer = 50000

// maxLiveClients bounds the SSE connections one LiveServer serves.
const maxLiveClients = 32

// LiveServer serves the run that is in progress over HTTP. It is a Sink: the
// recorder hands it every event, and it keeps them in memory (up to Buffer)
// so a client that connects late, or reconnects with Last-Event-ID, reads the
// run from where it left off.
//
// It serves the same read-only /v1 API as `stream replay`, with one run, so a
// consumer written against replay works unchanged. GET /v1/events is an alias
// for that run's event stream.
//
// Past Buffer the oldest probe.sample, then point, events are dropped from
// the buffer. Lifecycle events are kept, so the run stays understandable, and
// a client sees a gap in seq where events were dropped.
type LiveServer struct {
	Addr       string
	Token      string
	CORSOrigin string
	Version    string
	Buffer     int

	mu      sync.Mutex
	rec     *Recording
	changed chan struct{} // closed and replaced whenever the buffer changes
	clients int

	ln  net.Listener
	srv *http.Server
}

// NewLiveServer checks the bind rule, starts listening, and begins serving.
func NewLiveServer(addr, token, corsOrigin, version string) (*LiveServer, error) {
	if err := CheckBind(addr, token); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l := &LiveServer{
		Addr: ln.Addr().String(), Token: token, CORSOrigin: corsOrigin, Version: version,
		Buffer: DefaultLiveBuffer, rec: &Recording{}, changed: make(chan struct{}), ln: ln,
	}
	l.srv = &http.Server{Handler: l.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = l.srv.Serve(ln) }()
	return l, nil
}

func (l *LiveServer) Name() string { return "pull " + l.Addr }

// Write implements Sink. It never fails and never blocks on a client.
func (l *LiveServer) Write(line []byte) error {
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	l.mu.Lock()
	l.rec.Events = append(l.rec.Events, e)
	l.rec.Raw = append(l.rec.Raw, append([]byte(nil), line...))
	if max := l.Buffer; max > 0 && len(l.rec.Events) > max {
		l.evict(len(l.rec.Events) - max)
	}
	old := l.changed
	l.changed = make(chan struct{})
	l.mu.Unlock()
	close(old)
	return nil
}

// evict removes n events: samples first, then points, oldest first. The caller
// holds the lock.
func (l *LiveServer) evict(n int) {
	for _, drop := range []string{TypeProbeSample, TypePoint, TypeLoadPoint} {
		if n <= 0 {
			return
		}
		keepE := l.rec.Events[:0:0]
		keepR := l.rec.Raw[:0:0]
		for i, e := range l.rec.Events {
			if n > 0 && e.Type == drop {
				n--
				continue
			}
			keepE = append(keepE, e)
			keepR = append(keepR, l.rec.Raw[i])
		}
		l.rec.Events, l.rec.Raw = keepE, keepR
	}
}

// Close implements Sink and does nothing: the server outlives the run. See
// Shutdown.
func (l *LiveServer) Close() error { return nil }

// Shutdown keeps serving for linger after the run, so a late client can read
// the final event, then stops. ctx cuts the wait short.
func (l *LiveServer) Shutdown(ctx context.Context, linger time.Duration) {
	if linger > 0 {
		select {
		case <-time.After(linger):
		case <-ctx.Done():
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.srv.Shutdown(sctx); err != nil {
		_ = l.srv.Close()
	}
}

// Handler returns the read-only /v1 handler.
func (l *LiveServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name": "litmus-lite", "version": l.Version, "mode": "live",
			"schemas":      []string{Schema},
			"capabilities": []string{"runs", "events", "result", "live"},
		})
	})
	mux.HandleFunc("GET /v1/runs", l.list)
	mux.HandleFunc("GET /v1/runs/{id}/events", func(w http.ResponseWriter, r *http.Request) { l.events(w, r, r.PathValue("id")) })
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) { l.events(w, r, "") })
	mux.HandleFunc("GET /v1/runs/{id}/result", l.result)
	return guard(l.Token, l.CORSOrigin, mux)
}

// snapshot returns a copy of the buffer's state under the lock.
func (l *LiveServer) snapshot() (rec Recording, changed <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Recording{Events: l.rec.Events[:len(l.rec.Events):len(l.rec.Events)], Raw: l.rec.Raw[:len(l.rec.Raw):len(l.rec.Raw)]}, l.changed
}

func (l *LiveServer) list(w http.ResponseWriter, r *http.Request) {
	rec, _ := l.snapshot()
	out := []RunSummary{}
	if len(rec.Events) > 0 {
		sum := Summary(&rec)
		q := r.URL.Query()
		match := q.Get("status") == "" || q.Get("status") == sum.Status
		if lbl := q.Get("label"); lbl != "" {
			k, v, _ := strings.Cut(lbl, "=")
			match = match && sum.Labels[k] == v
		}
		if match {
			out = append(out, sum)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (l *LiveServer) known(id string) bool {
	rec, _ := l.snapshot()
	return len(rec.Events) > 0 && (id == "" || rec.RunID() == id)
}

func (l *LiveServer) result(w http.ResponseWriter, r *http.Request) {
	rec, _ := l.snapshot()
	if len(rec.Events) == 0 || rec.RunID() != r.PathValue("id") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown run"})
		return
	}
	last, ok := rec.Terminal()
	if !ok || last.Type != TypeRunFinished {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run has no result"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(last.Data)
}

func (l *LiveServer) events(w http.ResponseWriter, r *http.Request, id string) {
	// Before the first event there is no run yet: tell the client to try again.
	if !l.known(id) {
		code := http.StatusNotFound
		msg := "unknown run"
		if id == "" {
			code, msg = http.StatusServiceUnavailable, "no run yet"
		}
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	l.mu.Lock()
	if l.clients >= maxLiveClients {
		l.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "too many clients"})
		return
	}
	l.clients++
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.clients--
		l.mu.Unlock()
	}()

	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	if rec, _ := l.snapshot(); len(rec.Events) > 0 {
		if last, done := rec.Terminal(); done && after >= last.Seq {
			// The client has everything. 204 tells a browser EventSource to stop reconnecting.
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		rec, changed := l.snapshot()
		// Events are in seq order: find the first one after the client's cursor.
		i := sort.Search(len(rec.Events), func(i int) bool { return rec.Events[i].Seq > after })
		for ; i < len(rec.Events); i++ {
			ev := rec.Events[i]
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, rec.Raw[i]); err != nil {
				return
			}
			after = ev.Seq
			if ev.Terminal() {
				flusher.Flush()
				return
			}
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		}
	}
}
