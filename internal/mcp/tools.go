package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tool is one MCP tool. Every entry must map to a CLI the user can type
// (get_results reads a file the CLI wrote; it is not an agent-only path).
type tool struct {
	Name     string
	CLI      string // litmus-lite subcommand, empty when ReadFile
	ReadFile bool
}

func allTools() []tool {
	return []tool{
		{Name: "create_scenario", CLI: "new"},
		{Name: "validate_scenario", CLI: "validate"},
		{Name: "run_test", CLI: "run"},
		{Name: "get_results", ReadFile: true},
		{Name: "diagnose_failure", CLI: "diagnose"},
		{Name: "list_faults", CLI: "hub"},
		{Name: "hub_import", CLI: "hub"},
		{Name: "compare_reports", CLI: "compare"},
		{Name: "doctor", CLI: "doctor"},
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
		desc := "read a report JSON path"
		if t.CLI != "" {
			desc = "litmus-lite " + t.CLI
			if t.Name == "list_faults" {
				desc = "litmus-lite hub list"
			}
			if t.Name == "hub_import" {
				desc = "litmus-lite hub import"
			}
			if t.Name == "validate_scenario" || t.Name == "run_test" {
				desc += " (yes, allow-target, and allow-pid are forwarded)"
			}
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": desc,
			"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
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
		return []string{"hub", "import", "-beside", str("beside", "."), "-output", "json", str("id", "")}, nil
	case "compare_reports":
		return []string{"compare", "-output", "json", str("baseline", ""), str("candidate", "")}, nil
	case "doctor":
		return []string{"doctor", "-output", "json"}, nil
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
