package faults

import (
	"strings"
	"testing"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

func fault(kind string, params map[string]any) scenario.Fault {
	return scenario.Fault{Name: "f", Kind: kind, Duration: "1s", Params: params}
}

func TestUnknownParams(t *testing.T) {
	got := UnknownParams(fault("http.latency", map[string]any{
		"listen": "127.0.0.1:1", "upstream": "127.0.0.1:2",
		"dealy":          "800ms",                                   // a typo
		"statusOverride": map[string]any{"percnt": 50, "code": 503}, // a typo inside a map
	}))
	want := "dealy,statusOverride.percnt"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
	if got := UnknownParams(fault("http.latency", map[string]any{"listen": "a", "upstream": "b", "delay": "1s", "jitter": "1s"})); got != nil {
		t.Fatalf("known params reported: %v", got)
	}
	if got := UnknownParams(fault("no.such.kind", map[string]any{"x": 1})); got != nil {
		t.Fatalf("an unregistered kind is not this check's business: %v", got)
	}
}

func TestLintSuggestsTheKeyYouMeant(t *testing.T) {
	sc := &scenario.Scenario{Faults: []scenario.Fault{
		fault("http.latency", map[string]any{"listen": "a", "upstream": "b", "dealy": "1s"}),
		fault("cpu.hog", map[string]any{"worker": 2}),
		fault("disk.fill", map[string]any{"sizee": "1MB", "pth": "/tmp/x"}),
	}}
	got := strings.Join(Lint(sc), "\n")
	for _, want := range []string{`unknown param "dealy"`, `did you mean "delay"`, `did you mean "workers"`, `did you mean "size"`, `did you mean "path"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Every kind that ships declares its params, so that the check cannot go
// quiet for a kind by forgetting to say what it reads. A new kind must too.
func TestEveryRegisteredKindDeclaresItsParams(t *testing.T) {
	for _, in := range List() {
		if _, ok := in.(ParamDeclarer); !ok {
			t.Errorf("%s does not implement ParamDeclarer", in.Name())
		}
	}
}

func TestHTTPFaultRefusesValuesThatWouldInjectNothing(t *testing.T) {
	base := func(extra map[string]any) scenario.Fault {
		p := map[string]any{"listen": "127.0.0.1:1", "upstream": "127.0.0.1:2"}
		for k, v := range extra {
			p[k] = v
		}
		return fault("http.latency", p)
	}
	bad := map[string]map[string]any{
		"delay":               {"delay": 800}, // no unit: used to inject nothing
		"delay text":          {"delay": "slow"},
		"jitter":              {"jitter": "10"},
		"timeout":             {"timeout": "soon"},
		"negative":            {"delay": "-1s"},
		"percent":             {"percent": "most"},
		"percent range":       {"percent": 150},
		"code":                {"code": "oops"},
		"code range":          {"code": 42},
		"statusOverride type": {"statusOverride": "yes"},
		"override percent":    {"statusOverride": map[string]any{"percent": 101, "code": 503}},
		"override code":       {"statusOverride": map[string]any{"percent": 50, "code": 1000}},
	}
	for name, extra := range bad {
		if err := (httpProxy{}).Validate(base(extra)); err == nil {
			t.Errorf("%s: accepted %v", name, extra)
		}
	}
	good := []map[string]any{
		{"delay": "800ms"},
		{"delay": "2s", "jitter": "100ms", "timeout": "5s"},
		{"percent": 50, "code": 503},
		{"percent": "25", "code": "502"},
		{"statusOverride": map[string]any{"percent": 50, "code": 503}},
	}
	for _, extra := range good {
		if err := (httpProxy{}).Validate(base(extra)); err != nil {
			t.Errorf("%v refused: %v", extra, err)
		}
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		d    int
	}{{"", "", 0}, {"delay", "delay", 0}, {"dealy", "delay", 2}, {"delai", "delay", 1}, {"abc", "", 3}} {
		if got := editDistance(c.a, c.b); got != c.d {
			t.Errorf("%q %q: %d, want %d", c.a, c.b, got, c.d)
		}
	}
}
