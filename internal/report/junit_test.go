package report

import (
	"os"
	"strings"
	"testing"
)

func TestJUnitFailures(t *testing.T) {
	res := &Result{
		Name:   "demo",
		Passed: false,
		Hypotheses: []HypothesisVerdict{
			{Name: "ok", Metric: "error_rate", Operator: "<", Limit: "15%", Observed: "1%", Passed: true},
			{Name: "bad", Metric: "recovery", Operator: "<=", Limit: "5s", Observed: "9s", Passed: false},
		},
	}
	xml := string(JUnitXML(res))
	if !strings.Contains(xml, `failures="1"`) || !strings.Contains(xml, "bad") {
		t.Fatal(xml)
	}
	p := t.TempDir() + "/junit.xml"
	if err := WriteJUnit(p, res); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "testcase") {
		t.Fatal(err, string(b))
	}
}
