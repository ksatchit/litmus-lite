package compose

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func point(off, rps int) string {
	return `{"schema":"vegaload.io/stream/v1","runId":"r","seq":1,"type":"point","data":{"offsetS":` +
		itoa(off) + `,"rps":` + itoa(rps) + `.5,"requests":10,"failed":1,"errorRate":0.1,"p95Ms":42.5}}` + "\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

type collected struct {
	mu  sync.Mutex
	pts []LivePoint
}

func (c *collected) add(p LivePoint) { c.mu.Lock(); c.pts = append(c.pts, p); c.mu.Unlock() }
func (c *collected) n() int          { c.mu.Lock(); defer c.mu.Unlock(); return len(c.pts) }

// The file appears late, a line arrives in two writes, other event types and
// junk are skipped, and stop reads what was written in its last moment, even a
// last line with no newline.
func TestTailReadsPointsAsTheyAreWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.ndjson")
	var c collected
	stop := tailVegaLoad(path, c.add)

	time.Sleep(200 * time.Millisecond) // the file does not exist yet
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	write := func(s string) {
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"schema":"vegaload.io/stream/v1","runId":"r","seq":0,"type":"run.started","data":{}}` + "\n")
	write("not json at all\n")
	write(point(0, 100))
	half := point(1, 101)
	write(half[:40])
	time.Sleep(300 * time.Millisecond)
	if c.n() != 1 {
		t.Fatalf("%d points while a line is half written, want 1", c.n())
	}
	write(half[40:])
	write(`{"schema":"litmus-lite.io/stream/v1","type":"point","data":{"offsetS":9}}` + "\n") // another family
	write(point(2, 102)[:len(point(2, 102))-1])                                               // no newline at the end
	stop()

	if c.n() != 3 {
		t.Fatalf("%d points, want 3: %+v", c.n(), c.pts)
	}
	for i, p := range c.pts {
		if p.OffsetS != float64(i) || p.RPS != float64(100+i)+0.5 || p.Requests != 10 || p.Failed != 1 || p.ErrorRate != 0.1 {
			t.Fatalf("point %d: %+v", i, p)
		}
		if p.P95Ms == nil || *p.P95Ms != 42.5 || p.P50Ms != nil || p.P99Ms != nil {
			t.Fatalf("point %d: percentiles %+v %+v %+v", i, p.P50Ms, p.P95Ms, p.P99Ms)
		}
	}
}

func TestTailWithNoFileStopsCleanly(t *testing.T) {
	var c collected
	stop := tailVegaLoad(filepath.Join(t.TempDir(), "never"), c.add)
	stop()
	stop() // stopping twice is fine
	if c.n() != 0 {
		t.Fatal("no file, no points")
	}
}
