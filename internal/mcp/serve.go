package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type req struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type toolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"arguments"`
}

func Serve(in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(bufio.NewReader(in))
	for {
		var r req
		if err := dec.Decode(&r); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp := handle(r)
		enc := json.NewEncoder(out)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

func handle(r req) map[string]any {
	switch r.Method {
	case "initialize":
		return ok(r.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "litmus-lite", "version": "0.1.0-dev"},
		})
	case "notifications/initialized", "initialized":
		return map[string]any{"jsonrpc": "2.0"}
	case "tools/list":
		return ok(r.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		var p toolCall
		_ = json.Unmarshal(r.Params, &p)
		text, err := callTool(p.Name, p.Args)
		if err != nil {
			return ok(r.ID, map[string]any{
				"content": []map[string]string{{"type": "text", "text": err.Error()}},
				"isError": true,
			})
		}
		return ok(r.ID, map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		})
	default:
		return errResp(r.ID, fmt.Sprintf("unknown method %s", r.Method))
	}
}

func ok(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errResp(id json.RawMessage, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": msg}}
}

func toolDefs() []map[string]any {
	names := []struct{ n, d string }{
		{"create_scenario", "litmus-lite new"},
		{"validate_scenario", "litmus-lite validate"},
		{"run_test", "litmus-lite run"},
		{"get_results", "read a report JSON path"},
		{"diagnose_failure", "litmus-lite diagnose"},
		{"list_faults", "litmus-lite hub list"},
		{"hub_import", "litmus-lite hub import"},
		{"compare_reports", "litmus-lite compare"},
		{"doctor", "litmus-lite doctor"},
	}
	var out []map[string]any
	for _, t := range names {
		out = append(out, map[string]any{
			"name":        t.n,
			"description": t.d,
			"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
		})
	}
	return out
}

func callTool(name string, args map[string]any) (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", err
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
	var argv []string
	switch name {
	case "create_scenario":
		argv = []string{"new", "-beside", str("beside", "."), "-name", str("name", "scenario"), "-output", "json"}
	case "validate_scenario":
		argv = []string{"validate", str("file", ""), "-output", "json"}
	case "run_test":
		argv = []string{"run", str("file", ""), "-output", "json"}
		if o := str("out", ""); o != "" {
			argv = append(argv, "-out", o)
		}
	case "diagnose_failure":
		argv = []string{"diagnose", str("file", ""), "-output", "json"}
	case "list_faults":
		argv = []string{"hub", "list", "-output", "json"}
	case "hub_import":
		argv = []string{"hub", "import", str("id", ""), "-beside", str("beside", "."), "-output", "json"}
	case "compare_reports":
		argv = []string{"compare", str("baseline", ""), str("candidate", ""), "-output", "json"}
	case "doctor":
		argv = []string{"doctor", "-output", "json"}
	case "get_results":
		b, err := os.ReadFile(str("file", "report.json"))
		return string(b), err
	default:
		return "", fmt.Errorf("unknown tool %s", name)
	}
	cmd := exec.Command(bin, argv...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(b)), fmt.Errorf("%s: %w", strings.TrimSpace(string(b)), err)
	}
	return string(b), nil
}
