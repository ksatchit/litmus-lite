// Package litmus writes a Litmus 4.0-shaped experiment document from move-to IR.
// It is a local file encoder: no network, no ChaosEngine or ChaosExperiment CRDs.
package litmus

import (
	"encoding/json"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/moveto"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "litmuschaos.io/v4"
	Kind       = "Experiment"
)

// Experiment is the Litmus 4.0-shaped document (control-plane primitives, not CRDs).
type Experiment struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

type Metadata struct {
	Name string `json:"name" yaml:"name"`
}

type Spec struct {
	Target     Target       `json:"target" yaml:"target"`
	Faults     []Fault      `json:"faults" yaml:"faults"`
	Probes     []Probe      `json:"probes" yaml:"probes"`
	Hypotheses []Hypothesis `json:"hypotheses,omitempty" yaml:"hypotheses,omitempty"`
	Duration   string       `json:"duration,omitempty" yaml:"duration,omitempty"`
	Rollback   string       `json:"rollback" yaml:"rollback"`
}

type Target struct {
	Kind    string `json:"kind" yaml:"kind"`
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
}

type Fault struct {
	Name     string         `json:"name" yaml:"name"`
	Kind     string         `json:"kind" yaml:"kind"`
	Duration string         `json:"duration" yaml:"duration"`
	Tunables map[string]any `json:"tunables,omitempty" yaml:"tunables,omitempty"`
}

type Probe struct {
	Name     string         `json:"name" yaml:"name"`
	Type     string         `json:"type" yaml:"type"`
	Mode     string         `json:"mode" yaml:"mode"`
	URL      string         `json:"url,omitempty" yaml:"url,omitempty"`
	Command  string         `json:"command,omitempty" yaml:"command,omitempty"`
	Method   string         `json:"method,omitempty" yaml:"method,omitempty"`
	Interval string         `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout  string         `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Expect   map[string]any `json:"expect,omitempty" yaml:"expect,omitempty"`
}

type Hypothesis struct {
	Name     string `json:"name" yaml:"name"`
	Metric   string `json:"metric" yaml:"metric"`
	Operator string `json:"operator" yaml:"operator"`
	Value    string `json:"value" yaml:"value"`
}

func FromIR(ex moveto.Experiment) Experiment {
	doc := Experiment{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: ex.Name},
		Spec: Spec{
			Target:   Target{Kind: ex.Target.Kind, BaseURL: ex.Target.BaseURL},
			Duration: ex.Duration,
			Rollback: ex.Rollback,
		},
	}
	for _, f := range ex.Faults {
		doc.Spec.Faults = append(doc.Spec.Faults, Fault{
			Name: f.Name, Kind: f.Kind, Duration: f.Duration, Tunables: f.Tunables,
		})
	}
	for _, p := range ex.Probes {
		doc.Spec.Probes = append(doc.Spec.Probes, Probe{
			Name: p.Name, Type: probeType(p.Type), Mode: probeMode(p.Mode),
			URL: p.URL, Command: p.Command, Method: p.Method,
			Interval: p.Interval, Timeout: p.Timeout, Expect: p.Expect,
		})
	}
	for _, h := range ex.Hypotheses {
		doc.Spec.Hypotheses = append(doc.Spec.Hypotheses, Hypothesis(h))
	}
	return doc
}

func YAML(doc Experiment) []byte {
	b, err := yaml.Marshal(doc)
	if err != nil {
		return nil
	}
	return b
}

func JSON(doc Experiment) []byte {
	b, _ := json.MarshalIndent(doc, "", "  ")
	return append(b, '\n')
}

func probeType(t string) string {
	switch strings.ToLower(t) {
	case "http", "httpprobe":
		return "http"
	case "cmd", "command", "cmdprobe":
		return "cmd"
	default:
		return t
	}
}

func probeMode(m string) string {
	s := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(m), "_", "-"))
	switch s {
	case "sot":
		return "SOT"
	case "eot":
		return "EOT"
	case "continuous":
		return "Continuous"
	case "on-chaos", "onchaos":
		return "OnChaos"
	default:
		return m
	}
}
