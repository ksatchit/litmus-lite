package compose

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// LivePoint is one second of generator progress, as it happens. OffsetS counts
// from when the generator started. The percentiles are pointers because a
// generator that cannot report them leaves them out of the stream.
type LivePoint struct {
	OffsetS   float64
	RPS       float64
	ErrorRate float64
	P50Ms     *float64
	P95Ms     *float64
	P99Ms     *float64
	Requests  int64
	Failed    int64
}

// RunOptions are the parts of a generator run that only matter when the run is
// streamed. The zero value is the old behaviour.
type RunOptions struct {
	// RunID is exported to the generator as LITMUS_LITE_RUN_ID (and, for
	// vegaload, VEGALOAD_RUN_ID) so its own recordings carry the same id.
	RunID string
	// Progress, when set, asks for live points. Only a generator that can write
	// a stream file supports it (today vegaload, found out from its `run -h`):
	// it is started with -stream-file and its points are read as they are written.
	// It is called from one goroutine, in order, and must return quickly.
	Progress func(LivePoint)
}

const vegaLoadSchemaPrefix = "vegaload.io/stream/v1"

// vegaLoadEvent is the part of a vegaload.io/stream/v1 event that is read.
type vegaLoadEvent struct {
	Schema string          `json:"schema"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
}

type vegaLoadPointData struct {
	OffsetS   float64  `json:"offsetS"`
	RPS       float64  `json:"rps"`
	Requests  int64    `json:"requests"`
	Failed    int64    `json:"failed"`
	ErrorRate float64  `json:"errorRate"`
	P50Ms     *float64 `json:"p50Ms"`
	P95Ms     *float64 `json:"p95Ms"`
	P99Ms     *float64 `json:"p99Ms"`
}

// tailVegaLoad reads the NDJSON file vegaload is writing and calls fn for each
// point event. The file may not exist yet. stop drains whatever is left (the
// generator has exited by then, so the file is complete) and returns once fn
// has seen it all. Lines it cannot read are skipped: the final result still
// comes from the generator's -out file.
func tailVegaLoad(path string, fn func(LivePoint)) (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		var (
			f       *os.File
			pending []byte
			buf     = make([]byte, 32*1024)
		)
		defer func() {
			if f != nil {
				f.Close()
			}
		}()
		read := func() {
			if f == nil {
				var err error
				if f, err = os.Open(path); err != nil {
					f = nil
					return
				}
			}
			for {
				n, err := f.Read(buf)
				if n > 0 {
					pending = append(pending, buf[:n]...)
					for {
						i := bytes.IndexByte(pending, '\n')
						if i < 0 {
							break
						}
						handleVegaLoadLine(pending[:i], fn)
						pending = pending[i+1:]
					}
				}
				if err == io.EOF || n == 0 {
					return
				}
			}
		}
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-quit:
				read()
				// A last line without a newline is still a line.
				if len(bytes.TrimSpace(pending)) > 0 {
					handleVegaLoadLine(pending, fn)
				}
				return
			case <-t.C:
				read()
			}
		}
	}()
	return func() {
		once.Do(func() { close(quit) })
		<-done
	}
}

func handleVegaLoadLine(line []byte, fn func(LivePoint)) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var ev vegaLoadEvent
	if json.Unmarshal(line, &ev) != nil || ev.Type != "point" || !strings.HasPrefix(ev.Schema, vegaLoadSchemaPrefix) {
		return
	}
	var d vegaLoadPointData
	if json.Unmarshal(ev.Data, &d) != nil {
		return
	}
	fn(LivePoint{
		OffsetS: d.OffsetS, RPS: d.RPS, ErrorRate: d.ErrorRate,
		P50Ms: d.P50Ms, P95Ms: d.P95Ms, P99Ms: d.P99Ms,
		Requests: d.Requests, Failed: d.Failed,
	})
}
