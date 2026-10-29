package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DirServer is `litmus-lite serve`: a read-only data source over a directory
// of NDJSON run files (the ones -stream-file writes, or `stream receive`
// stores). It never starts a run and never writes. It follows a file that is
// still growing, so a run in progress can be watched without that run knowing
// the server exists.
//
// The directory is read on every request and a file is parsed only from where
// the last request stopped, so a standing server costs nothing between
// requests and a restart loses nothing.
type DirServer struct {
	Dir        string
	Token      string
	CORSOrigin string
	Version    string
	// Stale is how long a run without a terminal event may go unwritten
	// before it is reported as incomplete (default 2 minutes). A live run
	// writes at least a point per second.
	Stale time.Duration
	// Poll is how often a followed file is checked for new lines (default
	// 200 ms).
	Poll time.Duration
	// MaxClients caps concurrent SSE clients (default 32).
	MaxClients int
	// Warn receives a line about a file that cannot be served, once per file
	// and problem. Nil discards them.
	Warn func(string)

	mu      sync.Mutex
	files   map[string]*fileState
	warned  map[string]bool
	clients atomic.Int32
}

// fileState is what is known about one run file: enough for the index and the
// result without keeping the events in memory.
type fileState struct {
	path    string
	offset  int64 // bytes parsed so far, always at a line boundary
	mtime   time.Time
	first   *Envelope
	count   int
	lastSeq int64
	term    *Envelope
	err     error // the file is not a valid run file
}

func (s *DirServer) stale() time.Duration {
	if s.Stale > 0 {
		return s.Stale
	}
	return 2 * time.Minute
}

func (s *DirServer) poll() time.Duration {
	if s.Poll > 0 {
		return s.Poll
	}
	return 200 * time.Millisecond
}

func (s *DirServer) maxClients() int32 {
	if s.MaxClients > 0 {
		return int32(s.MaxClients)
	}
	return 32
}

func (s *DirServer) warn(key, msg string) {
	s.mu.Lock()
	if s.warned == nil {
		s.warned = map[string]bool{}
	}
	seen := s.warned[key]
	s.warned[key] = true
	s.mu.Unlock()
	if !seen && s.Warn != nil {
		s.Warn(msg)
	}
}

// refresh brings the state of one file up to date and returns a copy. A file
// that shrank was replaced by a new run and is read again from the start.
func (s *DirServer) refresh(path string, info os.FileInfo) fileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = map[string]*fileState{}
	}
	st := s.files[path]
	if st == nil || info.Size() < st.offset {
		st = &fileState{path: path}
		s.files[path] = st
	}
	st.mtime = info.ModTime()
	if st.err != nil || st.term != nil || info.Size() == st.offset {
		return *st
	}
	f, err := os.Open(path)
	if err != nil {
		return *st
	}
	defer f.Close()
	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		return *st
	}
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			// No newline yet: the writer is in the middle of a line.
			break
		}
		st.offset += int64(len(line))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev Envelope
		if jerr := json.Unmarshal(line, &ev); jerr != nil {
			st.err = fmt.Errorf("event %d: %w", st.count+1, jerr)
			break
		}
		if ev.Schema != Schema || ValidRunID(ev.RunID) != nil || ev.Seq < 1 || ev.Type == "" {
			st.err = fmt.Errorf("event %d is not a %s event", st.count+1, Schema)
			break
		}
		if st.first == nil {
			if ev.Type != TypeRunStarted {
				st.err = fmt.Errorf("the first event is %q, not %s", ev.Type, TypeRunStarted)
				break
			}
			first := ev
			st.first = &first
		} else if ev.RunID != st.first.RunID {
			st.err = fmt.Errorf("event %d has run id %q, the file started with %q", st.count+1, ev.RunID, st.first.RunID)
			break
		}
		st.count++
		st.lastSeq = ev.Seq
		if ev.Terminal() {
			t := ev
			st.term = &t
			break
		}
	}
	return *st
}

// scan returns the valid run files in the directory by run id. When two files
// hold the same run id, the one written last wins. A missing directory is an
// empty one: it may appear when the first run starts.
func (s *DirServer) scan() map[string]fileState {
	out := map[string]fileState{}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return out
	}
	live := map[string]bool{}
	for _, e := range entries {
		// Regular files only: a symlink could point anywhere.
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".ndjson") {
			continue
		}
		path := filepath.Join(s.Dir, e.Name())
		// Stat the file itself. The entry's own Info() is what the directory
		// listing holds, and on Windows that is not updated while the writer
		// still has the file open: a run in progress would show size 0 and
		// never be read.
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		live[path] = true
		st := s.refresh(path, info)
		if st.err != nil {
			s.warn(path+st.err.Error(), fmt.Sprintf("serve: skipping %s: %v", e.Name(), st.err))
			continue
		}
		if st.first == nil {
			continue // empty, or the first line has not been completed yet
		}
		id := st.first.RunID
		if prev, dup := out[id]; dup {
			s.warn("dup"+id, fmt.Sprintf("serve: %s and %s have the same run id %s; serving the newer", filepath.Base(prev.path), e.Name(), id))
			if prev.mtime.After(st.mtime) {
				continue
			}
		}
		out[id] = st
	}
	// Forget files that are gone.
	s.mu.Lock()
	for p := range s.files {
		if !live[p] {
			delete(s.files, p)
		}
	}
	s.mu.Unlock()
	return out
}

func (s *DirServer) summary(st fileState) RunSummary {
	sum := summarize(*st.first, st.count, st.term)
	if st.term == nil && time.Since(st.mtime) > s.stale() {
		sum.Status = "errored"
		sum.Incomplete = true
	}
	return sum
}

// Handler returns the HTTP handler.
func (s *DirServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":         "litmus-lite",
			"version":      s.Version,
			"mode":         "serve",
			"schemas":      []string{Schema},
			"capabilities": []string{"runs", "events", "result", "follow"},
		})
	})
	mux.HandleFunc("GET /v1/runs", s.list)
	mux.HandleFunc("GET /v1/runs/{id}/events", s.events)
	mux.HandleFunc("GET /v1/runs/{id}/result", s.result)
	return guard(s.Token, s.CORSOrigin, mux)
}

func (s *DirServer) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status, label := q.Get("status"), q.Get("label")
	out := []RunSummary{}
	for _, st := range s.scan() {
		sum := s.summary(st)
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
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].RunID < out[j].RunID
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *DirServer) result(w http.ResponseWriter, r *http.Request) {
	st, ok := s.scan()[r.PathValue("id")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown run"})
		return
	}
	if st.term == nil || st.term.Type != TypeRunFinished {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run has no result"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(st.term.Data)
}

func (s *DirServer) events(w http.ResponseWriter, r *http.Request) {
	st, ok := s.scan()[r.PathValue("id")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown run"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	ended := st.term != nil || s.summary(st).Incomplete
	if ended && after >= st.lastSeq {
		// A browser EventSource reconnects when the stream closes. 204 tells
		// it there is nothing more, so it stops.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.clients.Add(1) > s.maxClients() {
		s.clients.Add(-1)
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "too many clients"})
		return
	}
	defer s.clients.Add(-1)

	f, err := os.Open(st.path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run file is gone"})
		return
	}
	defer f.Close()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	s.follow(r.Context(), f, st.path, after, w, flusher)
}

// follow sends the events of an open run file with seq > after, then waits
// for the file to grow until the run ends, the file goes quiet for longer
// than Stale, it is replaced, or the client leaves.
func (s *DirServer) follow(ctx context.Context, f *os.File, path string, after int64, w io.Writer, fl http.Flusher) {
	br := bufio.NewReaderSize(f, 1<<20)
	var partial []byte
	var consumed int64
	lastWrite, lastBeat := time.Now(), time.Now()
	for {
		chunk, err := br.ReadBytes('\n')
		consumed += int64(len(chunk))
		if err == nil {
			line := bytes.TrimSpace(append(partial, chunk...))
			partial = nil
			if len(line) == 0 {
				continue
			}
			var ev Envelope
			if json.Unmarshal(line, &ev) != nil {
				return
			}
			if ev.Seq > after {
				if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, line); err != nil {
					return
				}
				fl.Flush()
				lastBeat = time.Now()
			}
			if ev.Terminal() {
				return
			}
			lastWrite = time.Now()
			continue
		}
		if !errors.Is(err, io.EOF) {
			return
		}
		partial = append(partial, chunk...)

		// Caught up. Wait for the writer.
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.poll()):
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() < consumed {
			return // removed, or replaced by a new run
		}
		if info.Size() > consumed {
			lastWrite = time.Now()
		} else if time.Since(lastWrite) > s.stale() && time.Since(info.ModTime()) > s.stale() {
			return // the writer is gone
		}
		if time.Since(lastBeat) > 15*time.Second {
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
			lastBeat = time.Now()
		}
	}
}

// ListenDir checks the bind rule and listens, for the serve command.
func ListenDir(addr, token string) (net.Listener, error) {
	if err := CheckBind(addr, token); err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}
