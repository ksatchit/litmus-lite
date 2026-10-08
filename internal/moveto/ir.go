package moveto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// Experiment is the vendor-neutral IR aligned with Litmus 4.0 / Harness RT primitives.
type Experiment struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	Target     Target       `json:"target"`
	Faults     []Fault      `json:"faults"`
	Probes     []Probe      `json:"probes"`
	Hypotheses []Hypothesis `json:"hypotheses"`
	Duration   string       `json:"duration"`
	Rollback   string       `json:"rollback"`
}

type Target struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type Fault struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Duration string         `json:"duration"`
	Tunables map[string]any `json:"tunables,omitempty"`
}

type Probe struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Mode   string         `json:"mode"`
	URL    string         `json:"url,omitempty"`
	Expect map[string]any `json:"expect,omitempty"`
}

type Hypothesis struct {
	Name     string `json:"name"`
	Metric   string `json:"metric"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

func FromScenario(s *scenario.Scenario) Experiment {
	ex := Experiment{
		APIVersion: "litmus-lite.io/v1",
		Kind:       "Experiment",
		Name:       s.Metadata.Name,
		Target:     Target{Kind: s.Target.Kind, BaseURL: s.Target.BaseURL},
		Rollback:   s.Rollback,
	}
	var max time.Duration
	for _, f := range s.Faults {
		ex.Faults = append(ex.Faults, Fault{Name: f.Name, Kind: f.Kind, Duration: f.Duration, Tunables: f.Params})
		if d := f.DurationTime(); d > max {
			max = d
		}
	}
	ex.Duration = max.String()
	for _, p := range append(s.SteadyState, s.Probes...) {
		ex.Probes = append(ex.Probes, Probe{Name: p.Name, Type: p.Type, Mode: p.Mode, URL: p.URL, Expect: p.Expect})
	}
	for _, h := range s.Hypotheses {
		ex.Hypotheses = append(ex.Hypotheses, Hypothesis(h))
	}
	return ex
}

func JSON(ex Experiment) []byte {
	b, _ := json.MarshalIndent(ex, "", "  ")
	return append(b, '\n')
}

func Push(adapter string, ex Experiment) error {
	switch adapter {
	case "ir", "":
		return nil
	case "litmus", "harness":
		return fmt.Errorf("adapter %q is not implemented until Litmus 4.0 field names are published; IR is ready (%d faults, %d probes)", adapter, len(ex.Faults), len(ex.Probes))
	default:
		return fmt.Errorf("unknown adapter %q", adapter)
	}
}
