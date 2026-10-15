package stream

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Server serves recordings over HTTP: a run index, one SSE stream per run,
// and each finished run's result. It is read-only.
//
// Events are replayed with the pacing they were recorded at, divided by Speed.
// Every client gets the run from its start (or after Last-Event-ID), so any
// number of clients can watch the same recording independently.
type Server struct {
	Runs []*Recording
	// Speed multiplies playback speed. Zero or less sends everything at once.
	Speed float64
	// MaxGap caps the pause between two events. Zero means no cap.
	MaxGap time.Duration
	// CORSOrigin, when set, is sent as Access-Control-Allow-Origin.
	CORSOrigin string
	// Token, when set, is required as a bearer token on every request except
	// /v1/health.
	Token   string
	Version string
}

// RunSummary is one entry of GET /v1/runs.
type RunSummary struct {
	RunID     string            `json:"runId"`
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels,omitempty"`
	Status    string            `json:"status"`
	Synthetic bool              `json:"synthetic,omitempty"`
	// Incomplete marks a run that has no terminal event and has not been
	// written to for a long time: the process that was running it is gone.
	// Its status is errored.
	Incomplete bool       `json:"incomplete,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Events     int        `json:"events"`
}

// Summary describes a recording for the run index. Status is passed or failed
// (the run completed, with or without all hypotheses holding), errored (the
// run did not complete), or running (no terminal event yet).
func Summary(rec *Recording) RunSummary {
	var term *Envelope
	if last, ok := rec.Terminal(); ok {
		term = &last
	}
	return summarize(rec.Events[0], len(rec.Events), term)
}

func summarize(first Envelope, events int, term *Envelope) RunSummary {
	s := RunSummary{
		RunID:     first.RunID,
		Labels:    first.Labels,
		Status:    "running",
		Synthetic: first.Synthetic,
		StartedAt: first.TS,
		Events:    events,
	}
	var started struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(first.Data, &started)
	s.Name = started.Name
	if term != nil {
		ts := term.TS
		s.FinishedAt = &ts
		switch term.Type {
		case TypeRunFailed:
			s.Status = "errored"
		default:
			var fin struct {
				Passed bool `json:"passed"`
			}
			_ = json.Unmarshal(term.Data, &fin)
			if fin.Passed {
				s.Status = "passed"
			} else {
				s.Status = "failed"
			}
		}
	}
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/info", s.info)
	mux.HandleFunc("GET /v1/runs", s.list)
	mux.HandleFunc("GET /v1/runs/{id}/events", s.events)
	mux.HandleFunc("GET /v1/runs/{id}/result", s.result)
	return s.wrap(mux)
}

func (s *Server) wrap(next http.Handler) http.Handler { return guard(s.Token, s.CORSOrigin, next) }

// guard applies the CORS header and the bearer token check shared by every
// read-only handler. /v1/health is open.
func guard(token, corsOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsOrigin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", corsOrigin)
			h.Set("Access-Control-Allow-Headers", "Authorization, Last-Event-ID, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if token != "" && r.URL.Path != "/v1/health" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":         "litmus-lite",
		"version":      s.Version,
		"mode":         "replay",
		"schemas":      []string{Schema},
		"capabilities": []string{"runs", "events", "result", "replay"},
	})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	label := q.Get("label")
	out := []RunSummary{}
	for _, rec := range s.Runs {
		sum := Summary(rec)
		if status != "" && sum.Status != status {
			continue
		}
		if label != "" {
			k, v, _ := strings.Cut(label, "=")
			if sum.Labels[k] != v {
				continue
			}
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) find(id string) *Recording {
	for _, rec := range s.Runs {
		if rec.RunID() == id {
			return rec
		}
	}
	return nil
}

func (s *Server) result(w http.ResponseWriter, r *http.Request) {
	rec := s.find(r.PathValue("id"))
	if rec == nil {
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

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rec := s.find(r.PathValue("id"))
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown run"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	speed := s.Speed
	if v := r.URL.Query().Get("speed"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			speed = f
		}
	}
	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	total := int64(len(rec.Events))
	if _, done := rec.Terminal(); done && after >= total {
		// A browser EventSource reconnects when the stream closes. 204 tells
		// it the run is over, so it stops.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	start := int(after)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(rec.Events); i++ {
		ev := rec.Events[i]
		if i > start && speed > 0 {
			if gap := ev.TS.Sub(rec.Events[i-1].TS); gap > 0 {
				d := time.Duration(float64(gap) / speed)
				if s.MaxGap > 0 && d > s.MaxGap {
					d = s.MaxGap
				}
				t := time.NewTimer(d)
				select {
				case <-r.Context().Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, rec.Raw[i]); err != nil {
			return
		}
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// CheckBind enforces the listen rule: a loopback address needs nothing, any
// other address needs a token.
func CheckBind(addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if token != "" || IsLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("listen address %q is not loopback: set LITMUS_LITE_STREAM_TOKEN to serve on it", addr)
}

// IsLoopbackHost reports whether host is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
