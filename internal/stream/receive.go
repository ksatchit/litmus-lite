package stream

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxIngestBody is the largest body Receiver accepts.
const MaxIngestBody = 8 << 20

// Receiver is the reference push receiver (docs/reference/streaming.md, "What an
// ingest endpoint must do"). It stores one NDJSON file per run in Dir, in the
// layout `-stream-file auto` writes, so `serve` can read them.
//
// It is a worked example of the ingest rules, not a production collector: it
// keeps the sequence numbers it has seen in memory and has no retention.
type Receiver struct {
	Dir   string
	Token string
	// Print, when set, is called with a one-line summary of every stored event.
	Print func(line string)
	// Delay waits before answering a batch, FailPercent answers 503 for that
	// share of batches. Both exist so a sender's retry and backpressure can be
	// tested; they are development flags.
	Delay       time.Duration
	FailPercent int

	mu   sync.Mutex
	runs map[string]*receivedRun
	rnd  *rand.Rand
}

type receivedRun struct {
	seen     map[int64]bool
	terminal bool
}

// NewReceiver creates the directory and reads the sequence numbers already
// stored in it, so a restarted receiver still treats a retried batch as
// duplicate.
func NewReceiver(dir string) (*Receiver, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Receiver{Dir: dir, runs: map[string]*receivedRun{}, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
	files, _ := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var e Envelope
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.RunID == "" {
				continue
			}
			run := r.run(e.RunID)
			run.seen[e.Seq] = true
			if e.Terminal() {
				run.terminal = true
			}
		}
	}
	return r, nil
}

func (r *Receiver) run(id string) *receivedRun {
	run := r.runs[id]
	if run == nil {
		run = &receivedRun{seen: map[int64]bool{}}
		r.runs[id] = run
	}
	return run
}

// Handler serves POST /v1/ingest and GET /v1/health.
func (r *Receiver) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/ingest", r.ingest)
	return mux
}

func (r *Receiver) ingest(w http.ResponseWriter, req *http.Request) {
	// 1. Token.
	if r.Token != "" {
		got := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(r.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
	}
	// Development switches, after authentication so a wrong token is never
	// answered with a retryable status.
	if r.Delay > 0 {
		select {
		case <-time.After(r.Delay):
		case <-req.Context().Done():
			return
		}
	}
	if r.FailPercent > 0 {
		r.mu.Lock()
		fail := r.rnd.Intn(100) < r.FailPercent
		r.mu.Unlock()
		if fail {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable (-fail-percent)"})
			return
		}
	}
	// 2. Body.
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, MaxIngestBody))
	if err != nil {
		code := http.StatusBadRequest
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			code = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	var batch []Envelope
	var raws [][]byte
	hdrRun := req.Header.Get("X-Litmus-Lite-Run-Id")
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), MaxIngestBody)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Envelope
		if err := json.Unmarshal(line, &e); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("line %d: %v", n, err)})
			return
		}
		if err := checkIngestEvent(e, hdrRun); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("line %d: %v", n, err)})
			return
		}
		batch = append(batch, e)
		raws = append(raws, append([]byte(nil), line...))
	}
	if err := sc.Err(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// 3. Store, skipping anything already seen.
	accepted, dups, ignored, err := r.store(batch, raws)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 4. Answer.
	writeJSON(w, http.StatusOK, map[string]int{"accepted": accepted, "duplicates": dups, "ignored": ignored})
}

func checkIngestEvent(e Envelope, hdrRun string) error {
	if e.Schema != Schema {
		return fmt.Errorf("schema %q, want %q", e.Schema, Schema)
	}
	if err := ValidRunID(e.RunID); err != nil {
		return err
	}
	if hdrRun != "" && hdrRun != e.RunID {
		return fmt.Errorf("runId %q does not match X-Litmus-Lite-Run-Id %q", e.RunID, hdrRun)
	}
	if e.Seq < 1 {
		return fmt.Errorf("seq %d: must be 1 or more", e.Seq)
	}
	if e.Type == "" {
		return fmt.Errorf("missing type")
	}
	if len(e.Data) == 0 || !json.Valid(e.Data) {
		return fmt.Errorf("missing or invalid data")
	}
	if e.TS.IsZero() {
		return fmt.Errorf("missing ts")
	}
	return nil
}

// store appends new events to each run's file. Events after a run's terminal
// event are ignored: a finished run is closed.
func (r *Receiver) store(batch []Envelope, raws [][]byte) (accepted, dups, ignored int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	files := map[string]*os.File{}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	for i, e := range batch {
		run := r.run(e.RunID)
		if run.seen[e.Seq] {
			dups++
			continue
		}
		if run.terminal {
			ignored++
			continue
		}
		f := files[e.RunID]
		if f == nil {
			f, err = os.OpenFile(filepath.Join(r.Dir, e.RunID+".ndjson"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return accepted, dups, ignored, err
			}
			files[e.RunID] = f
		}
		if _, err = f.Write(append(append([]byte(nil), raws[i]...), '\n')); err != nil {
			return accepted, dups, ignored, err
		}
		run.seen[e.Seq] = true
		if e.Terminal() {
			run.terminal = true
		}
		accepted++
		if r.Print != nil {
			r.Print(fmt.Sprintf("%s #%d %s", e.RunID, e.Seq, e.Type))
		}
	}
	return accepted, dups, ignored, nil
}
