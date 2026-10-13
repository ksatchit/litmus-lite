package mcp

import (
	"encoding/json"
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

func TestEvalCLIDestWindowsExe(t *testing.T) {
	win := evalCLIDest(5112, "windows")
	if !strings.HasSuffix(win, "litmus-lite-mcp-eval-5112.exe") {
		t.Fatal(win)
	}
	unix := evalCLIDest(5112, "linux")
	if strings.HasSuffix(unix, ".exe") || !strings.HasSuffix(unix, "litmus-lite-mcp-eval-5112") {
		t.Fatal(unix)
	}
}

func TestCLIArgsForwardSafetyFlags(t *testing.T) {
	argv, err := cliArgs("run_test", map[string]any{
		"file":         "a.chaos.yaml",
		"yes":          true,
		"allow-pid":    "42",
		"allow-target": "example.com",
		"out":          "out.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	for _, want := range []string{"-yes", "-allow-pid 42", "-allow-target example.com", "-out out.json", "a.chaos.yaml"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	argv, err = cliArgs("validate_scenario", map[string]any{"file": "a.chaos.yaml", "allow-pid": "7"})
	if err != nil {
		t.Fatal(err)
	}
	got = strings.Join(argv, " ")
	if !strings.Contains(got, "-allow-pid 7") || !strings.HasSuffix(got, "a.chaos.yaml") {
		t.Fatal(got)
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

// TestToolSchemasMatchCLIArgs keeps the advertised schemas honest: every
// property a tool declares must change the CLI argv, and every required
// property must be declared.
func TestToolSchemasMatchCLIArgs(t *testing.T) {
	for _, tl := range allTools() {
		if tl.Desc == "" {
			t.Errorf("%s: empty description", tl.Name)
		}
		for _, r := range tl.Required {
			if _, ok := tl.Props[r]; !ok {
				t.Errorf("%s: required %q is not a declared property", tl.Name, r)
			}
		}
		if tl.ReadFile {
			continue
		}
		args := map[string]any{}
		for name, raw := range tl.Props {
			if raw.(map[string]any)["type"] == "boolean" {
				args[name] = true
			} else {
				args[name] = "S_" + name
			}
		}
		argv, err := cliArgs(tl.Name, args)
		if err != nil {
			t.Fatalf("%s: %v", tl.Name, err)
		}
		got := strings.Join(argv, " ")
		for name, raw := range tl.Props {
			want := "S_" + name
			if raw.(map[string]any)["type"] == "boolean" {
				want = "-" + name
			}
			if !strings.Contains(got, want) {
				t.Errorf("%s: property %q is advertised but not forwarded to the CLI (%s)", tl.Name, name, got)
			}
		}
	}
}

func TestToolDefsAdvertiseSchemas(t *testing.T) {
	for _, d := range toolDefs() {
		schema, ok := d["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("%v: inputSchema must be an object schema", d["name"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Fatalf("%v: inputSchema.properties missing", d["name"])
		}
	}
	argv, err := cliArgs("hub_import", map[string]any{"id": "http.status-inject", "beside": "d", "name": "n"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); !strings.Contains(got, "-name n") || !strings.HasSuffix(got, "http.status-inject") {
		t.Fatal(got)
	}
}

func TestInitializeReportsVersion(t *testing.T) {
	old := Version
	Version = "9.9.9-test"
	defer func() { Version = old }()
	resp := handle(req{Method: "initialize"})
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), `"version":"9.9.9-test"`) {
		t.Fatal(string(b))
	}
}
