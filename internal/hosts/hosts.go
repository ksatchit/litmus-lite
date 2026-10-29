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

If a live-view service is configured (LITMUS_LITE_LIVE_URL, or ` + "`litmus-lite live enable URL`" + `), a run is also shown live in the user's browser: the page opens by itself, and its URL is printed as ` + "`live view: URL`" + ` (over MCP it is the second text item of ` + "`run_test`" + `). When you see that URL, tell the user. Pass ` + "`-no-live`" + ` (MCP: ` + "`no-live`" + `) if they do not want it, and ` + "`-no-open`" + ` to keep it from opening a browser. With no service configured nothing is shown or sent. Never keep a scenario only in chat. Always write a file. Point probes at the proxy listen address during HTTP faults. Do not require Prometheus or a load generator for a chaos-only run. A ` + "`load:`" + ` block runs during inject. ` + "`tool`" + ` defaults to ` + "`vegaload`" + `; ` + "`k6`" + ` and ` + "`command`" + ` are the other tools. For ` + "`vegaload`" + `, ` + "`scenario`" + ` is a path relative to the chaos file and ` + "`args`" + ` are its flags, for example ` + "`args: [\"-executor\", \"constant-arrival-rate\", \"-rate\", \"10\", \"-max-vus\", \"100\", \"-duration\", \"17s\"]`" + ` (no ` + "`-allow-target`" + ` for localhost). During an HTTP fault the load scenario must target the proxy listen address, not the service itself. The load shows up in the live view next to the probes. ` + "`command`" + ` must print JSON ` + "`time_series`" + ` of ` + "`offset_s`" + `, ` + "`rps`" + `, and ` + "`error_rate`" + ` on stdout. ` + "`process.pause`" + ` and ` + "`process.kill`" + ` signal only PIDs passed with ` + "`-allow-pid`" + `. ` + "`disk.fill`" + ` requires ` + "`-yes`" + ` and will not overwrite an existing file. Over MCP, ` + "`yes`" + `, ` + "`allow-pid`" + ` and ` + "`allow-target`" + ` are refused unless the person enabled them with ` + "`LITMUS_LITE_MCP_ALLOW`" + ` in the MCP server's env (` + "`litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid`" + `): you cannot consent for them. If a call is refused, tell the person, or give them the CLI command to run themselves. Validate a scenario you wrote with ` + "`strict`" + ` (CLI ` + "`-strict`" + `), which fails on a misspelled key or param instead of ignoring it. A hypothesis over a window with no samples fails as "no data" and a ` + "`recovery`" + ` hypothesis fails as "not recovered" if the service never came back. A ` + "`failpoint`" + ` fault arms a named Go hook for the inject window. The process must set ` + "`LITMUS_LITE_FAILPOINT`" + ` and ` + "`LITMUS_LITE_FAILPOINT_TOKEN`" + `, and the scenario sends that token to ` + "`params.url`" + `.
`

// WriteCursor writes the Cursor rule and MCP entry. env, when not empty, is
// set on the MCP server entry (see mergeMCP).
func WriteCursor(projectRoot, binary string, env map[string]string) error {
	rules := filepath.Join(projectRoot, ".cursor", "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(rules, "litmus-lite.mdc"), []byte(RuleBody), 0o644); err != nil {
		return err
	}
	return mergeMCP(filepath.Join(projectRoot, ".cursor", "mcp.json"), binary, env)
}

// mergeMCP adds or updates the litmus-lite server in an MCP config file and
// leaves every other server alone. env entries are set on the server's env
// object, and an empty value removes the entry; an env object already there is
// kept, so a re-run of init without them does not undo an earlier -env.
func mergeMCP(path, binary string, env map[string]string) error {
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
	entry := map[string]any{
		"command": binary,
		"args":    []string{"mcp", "serve"},
	}
	var merged map[string]any
	if old, ok := servers["litmus-lite"].(map[string]any); ok {
		merged, _ = old["env"].(map[string]any)
	}
	if len(env) > 0 {
		if merged == nil {
			merged = map[string]any{}
		}
		for k, v := range env {
			if v == "" {
				delete(merged, k)
				continue
			}
			merged[k] = v
		}
	}
	if len(merged) > 0 {
		entry["env"] = merged
	}
	servers["litmus-lite"] = entry
	root["mcpServers"] = servers
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func WriteProjectMCP(projectRoot, binary string, env map[string]string) error {
	return mergeMCP(filepath.Join(projectRoot, ".mcp.json"), binary, env)
}
