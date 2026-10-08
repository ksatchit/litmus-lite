package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ksatchit/litmus-lite/internal/probe"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type Result struct {
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
	Prometheus         []Point             `json:"prometheus,omitempty"`
	ObservedVsInjected []KV                `json:"observedVsInjected"`
	Recovery           time.Duration       `json:"recovery_ns"`
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
}

func WriteJSON(path string, res *Result) error {
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

func HypothesisEval(h scenario.Hypothesis, inject Phase, recovery time.Duration) HypothesisVerdict {
	v := HypothesisVerdict{Name: h.Name, Metric: h.Metric, Operator: h.Operator, Limit: h.Value}
	var observed float64
	unit := ""
	switch h.Metric {
	case "error_rate":
		observed = inject.ErrorRate * 100
		unit = "%"
	case "availability":
		observed = inject.Availability * 100
		unit = "%"
	case "p50":
		observed = float64(inject.P50.Milliseconds())
		unit = "ms"
	case "p95":
		observed = float64(inject.P95.Milliseconds())
		unit = "ms"
	case "p99":
		observed = float64(inject.P99.Milliseconds())
		unit = "ms"
	case "rps":
		observed = inject.RPS
	case "recovery":
		observed = recovery.Seconds()
		unit = "s"
	}
	limit := ParseLimit(h.Value)
	v.Observed = FormatObs(observed, unit)
	v.Passed = Cmp(observed, h.Operator, limit)
	return v
}

func ParseLimit(s string) float64 {
	n := strings.TrimSpace(s)
	n = strings.TrimSuffix(n, "%")
	n = strings.TrimSuffix(n, "ms")
	n = strings.TrimSuffix(n, "s")
	f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
	return f
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

func RecoveryTime(samples []probe.Sample, injectEnd time.Time) time.Duration {
	streak := 0
	var first time.Time
	for _, s := range samples {
		if s.At.Before(injectEnd) {
			continue
		}
		if s.OK {
			streak++
			if streak == 1 {
				first = s.At
			}
			if streak >= 3 {
				return first.Sub(injectEnd)
			}
		} else {
			streak = 0
		}
	}
	if len(samples) == 0 {
		return 0
	}
	last := samples[len(samples)-1].At
	if last.Before(injectEnd) {
		return 0
	}
	return last.Sub(injectEnd)
}

func BucketSeries(start time.Time, samples []probe.Sample, faults []FaultEvent, end time.Time) []Point {
	if !end.After(start) {
		end = start.Add(time.Second)
	}
	var pts []Point
	for t := start; !t.After(end); t = t.Add(time.Second) {
		windowEnd := t.Add(time.Second)
		ph := Summarize("bucket", t, windowEnd, samples)
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
		pts = append(pts, p)
	}
	return pts
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
