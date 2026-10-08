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

func seriesFromVegaLoad(raw json.RawMessage) ([]LoadPoint, error) {
	vl, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	pts := make([]LoadPoint, 0, len(vl.TimeSeries))
	for _, lp := range vl.TimeSeries {
		pts = append(pts, LoadPoint{
			OffsetS:   float64(lp.OffsetNs) / 1e9,
			RPS:       lp.RPS,
			ErrorRate: lp.ErrorRate,
		})
	}
	return pts, nil
}

// MergeSeries copies a VegaLoad time_series onto dst. shiftS is seconds from
// the chaos run start until the generator started (typically inject).
// Probe p95 is left as the latency signal.
func MergeSeries(dst []report.Point, raw json.RawMessage, shiftS float64) []report.Point {
	pts, err := seriesFromVegaLoad(raw)
	if err != nil || len(pts) == 0 || len(dst) == 0 {
		return dst
	}
	return MergePoints(dst, pts, shiftS)
}

// MergePoints copies normalized per-second RPS and error rate onto dst.
func MergePoints(dst []report.Point, pts []LoadPoint, shiftS float64) []report.Point {
	if len(pts) == 0 || len(dst) == 0 {
		return dst
	}
	for _, lp := range pts {
		idx := nearest(dst, shiftS+lp.OffsetS)
		if idx < 0 {
			continue
		}
		dst[idx].RPS = lp.RPS
		dst[idx].ErrorRate = lp.ErrorRate
		dst[idx].Availability = 1 - lp.ErrorRate
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
