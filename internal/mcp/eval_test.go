package mcp

import (
	"strings"
	"testing"
)

func TestEvalSuite(t *testing.T) {
	p, f, report := Eval()
	if f != 0 {
		t.Fatalf("passed=%d failed=%d\n%s", p, f, report)
	}
	if p < 7 {
		t.Fatalf("expected versioned fixtures, passed=%d\n%s", p, report)
	}
	for _, name := range []string{
		"v1/initialize",
		"v1/tools-list",
		"v1/validate_scenario",
		"v1/list_faults",
		"v1/hub_import",
		"v1/create_scenario",
		"v1/tools-cli-equivalent",
	} {
		if !strings.Contains(report, "PASS "+name) {
			t.Fatalf("missing %s\n%s", name, report)
		}
	}
}

func TestToolsHaveCLI(t *testing.T) {
	if err := toolsHaveCLI(); err != nil {
		t.Fatal(err)
	}
	if _, ok := toolByName("no_such_tool"); ok {
		t.Fatal("unknown tool should not map")
	}
}
