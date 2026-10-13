package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tool is one MCP tool. Every entry must map to a CLI the user can type
// (get_results reads a file the CLI wrote; it is not an agent-only path).
//
// Desc, Props and Required are what tools/list advertises. They exist so an
// agent can call a tool correctly on the first try instead of probing for
// argument names. TestToolSchemasMatchCLIArgs keeps them in step with cliArgs.
type tool struct {
	Name     string
	CLI      string // litmus-lite subcommand, empty when ReadFile
	ReadFile bool
	Desc     string
	Props    map[string]any // argument name -> JSON Schema property
	Required []string
}

func strArg(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func boolArg(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// safetyProps are the flags validate_scenario and run_test forward to the CLI.
func safetyProps(extra map[string]any) map[string]any {
	p := map[string]any{
		"yes":          boolArg("Forward -yes: confirm a destructive fault (process.kill, disk.fill). Not needed for latency, status, timeout or pause faults."),
		"allow-target": strArg("Forward -allow-target: comma-separated extra non-loopback hosts the scenario may touch."),
		"allow-pid":    strArg("Forward -allow-pid: the one PID process.pause or process.kill may signal, for example \"4242\". Required for those faults; no other PID is signalled."),
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func allTools() []tool {
	return []tool{
		{
			Name: "create_scenario", CLI: "new",
			Desc: "litmus-lite new: write a co-located http.latency scenario template (<name>.chaos.yaml) into a directory. Returns {\"path\": ...}. Edit the file afterwards to fit the test.",
			Props: map[string]any{
				"beside": strArg("Directory to write the scenario into, usually next to the service code (default \".\")."),
				"name":   strArg("File stem without .chaos.yaml (default \"scenario\")."),
			},
		},
		{
			Name: "validate_scenario", CLI: "validate",
			Desc:     "litmus-lite validate: check a scenario file and the safety rules without running it. Run this before run_test.",
			Props:    safetyProps(map[string]any{"file": strArg("Path to the *.chaos.yaml file.")}),
			Required: []string{"file"},
		},
		{
			Name: "run_test", CLI: "run",
			Desc: "litmus-lite run: execute a scenario (steady state, inject, recover) and write a JSON report and an overlay HTML report. Returns the report as JSON.",
			Props: safetyProps(map[string]any{
				"file": strArg("Path to the *.chaos.yaml file."),
				"out":  strArg("Where to write the JSON report, for example report.json. Pass this path to diagnose_failure or get_results."),
			}),
			Required: []string{"file"},
		},
		{
			Name: "get_results", ReadFile: true,
			Desc:  "Read a report JSON file that run_test wrote.",
			Props: map[string]any{"file": strArg("Path to the report JSON (default \"report.json\").")},
		},
		{
			Name: "diagnose_failure", CLI: "diagnose",
			Desc:     "litmus-lite diagnose: explain a report in terms of its hypotheses, phases and likely cause.",
			Props:    map[string]any{"file": strArg("Path to the report JSON written by run_test.")},
			Required: []string{"file"},
		},
		{
			Name: "list_faults", CLI: "hub",
			Desc: "litmus-lite hub list: list the fault kinds in the hub catalog. Takes no arguments.",
		},
		{
			Name: "hub_import", CLI: "hub",
			Desc: "litmus-lite hub import: write a hub scenario template (<name>.chaos.yaml) into a directory. Returns {\"path\": ...}. An existing file with the same name is overwritten, so pass name when importing the same id twice.",
			Props: map[string]any{
				"id":     strArg("Hub id from list_faults, for example http.latency, http.status-inject, http.timeout or process.pause."),
				"beside": strArg("Directory to write into, usually next to the service code (default \".\")."),
				"name":   strArg("File stem without .chaos.yaml (default: the id with dots replaced by dashes)."),
			},
			Required: []string{"id"},
		},
		{
			Name: "compare_reports", CLI: "compare",
			Desc: "litmus-lite compare: compare two report JSON files phase by phase (steady, inject, recover).",
			Props: map[string]any{
				"baseline":  strArg("Path to the baseline report JSON."),
				"candidate": strArg("Path to the candidate report JSON."),
			},
			Required: []string{"baseline", "candidate"},
		},
		{
			Name: "doctor", CLI: "doctor",
			Desc:  "litmus-lite doctor: check the local environment (Cursor MCP setup, optional VegaLoad and Prometheus).",
			Props: map[string]any{"file": strArg("Optional scenario file; also checks its load tool.")},
		},
	}
}

func toolByName(name string) (tool, bool) {
	for _, t := range allTools() {
		if t.Name == name {
			return t, true
		}
	}
	return tool{}, false
}

func toolsHaveCLI() error {
	for _, t := range allTools() {
		if t.CLI == "" && !t.ReadFile {
			return fmt.Errorf("tool %q has no CLI equivalent", t.Name)
		}
	}
	return nil
}

func toolDefs() []map[string]any {
	var out []map[string]any
	for _, t := range allTools() {
		props := t.Props
		if props == nil {
			props = map[string]any{}
		}
		schema := map[string]any{
			"type":       "object",
			"properties": props,
			// Unknown keys stay accepted (and ignored), as before this schema existed.
			"additionalProperties": true,
		}
		if len(t.Required) > 0 {
			schema["required"] = t.Required
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Desc,
			"inputSchema": schema,
		})
	}
	return out
}

func cliArgs(name string, args map[string]any) ([]string, error) {
	str := func(k, def string) string {
		if args == nil {
			return def
		}
		if v, ok := args[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	safetyFlags := func(argv []string) []string {
		if flagOn(args, "yes") {
			argv = append(argv, "-yes")
		}
		if v := str("allow-target", ""); v != "" {
			argv = append(argv, "-allow-target", v)
		}
		if v := str("allow-pid", ""); v != "" {
			argv = append(argv, "-allow-pid", v)
		}
		return argv
	}
	switch name {
	case "create_scenario":
		return []string{"new", "-beside", str("beside", "."), "-name", str("name", "scenario"), "-output", "json"}, nil
	case "validate_scenario":
		argv := safetyFlags([]string{"validate", "-output", "json"})
		return append(argv, str("file", "")), nil
	case "run_test":
		argv := []string{"run", "-output", "json"}
		if o := str("out", ""); o != "" {
			argv = append(argv, "-out", o)
		}
		argv = safetyFlags(argv)
		return append(argv, str("file", "")), nil
	case "diagnose_failure":
		return []string{"diagnose", "-output", "json", str("file", "")}, nil
	case "list_faults":
		return []string{"hub", "list", "-output", "json"}, nil
	case "hub_import":
		argv := []string{"hub", "import", "-beside", str("beside", "."), "-output", "json"}
		if n := str("name", ""); n != "" {
			argv = append(argv, "-name", n)
		}
		return append(argv, str("id", "")), nil
	case "compare_reports":
		return []string{"compare", "-output", "json", str("baseline", ""), str("candidate", "")}, nil
	case "doctor":
		argv := []string{"doctor", "-output", "json"}
		if f := str("file", ""); f != "" {
			argv = append(argv, "-file", f)
		}
		return argv, nil
	default:
		return nil, fmt.Errorf("tool %q has no CLI equivalent", name)
	}
}

func flagOn(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return fmt.Sprint(t) == "true"
	}
}

func callTool(name string, args map[string]any) (string, error) {
	t, ok := toolByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	if t.CLI == "" && !t.ReadFile {
		return "", fmt.Errorf("tool %q has no CLI equivalent", name)
	}
	str := func(k, def string) string {
		if args == nil {
			return def
		}
		if v, ok := args[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	if t.ReadFile {
		b, err := os.ReadFile(str("file", "report.json"))
		return string(b), err
	}
	argv, err := cliArgs(name, args)
	if err != nil {
		return "", err
	}
	bin, err := resolveCLI()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, argv...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(b)), fmt.Errorf("%s: %w", strings.TrimSpace(string(b)), err)
	}
	return string(b), nil
}
