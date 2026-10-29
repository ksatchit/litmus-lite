package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/probe"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func hyp(metric, op, value string) scenario.Hypothesis {
	return scenario.Hypothesis{Name: "h", Metric: metric, Operator: op, Value: value}
}

func TestHypothesisUsesTheUnitOfItsMetric(t *testing.T) {
	ph := Phase{Samples: 10, P95: 1500 * time.Millisecond}
	// p95 is 1.5 s. "<= 2s" is 2000 ms and passes; it used to be read as 2 ms.
	if v := HypothesisEval(hyp("p95", "<=", "2s"), ph, RecoveryInfo{}); !v.Passed {
		t.Fatalf("p95 1.5s <= 2s failed: %+v", v)
	}
	if v := HypothesisEval(hyp("p95", "<=", "1s"), ph, RecoveryInfo{}); v.Passed {
		t.Fatalf("p95 1.5s <= 1s passed: %+v", v)
	}
	// Recovery of 0.5 s against "<= 500ms": it used to be read as 500 seconds.
	rec := RecoveryInfo{Time: 400 * time.Millisecond, Recovered: true, Samples: 5}
	if v := HypothesisEval(hyp("recovery", "<=", "500ms"), Phase{}, rec); !v.Passed {
		t.Fatalf("recovery 0.4s <= 500ms failed: %+v", v)
	}
	rec.Time = 600 * time.Millisecond
	if v := HypothesisEval(hyp("recovery", "<=", "500ms"), Phase{}, rec); v.Passed {
		t.Fatalf("recovery 0.6s <= 500ms passed: %+v", v)
	}
}

func TestHypothesisNeverPassesOnAValueItCannotRead(t *testing.T) {
	ph := Phase{Samples: 10, Failed: 10, ErrorRate: 1} // everything failed
	for _, h := range []scenario.Hypothesis{
		hyp("availability", ">=", "ninety-nine"), // used to become ">= 0": true
		hyp("p95", "<=", "oops"),
		hyp("error_rate", "<", ""),
	} {
		if v := HypothesisEval(h, ph, RecoveryInfo{}); v.Passed {
			t.Errorf("%+v passed", v)
		}
	}
}

func TestHypothesisOverNoSamplesIsNotAPass(t *testing.T) {
	empty := Phase{} // the window held no sample: nothing was measured
	for _, h := range []scenario.Hypothesis{
		hyp("error_rate", "<", "5%"), // used to pass: 0 < 5
		hyp("availability", ">=", "0"),
		hyp("p95", "<", "1s"),
		hyp("p50", "<", "1s"),
		hyp("p99", "<", "1s"),
		hyp("rps", ">=", "0"),
	} {
		v := HypothesisEval(h, empty, RecoveryInfo{})
		if v.Passed || !v.NoData || v.Observed != "no data" {
			t.Errorf("%s over no samples: %+v", h.Metric, v)
		}
	}
	h := hyp("error_rate", "<", "5%")
	h.AllowNoData = true
	if v := HypothesisEval(h, empty, RecoveryInfo{}); !v.Passed || !v.NoData || !v.AllowNoData {
		t.Fatalf("allowNoData did not allow it: %+v", v)
	}
	// With samples, allowNoData changes nothing.
	h.Value = "1%"
	if v := HypothesisEval(h, Phase{Samples: 4, Failed: 2, ErrorRate: 0.5}, RecoveryInfo{}); v.Passed || v.NoData {
		t.Fatalf("%+v", v)
	}
}

func TestRecoveryHypothesisNeedsARecovery(t *testing.T) {
	notBack := RecoveryInfo{Time: 5 * time.Second, Recovered: false, Samples: 25}
	// The run ended 5 s after the fault with the service still down. "<= 30s"
	// must not be satisfied by the length of the recovery window.
	v := HypothesisEval(hyp("recovery", "<=", "30s"), Phase{}, notBack)
	if v.Passed || v.Observed != "not recovered" {
		t.Fatalf("%+v", v)
	}
	h := hyp("recovery", "<=", "30s")
	h.AllowNoData = true
	if v := HypothesisEval(h, Phase{}, notBack); v.Passed {
		t.Fatal("allowNoData must not excuse a service that did not come back")
	}
	// Nothing sampled after the fault is no data, which allowNoData may excuse.
	if v := HypothesisEval(hyp("recovery", "<=", "30s"), Phase{}, RecoveryInfo{}); v.Passed || !v.NoData {
		t.Fatalf("%+v", v)
	}
	if v := HypothesisEval(h, Phase{}, RecoveryInfo{}); !v.Passed {
		t.Fatalf("%+v", v)
	}
}

func TestRecovery(t *testing.T) {
	end := time.Unix(1000, 0)
	at := func(ms int, ok bool) probe.Sample {
		return probe.Sample{At: end.Add(time.Duration(ms) * time.Millisecond), OK: ok}
	}
	cases := []struct {
		name    string
		samples []probe.Sample
		want    RecoveryInfo
	}{
		{"none", nil, RecoveryInfo{}},
		{"only before the end", []probe.Sample{at(-100, true)}, RecoveryInfo{}},
		{"back after three", []probe.Sample{at(-50, false), at(100, false), at(200, true), at(300, true), at(400, true)},
			RecoveryInfo{Time: 200 * time.Millisecond, Recovered: true, Samples: 4}},
		{"never back", []probe.Sample{at(100, false), at(200, false), at(300, false), at(400, false)},
			RecoveryInfo{Time: 400 * time.Millisecond, Samples: 4}},
		{"up then down again", []probe.Sample{at(100, true), at(200, true), at(300, false), at(400, false)},
			RecoveryInfo{Time: 400 * time.Millisecond, Samples: 4}},
		{"short window, healthy", []probe.Sample{at(100, true), at(200, true)},
			RecoveryInfo{Time: 100 * time.Millisecond, Recovered: true, Samples: 2}},
		{"short window, last one down", []probe.Sample{at(100, true), at(200, false)},
			RecoveryInfo{Time: 200 * time.Millisecond, Samples: 2}},
		{"out of order", []probe.Sample{at(400, true), at(100, false), at(300, true), at(200, true)},
			RecoveryInfo{Time: 200 * time.Millisecond, Recovered: true, Samples: 4}},
	}
	for _, c := range cases {
		if got := Recovery(c.samples, end); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestReportSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := WriteJSON(path, &Result{Name: "n", Passed: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil || raw["schemaVersion"] != float64(SchemaVersion) {
		t.Fatalf("schemaVersion not written: %v %s", err, b)
	}
	if _, err := LoadJSON(path); err != nil {
		t.Fatal(err)
	}
	// A report from before the field existed is read as it always was.
	old := filepath.Join(dir, "old.json")
	_ = os.WriteFile(old, []byte(`{"name":"n","passed":true,"phases":[]}`), 0o644)
	if r, err := LoadJSON(old); err != nil || r.SchemaVersion != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	// A report from a newer litmus-lite is refused, saying why.
	newer := filepath.Join(dir, "new.json")
	_ = os.WriteFile(newer, []byte(`{"schemaVersion":99,"name":"n"}`), 0o644)
	if _, err := LoadJSON(newer); err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("a report from the future was read: %v", err)
	}
}
