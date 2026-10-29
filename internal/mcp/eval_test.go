package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		"v1/refuses-agent-yes",
		"v1/refuses-agent-allow-pid",
		"v1/refuses-agent-allow-target",
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

func TestCLIArgsForwardStreamFlags(t *testing.T) {
	argv, err := cliArgs("run_test", map[string]any{
		"file":        "a.chaos.yaml",
		"stream_file": "auto",
		"stream_push": "http://127.0.0.1:7080/v1/ingest, https://b.example.com/v1/ingest",
		"stream_pull": "127.0.0.1:7070",
		"run_id":      "r-1",
		"labels":      "service=checkout, env=ci",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	for _, want := range []string{
		"-stream-file auto",
		"-stream-push http://127.0.0.1:7080/v1/ingest",
		"-stream-push https://b.example.com/v1/ingest",
		"-stream-pull 127.0.0.1:7070",
		"-run-id r-1",
		"-label service=checkout",
		"-label env=ci",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if !strings.HasSuffix(got, "a.chaos.yaml") {
		t.Errorf("the file must stay last: %s", got)
	}
	// There is no argument that carries a token or relaxes https.
	for _, banned := range []string{"token", "insecure"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("%s reachable from MCP: %s", banned, got)
		}
	}
	argv, _ = cliArgs("run_test", map[string]any{"file": "a.chaos.yaml"})
	if strings.Contains(strings.Join(argv, " "), "stream") {
		t.Errorf("stream flags without arguments: %v", argv)
	}
}

func TestCLIArgsForwardLiveViewFlags(t *testing.T) {
	// No argument: no flag. `run` then does what it does from a shell.
	argv, _ := cliArgs("run_test", map[string]any{"file": "a.chaos.yaml"})
	got := strings.Join(argv, " ")
	for _, f := range []string{"-live", "-no-live", "-no-open"} {
		if strings.Contains(got, f) {
			t.Errorf("%s without an argument: %s", f, got)
		}
	}
	for arg, flag := range map[string]string{"live": "-live", "no-live": "-no-live", "no-open": "-no-open"} {
		argv, err := cliArgs("run_test", map[string]any{"file": "a.chaos.yaml", arg: true})
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(argv, " ")
		if !strings.Contains(got, flag) || !strings.HasSuffix(got, "a.chaos.yaml") {
			t.Errorf("%s=true: %s", arg, got)
		}
	}
	// false is the same as absent.
	argv, _ = cliArgs("run_test", map[string]any{"file": "a.chaos.yaml", "live": false, "no-live": "false"})
	if strings.Contains(strings.Join(argv, " "), "-live") {
		t.Errorf("false forwarded: %v", argv)
	}
}

// A note on stderr must never reach the first content item, which is the
// tool's JSON. This is the regression the live view caused: its "live view:"
// line was merged into stdout and run_test stopped returning valid JSON.
func TestToolStdoutStaysJSONAndNotesGoSeparately(t *testing.T) {
	dir := t.TempDir()
	needShell(t)
	script := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\necho 'live view: http://127.0.0.1:7080/p/p-abc' 1>&2\necho '{\"passed\":true}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITMUS_LITE_BIN", script)

	res, err := runTool("run_test", map[string]any{"file": "a.chaos.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(res.Text), &v); err != nil || v["passed"] != true {
		t.Fatalf("stdout is not the JSON: %q (%v)", res.Text, err)
	}
	if !strings.Contains(res.Notes, "live view: http://127.0.0.1:7080/p/p-abc") || strings.Contains(res.Text, "live view") {
		t.Fatalf("notes %q, text %q", res.Notes, res.Text)
	}

	// Over the wire: item 1 is the JSON, item 2 is the note.
	resp := handle(req{ID: json.RawMessage(`1`), Method: "tools/call",
		Params: json.RawMessage(`{"name":"run_test","arguments":{"file":"a.chaos.yaml"}}`)})
	items := resp["result"].(map[string]any)["content"].([]map[string]string)
	if len(items) != 2 || json.Unmarshal([]byte(items[0]["text"]), &v) != nil || !strings.HasPrefix(items[1]["text"], "live view:") {
		t.Fatalf("content %v", items)
	}
}

func TestToolWithoutNotesHasOneContentItem(t *testing.T) {
	dir := t.TempDir()
	needShell(t)
	script := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho '{\"ok\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITMUS_LITE_BIN", script)
	resp := handle(req{ID: json.RawMessage(`1`), Method: "tools/call",
		Params: json.RawMessage(`{"name":"list_faults"}`)})
	if items := resp["result"].(map[string]any)["content"].([]map[string]string); len(items) != 1 {
		t.Fatalf("content %v", items)
	}
}

func TestFailedToolReportsBothStreams(t *testing.T) {
	dir := t.TempDir()
	needShell(t)
	script := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'out' \necho 'bad scenario' 1>&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITMUS_LITE_BIN", script)
	_, err := runTool("run_test", map[string]any{"file": "a.chaos.yaml"})
	if err == nil || !strings.Contains(err.Error(), "bad scenario") || !strings.Contains(err.Error(), "out") {
		t.Fatalf("%v", err)
	}
}

// A run that finished with a failed hypothesis exits 3. The agent gets its JSON
// and the live view note as for a pass, not an error with both streams glued
// together.
func TestNotPassedRunIsAResultNotAnError(t *testing.T) {
	dir := t.TempDir()
	needShell(t)
	script := filepath.Join(dir, "fake-cli")
	body := "#!/bin/sh\necho '{\"passed\":false}'\necho 'live view: http://127.0.0.1:7080/p/p-abc' 1>&2\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITMUS_LITE_BIN", script)
	resp := handle(req{ID: json.RawMessage(`1`), Method: "tools/call",
		Params: json.RawMessage(`{"name":"run_test","arguments":{"file":"a.chaos.yaml"}}`)})
	result := resp["result"].(map[string]any)
	if result["isError"] != nil {
		t.Fatalf("a failed hypothesis was reported as a tool error: %v", result)
	}
	items := result["content"].([]map[string]string)
	var v map[string]any
	if len(items) != 2 || json.Unmarshal([]byte(items[0]["text"]), &v) != nil || v["passed"] != false ||
		!strings.HasPrefix(items[1]["text"], "live view:") {
		t.Fatalf("content %v", items)
	}
}

// needShell skips a test whose fake CLI is a shell script.
func needShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
}

// A value in a positional place never becomes a flag. Without "--", a file
// named "-stream-insecure" would be read by the CLI as that flag, and a tool
// call would reach flags its schema does not declare.
func TestPositionalValuesComeAfterDoubleDash(t *testing.T) {
	cases := map[string]map[string]any{
		"run_test":          {"file": "-stream-insecure"},
		"validate_scenario": {"file": "-stream-insecure"},
		"diagnose_failure":  {"file": "-stream-insecure"},
		"hub_import":        {"id": "-stream-insecure"},
		"compare_reports":   {"baseline": "-stream-insecure", "candidate": "-live"},
	}
	for name, args := range cases {
		argv, err := cliArgs(name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		dd := -1
		for i, a := range argv {
			if a == "--" {
				dd = i
				break
			}
		}
		if dd < 0 {
			t.Errorf("%s: no -- in %v", name, argv)
			continue
		}
		for _, a := range argv[:dd] {
			if strings.HasPrefix(a, "-stream-insecure") || a == "-live" {
				t.Errorf("%s: the caller's value %q is before --: %v", name, a, argv)
			}
		}
	}
}

// get_results is the one tool without a CLI command behind it, so it must not
// be a way to read other files.
func TestGetResultsReturnsReportsOnly(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("report.json", `{"name":"x","passed":true,"phases":[],"probes":[]}`)
	out, err := callTool("get_results", map[string]any{"file": good})
	if err != nil || !strings.Contains(out, `"passed":true`) {
		t.Fatalf("a report: %q %v", out, err)
	}

	for name, body := range map[string]string{
		"key.pem":      "-----BEGIN PRIVATE KEY-----\nSECRET-KEY-MATERIAL\n",
		"package.json": `{"name":"app","version":"1.0.0","secret":"SECRET-KEY-MATERIAL"}`,
		"cred.json":    `{"name":"x","passed":"SECRET-KEY-MATERIAL"}`,
	} {
		out, err := callTool("get_results", map[string]any{"file": write(name, body)})
		if err == nil {
			t.Errorf("%s was returned: %q", name, out)
		}
		if strings.Contains(out+fmt.Sprint(err), "SECRET-KEY-MATERIAL") {
			t.Errorf("%s: the refusal quotes the file", name)
		}
	}
	if _, err := callTool("get_results", map[string]any{"file": dir}); err == nil {
		t.Error("a directory was read")
	}
}
