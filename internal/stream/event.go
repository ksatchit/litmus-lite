// Package stream defines the litmus-lite run event stream: the event format,
// recording files (NDJSON), and the HTTP surface that serves recordings.
//
// The format is documented in docs/reference/streaming.md and described by
// docs/stream/schema/v1.json. This package has no dependency on the engine.
package stream

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
)

// Schema is the value of the schema field on every event.
const Schema = "litmus-lite.io/stream/v1"

// Event types.
const (
	TypeRunStarted   = "run.started"
	TypePhaseChanged = "phase.changed"
	TypeFaultStarted = "fault.started"
	TypeFaultEnded   = "fault.ended"
	TypePoint        = "point"
	TypeProbeSample  = "probe.sample"
	TypeLoadStarted  = "load.started"
	TypeLoadPoint    = "load.point"
	TypeLoadFinished = "load.finished"
	TypeWarning      = "warning"
	TypeRunFinished  = "run.finished"
	TypeRunFailed    = "run.failed"
)

// Envelope wraps every event. Data stays raw so that readers keep fields and
// event types they do not know.
type Envelope struct {
	Schema    string            `json:"schema"`
	RunID     string            `json:"runId"`
	Seq       int64             `json:"seq"`
	TS        time.Time         `json:"ts"`
	OffsetS   float64           `json:"offsetS"`
	Labels    map[string]string `json:"labels,omitempty"`
	Synthetic bool              `json:"synthetic,omitempty"`
	Type      string            `json:"type"`
	Data      json.RawMessage   `json:"data"`
}

// Terminal reports whether the event ends a run.
func (e Envelope) Terminal() bool {
	return e.Type == TypeRunFinished || e.Type == TypeRunFailed
}

// RunStarted is the data of run.started.
type RunStarted struct {
	Name       string           `json:"name"`
	Target     *Target          `json:"target,omitempty"`
	Baseline   string           `json:"baseline,omitempty"`
	Recover    string           `json:"recover,omitempty"`
	Faults     []FaultSummary   `json:"faults"`
	Probes     []string         `json:"probes,omitempty"`
	Hypotheses []HypothesisSpec `json:"hypotheses"`
	Load       *LoadInfo        `json:"load,omitempty"`
	Version    string           `json:"version,omitempty"`
	Scenario   string           `json:"scenario,omitempty"`
}

type Target struct {
	Kind    string `json:"kind,omitempty"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type FaultSummary struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Duration string         `json:"duration,omitempty"`
	Tunables map[string]any `json:"tunables,omitempty"`
}

type HypothesisSpec struct {
	Name     string `json:"name"`
	Metric   string `json:"metric"`
	Operator string `json:"operator"`
	Limit    string `json:"limit"`
}

type LoadInfo struct {
	Tool  string `json:"tool,omitempty"`
	Start string `json:"start,omitempty"`
}

// PhaseChanged is the data of phase.changed.
type PhaseChanged struct {
	Phase string `json:"phase"`
}

// FaultStarted is the data of fault.started.
type FaultStarted struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Intensity float64        `json:"intensity"`
	Tunables  map[string]any `json:"tunables,omitempty"`
}

// FaultEnded is the data of fault.ended.
type FaultEnded struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Rollback string `json:"rollback"`
}

// Point is the data of point. OffsetS is the start of the one-second bucket.
// ErrorRate and Availability are fractions between 0 and 1.
type Point struct {
	OffsetS      float64 `json:"offsetS"`
	Phase        string  `json:"phase"`
	P95Ms        float64 `json:"p95Ms"`
	ErrorRate    float64 `json:"errorRate"`
	Availability float64 `json:"availability"`
	RPS          float64 `json:"rps"`
	Intensity    float64 `json:"intensity"`
}

// ProbeSample is the data of probe.sample.
type ProbeSample struct {
	Name      string  `json:"name"`
	OK        bool    `json:"ok"`
	LatencyMs float64 `json:"latencyMs"`
	Status    int     `json:"status,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// LoadStarted is the data of load.started. StartOffsetS is seconds from the
// run's time zero.
type LoadStarted struct {
	Tool         string  `json:"tool"`
	StartOffsetS float64 `json:"startOffsetS"`
}

// LoadPoint is the data of load.point: one second of the generator's own
// progress. OffsetS is seconds from the run's time zero, like Point.OffsetS.
// The percentiles, requests and failed are left out when the tool cannot
// report them. ErrorRate is a fraction between 0 and 1.
type LoadPoint struct {
	OffsetS   float64  `json:"offsetS"`
	RPS       float64  `json:"rps"`
	ErrorRate float64  `json:"errorRate"`
	P50Ms     *float64 `json:"p50Ms,omitempty"`
	P95Ms     *float64 `json:"p95Ms,omitempty"`
	P99Ms     *float64 `json:"p99Ms,omitempty"`
	Requests  int64    `json:"requests,omitempty"`
	Failed    int64    `json:"failed,omitempty"`
}

// LoadSeriesPoint is one second of the generator's whole-run series.
type LoadSeriesPoint struct {
	OffsetS   float64 `json:"offsetS"`
	RPS       float64 `json:"rps"`
	ErrorRate float64 `json:"errorRate"`
}

// LoadFinished is the data of load.finished. Series is the generator's whole
// series; it can be empty. Error is set when the generator failed.
type LoadFinished struct {
	Series []LoadSeriesPoint `json:"series"`
	Error  string            `json:"error,omitempty"`
}

// Warning is the data of warning.
type Warning struct {
	Message string `json:"message"`
}

// Finished is the data of run.finished: the report.json content plus the
// diagnose text. Raw probe samples are left out of it unless the stream was
// asked to carry samples, because they are also available as probe.sample.
type Finished struct {
	report.Result
	Probes    []probe.Sample `json:"probes,omitempty"`
	Diagnosis string         `json:"diagnosis"`
}

// Failed is the data of run.failed.
type Failed struct {
	Reason string         `json:"reason"`
	Result *report.Result `json:"result,omitempty"`
	Faults []FaultSummary `json:"faults,omitempty"`
}

var secretKey = regexp.MustCompile(`(?i)(token|secret|password|key)`)

// Redact returns a deep copy of tunables with the value of every key that
// looks like a credential replaced by "***". Values are never emitted as-is
// for such keys, at any depth.
func Redact(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if secretKey.MatchString(k) {
			out[k] = "***"
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return Redact(t)
	case []any:
		cp := make([]any, len(t))
		for i := range t {
			cp[i] = redactValue(t[i])
		}
		return cp
	default:
		return v
	}
}

func durationString(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.Round(100 * time.Millisecond).String()
}
