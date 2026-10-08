package compose

import (
	"encoding/json"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/report"
)

func TestMergeSeriesFromVegaLoadJSON(t *testing.T) {
	raw := json.RawMessage(`{
  "executor": "constant-vus",
  "error_rate": 0.1,
  "total": 100,
  "failed": 10,
  "latency": { "p95_ns": 850000000 },
  "time_series": [
    { "offset_ns": 0, "requests": 40, "failed": 0, "rps": 40.0, "error_rate": 0 },
    { "offset_ns": 1000000000, "requests": 38, "failed": 8, "rps": 38.0, "error_rate": 0.21 }
  ]
}`)
	dst := []report.Point{
		{OffsetS: 3, RPS: 5, ErrorRate: 0.01, P95Ms: 12, Intensity: 0},
		{OffsetS: 4, RPS: 5, ErrorRate: 0.01, P95Ms: 800, Intensity: 800},
	}
	MergeSeries(dst, raw, 3)
	if dst[0].RPS != 40 || dst[0].ErrorRate != 0 {
		t.Fatalf("bucket 0: %+v", dst[0])
	}
	if dst[1].RPS != 38 || dst[1].ErrorRate != 0.21 {
		t.Fatalf("bucket 1: %+v", dst[1])
	}
	if dst[1].P95Ms != 800 {
		t.Fatalf("probe p95 should win over overall vegaload p95, got %v", dst[1].P95Ms)
	}
	if dst[1].Availability != 1-0.21 {
		t.Fatalf("availability %v", dst[1].Availability)
	}
}

func TestParseEmpty(t *testing.T) {
	v, err := Parse(nil)
	if err != nil || len(v.TimeSeries) != 0 {
		t.Fatal(err, v)
	}
}
