package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// SchemaVersion is the version of the report.json format this build writes.
// A report without the field was written before it existed and is read as
// version 1. Later versions only add fields; a change that removes or renames
// one is a new number, and a reader refuses a number it does not know.
//
//	1  no schemaVersion field
//	2  adds schemaVersion, hypotheses[].noData and hypotheses[].allowNoData
const SchemaVersion = 2

type Result struct {
	SchemaVersion      int                 `json:"schemaVersion,omitempty"`
	Name               string              `json:"name"`
	StartedAt          time.Time           `json:"startedAt"`
	Elapsed            time.Duration       `json:"elapsed_ns"`
	Phases             []Phase             `json:"phases"`
	FaultLog           []FaultEvent        `json:"faultLog"`
	Probes             []probe.Sample      `json:"probes"`
	TimeSeries         []Point             `json:"timeseries"`
	Hypotheses         []HypothesisVerdict `json:"hypotheses"`
	Passed             bool                `json:"passed"`
	Warnings           []string            `json:"warnings,omitempty"`
	Load               json.RawMessage     `json:"load,omitempty"`
	LoadGenerator      *LoadGenerator      `json:"loadGenerator,omitempty"`
	Prometheus         []Point             `json:"prometheus,omitempty"`
	ObservedVsInjected []KV                `json:"observedVsInjected"`
	Recovery           time.Duration       `json:"recovery_ns"`
}

// LoadGenerator records which load-generator binary ran in a composed run:
// with several copies of a tool on one machine, a reader can tell which one
// produced the load series. Path is the local file and stays in report.json;
// the event stream leaves it out (stream.BuildFinished).
type LoadGenerator struct {
	Tool    string `json:"tool"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Via     string `json:"via,omitempty"`
}

type KV struct {
	Name     string `json:"name"`
	Injected string `json:"injected"`
	Observed string `json:"observed"`
}

type Phase struct {
	Name         string        `json:"name"`
	From         time.Time     `json:"from"`
	To           time.Time     `json:"to"`
	ErrorRate    float64       `json:"errorRate"`
	Availability float64       `json:"availability"`
	P50          time.Duration `json:"p50_ns"`
	P95          time.Duration `json:"p95_ns"`
	P99          time.Duration `json:"p99_ns"`
	RPS          float64       `json:"rps"`
	Samples      int           `json:"samples"`
	Failed       int           `json:"failed"`
}

type FaultEvent struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Start     time.Time      `json:"start"`
	End       time.Time      `json:"end"`
	Tunables  map[string]any `json:"tunables,omitempty"`
	Intensity float64        `json:"intensity"`
	Rollback  string         `json:"rollback"`
}

type Point struct {
	T            time.Time `json:"t"`
	OffsetS      float64   `json:"offsetS"`
	Intensity    float64   `json:"intensity"`
	P95Ms        float64   `json:"p95Ms"`
	ErrorRate    float64   `json:"errorRate"`
	Availability float64   `json:"availability"`
	RPS          float64   `json:"rps"`
	Phase        string    `json:"phase"`
}

type HypothesisVerdict struct {
	Name     string `json:"name"`
	Metric   string `json:"metric"`
	Operator string `json:"operator"`
	Limit    string `json:"limit"`
	Observed string `json:"observed"`
	Passed   bool   `json:"passed"`
	// NoData is set when the window the hypothesis judges had nothing to
	// measure. Such a verdict fails unless the hypothesis sets allowNoData.
	NoData      bool `json:"noData,omitempty"`
	AllowNoData bool `json:"allowNoData,omitempty"`
}

func WriteJSON(path string, res *Result) error {
	if res.SchemaVersion == 0 {
		res.SchemaVersion = SchemaVersion
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func LoadJSON(path string) (*Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("%s has schemaVersion %d; this litmus-lite reads up to %d. Upgrade litmus-lite to read it", path, r.SchemaVersion, SchemaVersion)
	}
	return &r, nil
}

func Percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func inRange(t, from, to time.Time) bool {
	return !t.Before(from) && !t.After(to)
}

func Summarize(name string, from, to time.Time, samples []probe.Sample) Phase {
	ph := Phase{Name: name, From: from, To: to}
	var lats []time.Duration
	for _, s := range samples {
		if !inRange(s.At, from, to) {
			continue
		}
		ph.Samples++
		lats = append(lats, s.Latency)
		if !s.OK {
			ph.Failed++
		}
	}
	if ph.Samples > 0 {
		ph.ErrorRate = float64(ph.Failed) / float64(ph.Samples)
		ph.Availability = 1 - ph.ErrorRate
		ph.P50 = Percentile(lats, 0.50)
		ph.P95 = Percentile(lats, 0.95)
		ph.P99 = Percentile(lats, 0.99)
		sec := to.Sub(from).Seconds()
		if sec <= 0 {
			sec = 1
		}
		ph.RPS = float64(ph.Samples) / sec
	}
	return ph
}

// RecoveryInfo is what the samples after the faults ended say about recovery.
type RecoveryInfo struct {
	// Time is how long after the faults ended the service was healthy again.
	// When it never was, it is the time to the last sample.
	Time time.Duration
	// Recovered is true when the service was healthy again by the end of the
	// run: three healthy samples in a row, or, when there were fewer than
	// three samples, all of them.
	Recovered bool
	// Samples is the number of samples after the faults ended.
	Samples int
}

func HypothesisEval(h scenario.Hypothesis, inject Phase, rec RecoveryInfo) HypothesisVerdict {
	v := HypothesisVerdict{Name: h.Name, Metric: h.Metric, Operator: h.Operator, Limit: h.Value, AllowNoData: h.AllowNoData}
	limit, err := h.Limit()
	if err != nil {
		// Validate refuses such a scenario, so this is only reached by a
		// scenario built in code. It must still never pass.
		v.Observed = "invalid limit"
		return v
	}
	var observed float64
	unit := ""
	noData := false
	switch h.Metric {
	case "error_rate":
		observed = inject.ErrorRate * 100
		unit = "%"
		noData = inject.Samples == 0
	case "availability":
		observed = inject.Availability * 100
		unit = "%"
		noData = inject.Samples == 0
	case "p50":
		observed = float64(inject.P50) / float64(time.Millisecond)
		unit = "ms"
		noData = inject.Samples == 0
	case "p95":
		observed = float64(inject.P95) / float64(time.Millisecond)
		unit = "ms"
		noData = inject.Samples == 0
	case "p99":
		observed = float64(inject.P99) / float64(time.Millisecond)
		unit = "ms"
		noData = inject.Samples == 0
	case "rps":
		observed = inject.RPS
		noData = inject.Samples == 0
	case "recovery":
		observed = rec.Time.Seconds()
		unit = "s"
		noData = rec.Samples == 0
		if !noData && !rec.Recovered {
			// The service was not healthy again by the end of the run. The
			// time to the last sample is not a recovery time, and it must not
			// satisfy "recovery <= 30s" just because the run was shorter.
			v.Observed = "not recovered"
			return v
		}
	}
	if noData {
		v.NoData = true
		v.Observed = "no data"
		v.Passed = h.AllowNoData
		return v
	}
	v.Observed = FormatObs(observed, unit)
	v.Passed = Cmp(observed, h.Operator, limit)
	return v
}

func FormatObs(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	return s + unit
}

func Cmp(observed float64, op string, limit float64) bool {
	switch op {
	case "<":
		return observed < limit
	case "<=":
		return observed <= limit
	case ">":
		return observed > limit
	case ">=":
		return observed >= limit
	default:
		return false
	}
}

// RecoveryTime is Recovery(...).Time, kept for callers that only show it.
func RecoveryTime(samples []probe.Sample, injectEnd time.Time) time.Duration {
	return Recovery(samples, injectEnd).Time
}

// Recovery reads the samples taken at or after injectEnd, oldest first, and
// finds when the service was healthy again (three OK samples in a row).
func Recovery(samples []probe.Sample, injectEnd time.Time) RecoveryInfo {
	var after []probe.Sample
	for _, s := range samples {
		if !s.At.Before(injectEnd) {
			after = append(after, s)
		}
	}
	sort.SliceStable(after, func(i, j int) bool { return after[i].At.Before(after[j].At) })
	info := RecoveryInfo{Samples: len(after)}
	if len(after) == 0 {
		return info
	}
	streak := 0
	var first time.Time
	for _, s := range after {
		if !s.OK {
			streak = 0
			continue
		}
		streak++
		if streak == 1 {
			first = s.At
		}
		if streak >= 3 {
			info.Time = first.Sub(injectEnd)
			info.Recovered = true
			return info
		}
	}
	// Fewer than three healthy samples in a row. With fewer than three
	// samples in all, healthy to the end is the most that can be asked.
	if len(after) < 3 && streak == len(after) {
		info.Time = first.Sub(injectEnd)
		info.Recovered = true
		return info
	}
	info.Time = after[len(after)-1].At.Sub(injectEnd)
	return info
}

func BucketSeries(start time.Time, samples []probe.Sample, faults []FaultEvent, end time.Time) []Point {
	if !end.After(start) {
		end = start.Add(time.Second)
	}
	var pts []Point
	for t := start; !t.After(end); t = t.Add(time.Second) {
		pts = append(pts, BucketPoint(start, t, samples, faults))
	}
	return pts
}

// BucketPoint computes the one-second point that starts at t. BucketSeries
// calls it for every bucket, and the live stream calls it as each bucket
// closes, so a live point and the final series come from the same code.
func BucketPoint(start, t time.Time, samples []probe.Sample, faults []FaultEvent) Point {
	ph := Summarize("bucket", t, t.Add(time.Second), samples)
	p := Point{
		T:            t,
		OffsetS:      t.Sub(start).Seconds(),
		P95Ms:        float64(ph.P95.Milliseconds()),
		ErrorRate:    ph.ErrorRate,
		Availability: ph.Availability,
		RPS:          ph.RPS,
		Phase:        phaseName(t, faults, start),
	}
	for _, f := range faults {
		if inRange(t, f.Start, f.End) {
			p.Intensity = f.Intensity
		}
	}
	return p
}

func phaseName(t time.Time, faults []FaultEvent, start time.Time) string {
	for _, f := range faults {
		if inRange(t, f.Start, f.End) {
			return "inject"
		}
		if t.After(f.End) {
			return "recover"
		}
	}
	if t.Equal(start) || t.After(start) {
		return "steady"
	}
	return "steady"
}

func IntensityText(f FaultEvent) string {
	switch f.Kind {
	case "http.latency", "http.timeout":
		return fmt.Sprintf("%.0f ms", f.Intensity)
	case "http.status-inject":
		return fmt.Sprintf("%.0f%% status", f.Intensity)
	case "disk.fill", "memory.hog":
		return fmt.Sprintf("%.0f bytes", f.Intensity)
	case "cpu.hog":
		return fmt.Sprintf("%.0f workers", f.Intensity)
	case "process.pause", "process.kill", "docker.pause":
		return "applied"
	case "failpoint":
		return "hook armed"
	default:
		return fmt.Sprintf("%.0f", f.Intensity)
	}
}

func LatencyKind(kind string) bool {
	return kind == "http.latency" || kind == "http.timeout"
}

func ObservedVsInjected(faults []FaultEvent, inject Phase) []KV {
	var out []KV
	for _, f := range faults {
		name := f.Name
		obs := fmt.Sprintf("%d ms p95, %.1f%% errors", inject.P95.Milliseconds(), inject.ErrorRate*100)
		if LatencyKind(f.Kind) {
			name = f.Name + " intensity vs probe p95"
			obs = fmt.Sprintf("%d ms p95", inject.P95.Milliseconds())
		}
		out = append(out, KV{Name: name, Injected: IntensityText(f), Observed: obs})
	}
	return out
}
