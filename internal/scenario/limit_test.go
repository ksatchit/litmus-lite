package scenario

import (
	"strings"
	"testing"
)

func TestHypothesisLimit(t *testing.T) {
	cases := []struct {
		metric, value string
		want          float64
	}{
		{"error_rate", "5", 5},
		{"error_rate", "5%", 5},
		{"error_rate", " 0.5 % ", 0.5},
		{"availability", "99.9%", 99.9},
		{"p95", "500", 500},        // a bare number is milliseconds
		{"p95", "500ms", 500},      //
		{"p95", "2s", 2000},        // a second is not two milliseconds
		{"p99", "1.5s", 1500},      //
		{"p50", "1m", 60000},       //
		{"recovery", "5", 5},       // a bare number is seconds
		{"recovery", "5s", 5},      //
		{"recovery", "500ms", 0.5}, // not 500 seconds
		{"recovery", "1m", 60},
		{"rps", "120", 120},
	}
	for _, c := range cases {
		got, err := Hypothesis{Name: "h", Metric: c.metric, Value: c.value}.Limit()
		if err != nil || got != c.want {
			t.Errorf("%s %q: got %v, %v; want %v", c.metric, c.value, got, err, c.want)
		}
	}
}

func TestHypothesisLimitRefusesWhatItCannotRead(t *testing.T) {
	bad := []struct{ metric, value string }{
		{"availability", "ninety-nine"},
		{"availability", "abc"},
		{"error_rate", "5s"},   // a duration is not a percentage
		{"error_rate", "150%"}, // not a percentage
		{"error_rate", "-1"},   // negative
		{"error_rate", "NaN"},  // ParseFloat would accept it
		{"error_rate", "Inf"},  //
		{"error_rate", "1e2"},  // no exponents
		{"p95", "oops"},
		{"p95", "5%"}, // a percentage is not a latency
		{"p95", "-5ms"},
		{"recovery", "5%"}, // the case the PRD names
		{"recovery", "soon"},
		{"rps", "100/s"},
		{"rps", "5s"},
	}
	for _, c := range bad {
		got, err := Hypothesis{Name: "h", Metric: c.metric, Value: c.value}.Limit()
		if err == nil {
			t.Errorf("%s %q was accepted as %v", c.metric, c.value, got)
			continue
		}
		if !strings.Contains(err.Error(), c.value) || !strings.Contains(err.Error(), c.metric) {
			t.Errorf("%s %q: the error should name both: %v", c.metric, c.value, err)
		}
	}
}

const modeBase = `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - { name: a, kind: http.latency, duration: 1s }
`

func TestParseRefusesAMalformedHypothesisValue(t *testing.T) {
	_, err := Parse([]byte(modeBase + `
probes: [{ name: p, type: http, url: "http://127.0.0.1:1/" }]
hypotheses:
  - { name: up, metric: availability, operator: ">=", value: ninety-nine }
`))
	if err == nil || !strings.Contains(err.Error(), "ninety-nine") {
		t.Fatalf("a hypothesis whose value is not a number was accepted: %v", err)
	}
}

func TestParseRefusesAnUnknownProbeMode(t *testing.T) {
	for _, group := range []string{"probes", "steadyState"} {
		_, err := Parse([]byte(modeBase + group + `: [{ name: p, type: http, url: "http://127.0.0.1:1/", mode: on-chaos-ish }]`))
		if err == nil || !strings.Contains(err.Error(), "on-chaos-ish") {
			t.Errorf("%s: an unknown mode was accepted (it used to be skipped silently): %v", group, err)
		}
	}
	for _, mode := range []string{"continuous", "on-chaos", "SOT", "EOT"} {
		if _, err := Parse([]byte(modeBase + `probes: [{ name: p, type: http, url: "http://127.0.0.1:1/", mode: ` + mode + ` }]`)); err != nil {
			t.Errorf("mode %s refused: %v", mode, err)
		}
	}
}

func TestParseReportsKeysNothingReads(t *testing.T) {
	s, err := Parse([]byte(modeBase + `
probes:
  - { name: p, type: http, url: "http://127.0.0.1:1/", intervall: 50ms }
hypotheses:
  - { name: h, metric: p95, operator: "<", value: 500ms, allowNodata: true }
`))
	if err != nil {
		t.Fatalf("a misspelled key must warn, not fail: %v", err)
	}
	got := strings.Join(s.Warnings, "\n")
	for _, want := range []string{"intervall", "allowNodata"} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning names %q: %q", want, got)
		}
	}
}

func TestParseHasNoWarningsForAValidScenario(t *testing.T) {
	s, err := Parse([]byte(modeBase + `
probes: [{ name: p, type: http, url: "http://127.0.0.1:1/", interval: 50ms, expect: { status: 200 } }]
hypotheses:
  - { name: h, metric: p95, operator: "<", value: 500ms, allowNoData: true }
`))
	if err != nil || len(s.Warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, s.Warnings)
	}
	if !s.Hypotheses[0].AllowNoData {
		t.Fatal("allowNoData was not read")
	}
}
