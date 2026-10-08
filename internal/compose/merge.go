package compose

import (
	"encoding/json"

	"github.com/ksatchit/litmus-lite/internal/report"
)

// VegaLoadResult is the subset of vegaload run -output json / -out we need.
// Field names match github.com/vegaload/vegaload/internal/report.Result.
type VegaLoadResult struct {
	ErrorRate  float64         `json:"error_rate"`
	Latency    VegaLoadLatency `json:"latency"`
	TimeSeries []VegaLoadPoint `json:"time_series"`
	Total      int64           `json:"total"`
	Failed     int64           `json:"failed"`
}

type VegaLoadLatency struct {
	P95Ns int64 `json:"p95_ns"`
}

type VegaLoadPoint struct {
	OffsetNs  int64   `json:"offset_ns"`
	RPS       float64 `json:"rps"`
	ErrorRate float64 `json:"error_rate"`
}

func Parse(raw json.RawMessage) (VegaLoadResult, error) {
	var v VegaLoadResult
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// MergeSeries copies VegaLoad per-second RPS and error rate onto dst.
// shiftS is seconds from the chaos run start until vegaload started (typically inject).
// VegaLoad does not emit per-second p95; overall latency.p95_ns is applied only
// to buckets that have no probe p95 yet.
func MergeSeries(dst []report.Point, raw json.RawMessage, shiftS float64) []report.Point {
	vl, err := Parse(raw)
	if err != nil || len(vl.TimeSeries) == 0 || len(dst) == 0 {
		return dst
	}
	p95ms := float64(vl.Latency.P95Ns) / 1e6
	for _, lp := range vl.TimeSeries {
		t := shiftS + float64(lp.OffsetNs)/1e9
		idx := nearest(dst, t)
		if idx < 0 {
			continue
		}
		dst[idx].RPS = lp.RPS
		dst[idx].ErrorRate = lp.ErrorRate
		dst[idx].Availability = 1 - lp.ErrorRate
		if dst[idx].P95Ms == 0 && p95ms > 0 {
			dst[idx].P95Ms = p95ms
		}
	}
	return dst
}

func nearest(dst []report.Point, t float64) int {
	best := -1
	bestD := 0.75
	for i, p := range dst {
		d := p.OffsetS - t
		if d < 0 {
			d = -d
		}
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}
