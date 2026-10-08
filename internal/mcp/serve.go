package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
