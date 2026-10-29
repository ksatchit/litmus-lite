package probe

import (
	"fmt"
	"strings"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// Lint reports what is wrong with a probe without stopping it from running:
// an expect key the probe type does not read, and an expect.status that is
// not a number. Each of these used to be ignored, so the probe passed on a
// check the author thought it made.
func Lint(p scenario.Probe) []string {
	var read []string
	switch strings.ToLower(p.Type) {
	case "cmd", "command":
		read = []string{"contains"}
	default:
		read = []string{"status", "body"}
	}
	var out []string
	for k := range p.Expect {
		if !contains(read, k) {
			out = append(out, fmt.Sprintf("probe %q (%s): unknown expect key %q, it is ignored; this type reads %s",
				p.Name, typeName(p), k, strings.Join(read, ", ")))
		}
	}
	if v, ok := p.Expect["status"]; ok && statusOf(v) == 0 {
		out = append(out, fmt.Sprintf("probe %q: expect.status %v is not a number, so the probe expects 200", p.Name, v))
	}
	return out
}

func typeName(p scenario.Probe) string {
	if p.Type == "" {
		return "http"
	}
	return p.Type
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// statusOf is the status code a probe expects, or 0 when expect.status is not
// a number.
func statusOf(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	}
	return 0
}
