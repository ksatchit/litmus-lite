package scenario

import (
	"strings"
	"testing"
)

func probeScenario(expect string) string {
	return `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - { name: a, kind: http.latency, duration: 1s, params: { listen: "127.0.0.1:9", upstream: "127.0.0.1:8" } }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:9/
    expect:
` + expect
}

func TestBodyExpectParsesFromYAML(t *testing.T) {
	s, err := Parse([]byte(probeScenario(`
      status: 200
      body:
        contains: ["ok", "version"]
        notContains: error
        matches: '"id":\s*[0-9]+'
`)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := BodyExpect(s.Probes[0].Expect)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Contains) != 2 || len(c.NotContains) != 1 || len(c.Matches) != 1 || !c.Any() {
		t.Fatalf("%+v", c)
	}
}

func TestBodyExpectRefusesWhatItWouldSkip(t *testing.T) {
	for name, expect := range map[string]string{
		"a number":           "      body: { contains: 5 }",
		"an empty string":    `      body: { contains: "" }`,
		"an empty list":      "      body: { contains: [] }",
		"a list of numbers":  "      body: { contains: [1, 2] }",
		"a bad regexp":       `      body: { matches: "(" }`,
		"a misspelled key":   `      body: { contain: "ok" }`,
		"body as plain text": `      body: ok`,
		"body as a list":     `      body: [ok]`,
	} {
		_, err := Parse([]byte(probeScenario(expect + "\n")))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "expect.body") {
			t.Errorf("%s: the error does not say where: %v", name, err)
		}
	}
}

func TestBodyCheckNamesTheAssertionAndNotTheBody(t *testing.T) {
	c, err := BodyExpect(map[string]any{"body": map[string]any{
		"contains": "ok", "notContains": "boom", "matches": `^\{`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body, want string
		ok         bool
	}{
		{`{"status":"ok"}`, "", true},
		{`{"status":"fine"}`, `body does not contain "ok"`, false},
		{`{"status":"ok","msg":"boom"}`, `body contains "boom", which it must not`, false},
		{`ok`, `body does not match`, false},
	} {
		reason, ok := c.Check([]byte(tc.body))
		if ok != tc.ok || !strings.Contains(reason, tc.want) {
			t.Errorf("%q: ok=%v reason=%q, want ok=%v %q", tc.body, ok, reason, tc.ok, tc.want)
		}
		if !ok && strings.Contains(reason, "status") {
			t.Errorf("the reason quotes the body: %q", reason)
		}
	}
}

func TestNoBodyExpectAssertsNothing(t *testing.T) {
	c, err := BodyExpect(map[string]any{"status": 200})
	if err != nil || c.Any() {
		t.Fatalf("%+v %v", c, err)
	}
}
