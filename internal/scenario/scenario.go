package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const APIVersion = "litmus-lite.io/v1"

// Values of probe mode.
//
//	continuous  every interval, from the start of the run to the end of recovery
//	on-chaos    every interval, and only while the faults are armed
//	SOT         once before the faults, as steadyState probes do; a failure
//	            stops the run
//	EOT         once after recovery
const (
	ModeContinuous = "continuous"
	ModeOnChaos    = "on-chaos"
	ModeSOT        = "SOT"
	ModeEOT        = "EOT"
)

// Values of load.start.
const (
	LoadStartInject   = "inject"
	LoadStartBaseline = "baseline"
)

type Scenario struct {
	APIVersion    string         `yaml:"apiVersion" json:"apiVersion"`
	Kind          string         `yaml:"kind" json:"kind"`
	Metadata      Metadata       `yaml:"metadata" json:"metadata"`
	Target        Target         `yaml:"target" json:"target"`
	SteadyState   []Probe        `yaml:"steadyState" json:"steadyState,omitempty"`
	Faults        []Fault        `yaml:"faults" json:"faults"`
	Probes        []Probe        `yaml:"probes" json:"probes,omitempty"`
	Hypotheses    []Hypothesis   `yaml:"hypotheses" json:"hypotheses,omitempty"`
	Load          *LoadSpec      `yaml:"load" json:"load,omitempty"`
	Observability *Observability `yaml:"observability" json:"observability,omitempty"`
	Rollback      string         `yaml:"rollback" json:"rollback,omitempty"`
	Baseline      string         `yaml:"baseline" json:"baseline,omitempty"`
	Recover       string         `yaml:"recover" json:"recover,omitempty"`

	// Warnings are problems Parse found that do not stop the scenario from
	// running, for example a key that no field of the format has (a typo such
	// as "intervall"). run and validate show them; -strict makes them errors.
	Warnings []string `yaml:"-" json:"-"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Target struct {
	Kind    string `yaml:"kind" json:"kind"`
	BaseURL string `yaml:"baseUrl" json:"baseUrl,omitempty"`
}

type Fault struct {
	Name     string         `yaml:"name" json:"name"`
	Kind     string         `yaml:"kind" json:"kind"`
	Duration string         `yaml:"duration" json:"duration"`
	Params   map[string]any `yaml:"params" json:"params,omitempty"`
}

type Probe struct {
	Name     string         `yaml:"name" json:"name"`
	Type     string         `yaml:"type" json:"type"`
	URL      string         `yaml:"url" json:"url,omitempty"`
	Method   string         `yaml:"method" json:"method,omitempty"`
	Command  string         `yaml:"command" json:"command,omitempty"`
	Interval string         `yaml:"interval" json:"interval,omitempty"`
	Timeout  string         `yaml:"timeout" json:"timeout,omitempty"`
	Mode     string         `yaml:"mode" json:"mode,omitempty"`
	Expect   map[string]any `yaml:"expect" json:"expect,omitempty"`
}

type Hypothesis struct {
	Name     string `yaml:"name" json:"name"`
	Metric   string `yaml:"metric" json:"metric"`
	Operator string `yaml:"operator" json:"operator"`
	Value    string `yaml:"value" json:"value"`
	// AllowNoData lets the hypothesis pass when the window it judges has no
	// samples (or, for recovery, none after the fault). Without it, no data
	// is a failure: a run that measured nothing proved nothing.
	AllowNoData bool `yaml:"allowNoData" json:"allowNoData,omitempty"`
}

type LoadSpec struct {
	// Tool is vegaload (default), k6, or command. command runs load.command
	// and expects normalized time_series JSON on stdout.
	Tool string `yaml:"tool" json:"tool,omitempty"`
	// Start is inject (default: the generator starts with the fault) or
	// baseline (it starts with the probes). Its series is aligned to its own
	// start either way.
	Start    string   `yaml:"start" json:"start,omitempty"`
	Scenario string   `yaml:"scenario" json:"scenario,omitempty"`
	Command  string   `yaml:"command" json:"command,omitempty"`
	Args     []string `yaml:"args" json:"args,omitempty"`
}

type Observability struct {
	Prometheus *Prometheus `yaml:"prometheus" json:"prometheus,omitempty"`
}

type Prometheus struct {
	URL     string   `yaml:"url" json:"url"`
	Queries []string `yaml:"queries" json:"queries,omitempty"`
}

func Load(path string) (*Scenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	s.Warnings = unknownFields(b)
	if err := s.Normalize(); err != nil {
		return nil, err
	}
	return &s, nil
}

// unknownFields lists the keys in the document that no field of the scenario
// format has. The document has already decoded without them, so a typo such
// as "operater" or "intervall" would otherwise do nothing and say nothing.
// It reads the document a second time with known fields enforced and keeps
// only the "not found" findings, so that anything else stays the first
// decode's business. Params and expect maps are free-form here; the fault and
// probe kinds check their own keys.
func unknownFields(b []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var again Scenario
	err := dec.Decode(&again)
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return nil
	}
	var out []string
	for _, m := range te.Errors {
		if strings.Contains(m, "not found in type") {
			out = append(out, "scenario: "+m)
		}
	}
	return out
}

func (s *Scenario) Normalize() error {
	if s.APIVersion == "" {
		s.APIVersion = APIVersion
	}
	if s.Kind == "" {
		s.Kind = "Scenario"
	}
	if s.Rollback == "" {
		s.Rollback = "always"
	}
	if s.Baseline == "" {
		s.Baseline = "3s"
	}
	if s.Recover == "" {
		s.Recover = "5s"
	}
	if s.Load != nil {
		s.Load.Tool = strings.ToLower(strings.TrimSpace(s.Load.Tool))
		if s.Load.Tool == "" {
			s.Load.Tool = "vegaload"
		}
		s.Load.Start = strings.ToLower(strings.TrimSpace(s.Load.Start))
		if s.Load.Start == "" {
			s.Load.Start = LoadStartInject
		}
	}
	for i := range s.Probes {
		if s.Probes[i].Mode == "" {
			s.Probes[i].Mode = "continuous"
		}
		if s.Probes[i].Interval == "" {
			s.Probes[i].Interval = "200ms"
		}
		if s.Probes[i].Method == "" {
			s.Probes[i].Method = "GET"
		}
	}
	for i := range s.SteadyState {
		if s.SteadyState[i].Mode == "" {
			s.SteadyState[i].Mode = "SOT"
		}
		if s.SteadyState[i].Method == "" {
			s.SteadyState[i].Method = "GET"
		}
	}
	return s.Validate()
}

// Validate checks the scenario's structure.
//
// Fault params and probe expect maps are free-form (map[string]any) here; the
// fault kinds and probe types report keys they do not read (faults.Lint,
// probe.Lint), and the document's own unknown fields are in Warnings.
//
// TODO(strictness): when existing kinds start failing on unknown keys, rather
// than warning (-strict makes them errors), is deliberately deferred; revisit.
func (s *Scenario) Validate() error {
	if s.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q (want %s)", s.APIVersion, APIVersion)
	}
	if !strings.EqualFold(s.Kind, "Scenario") {
		return fmt.Errorf("kind must be Scenario")
	}
	if strings.TrimSpace(s.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if len(s.Faults) == 0 {
		return fmt.Errorf("at least one fault is required")
	}
	if len(s.SteadyState) == 0 && len(s.Probes) == 0 {
		return fmt.Errorf("at least one probe is required (steadyState or probes)")
	}
	if strings.ToLower(s.Rollback) != "always" {
		return fmt.Errorf("rollback must be always")
	}
	for _, f := range s.Faults {
		if f.Name == "" || f.Kind == "" || f.Duration == "" {
			return fmt.Errorf("fault %q needs name, kind, duration", f.Name)
		}
		if _, err := time.ParseDuration(f.Duration); err != nil {
			return fmt.Errorf("fault %s duration: %w", f.Name, err)
		}
	}
	for _, h := range s.Hypotheses {
		switch h.Metric {
		case "error_rate", "p50", "p95", "p99", "availability", "recovery", "rps":
		default:
			return fmt.Errorf("unknown hypothesis metric %q", h.Metric)
		}
		switch h.Operator {
		case "<", "<=", ">", ">=":
		default:
			return fmt.Errorf("hypothesis %q operator must be < <= > >=", h.Name)
		}
		if h.Value == "" {
			return fmt.Errorf("hypothesis %q needs value", h.Name)
		}
		if _, err := h.Limit(); err != nil {
			return err
		}
	}
	for _, group := range []struct {
		name   string
		probes []Probe
	}{{"steadyState", s.SteadyState}, {"probes", s.Probes}} {
		for _, p := range group.probes {
			switch strings.ToLower(p.Type) {
			case "", "http", "cmd", "command":
			default:
				return fmt.Errorf("%s probe %q: unknown type %q (want http or cmd)", group.name, p.Name, p.Type)
			}
			if t := strings.ToLower(p.Type); t == "" || t == "http" {
				if _, err := BodyExpect(p.Expect); err != nil {
					return fmt.Errorf("%s probe %q: %w", group.name, p.Name, err)
				}
			}
			switch p.Mode {
			case "", ModeContinuous, ModeOnChaos, ModeSOT, ModeEOT:
			default:
				return fmt.Errorf("%s probe %q: unknown mode %q (want %s, %s, %s or %s)",
					group.name, p.Name, p.Mode, ModeContinuous, ModeOnChaos, ModeSOT, ModeEOT)
			}
		}
	}
	if s.Load != nil {
		if s.Load.Start != LoadStartInject && s.Load.Start != LoadStartBaseline {
			return fmt.Errorf("load.start %q must be %s or %s", s.Load.Start, LoadStartInject, LoadStartBaseline)
		}
		switch s.Load.Tool {
		case "vegaload", "k6":
			if strings.TrimSpace(s.Load.Scenario) == "" {
				return fmt.Errorf("load.scenario is required for tool %s", s.Load.Tool)
			}
		case "command":
			if strings.TrimSpace(s.Load.Command) == "" {
				return fmt.Errorf("load.command is required for tool command")
			}
		default:
			return fmt.Errorf("unknown load tool %q (want vegaload, k6, or command)", s.Load.Tool)
		}
	}
	if _, err := time.ParseDuration(s.Baseline); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	if _, err := time.ParseDuration(s.Recover); err != nil {
		return fmt.Errorf("recover: %w", err)
	}
	return nil
}

func (f Fault) DurationTime() time.Duration {
	d, _ := time.ParseDuration(f.Duration)
	return d
}

func StringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int(t)) {
			return fmt.Sprintf("%d", int(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func MapParam(params map[string]any, key string) map[string]any {
	if params == nil {
		return nil
	}
	v, ok := params[key]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func FloatParam(params map[string]any, key string, def float64) float64 {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case float64:
		return t
	case string:
		var f float64
		fmt.Sscanf(t, "%f", &f)
		return f
	default:
		return def
	}
}

func IntParam(params map[string]any, key string, def int) int {
	return int(FloatParam(params, key, float64(def)))
}
