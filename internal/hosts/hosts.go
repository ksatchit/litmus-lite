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

Never keep a scenario only in chat. Always write a file. Point probes at the proxy listen address during HTTP faults. Do not require Prometheus or VegaLoad for a chaos-only run. ` + "`process.pause`" + ` and ` + "`process.kill`" + ` signal only PIDs passed with ` + "`-allow-pid`" + `. ` + "`disk.fill`" + ` requires ` + "`-yes`" + ` and will not overwrite an existing file.
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
