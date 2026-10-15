package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Dir is the user host directory (~/.litmus-lite, or $LITMUS_LITE_HOME).
func Dir() string {
	if d := os.Getenv("LITMUS_LITE_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".litmus-lite")
}

const RuleBody = `---
description: litmus-lite chaos scenarios next to application code
globs:
alwaysApply: true
---

Litmus-lite is installed in this repo. Chaos tests are co-located ` + "`*.chaos.yaml`" + ` files.

Use the CLI (or the matching MCP tools, which wrap the CLI):

- ` + "`litmus-lite hub import http.latency -beside DIR`" + `
- ` + "`litmus-lite validate FILE`" + `
- ` + "`litmus-lite run FILE -output json -out report.json`" + `
- ` + "`litmus-lite diagnose report.json`" + `

A run is shown live in the user's browser by default (the live view): the page opens by itself, and its URL is printed as ` + "`live view: URL`" + ` (over MCP it is the second text item of ` + "`run_test`" + `). Tell the user that URL after starting a run. Pass ` + "`-no-live`" + ` (MCP: ` + "`no-live`" + `) only if they do not want it, and ` + "`-no-open`" + ` to keep it from opening a browser. Never keep a scenario only in chat. Always write a file. Point probes at the proxy listen address during HTTP faults. Do not require Prometheus or a load generator for a chaos-only run. A ` + "`load:`" + ` block runs during inject. ` + "`tool`" + ` defaults to ` + "`vegaload`" + `; ` + "`k6`" + ` and ` + "`command`" + ` are the other tools. ` + "`command`" + ` must print JSON ` + "`time_series`" + ` of ` + "`offset_s`" + `, ` + "`rps`" + `, and ` + "`error_rate`" + ` on stdout. ` + "`process.pause`" + ` and ` + "`process.kill`" + ` signal only PIDs passed with ` + "`-allow-pid`" + `. ` + "`disk.fill`" + ` requires ` + "`-yes`" + ` and will not overwrite an existing file. A ` + "`failpoint`" + ` fault arms a named Go hook for the inject window. The process must set ` + "`LITMUS_LITE_FAILPOINT`" + ` and ` + "`LITMUS_LITE_FAILPOINT_TOKEN`" + `, and the scenario sends that token to ` + "`params.url`" + `.
`

func WriteCursor(projectRoot, binary string) error {
	rules := filepath.Join(projectRoot, ".cursor", "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(rules, "litmus-lite.mdc"), []byte(RuleBody), 0o644); err != nil {
		return err
	}
	return mergeMCP(filepath.Join(projectRoot, ".cursor", "mcp.json"), binary)
}

func mergeMCP(path, binary string) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	var root map[string]any
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["litmus-lite"] = map[string]any{
		"command": binary,
		"args":    []string{"mcp", "serve"},
	}
	root["mcpServers"] = servers
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func WriteProjectMCP(projectRoot, binary string) error {
	return mergeMCP(filepath.Join(projectRoot, ".mcp.json"), binary)
}
