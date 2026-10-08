package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Case struct {
	Name   string
	Method string
	Params any
	Want   string
}

func EvalSuite() []Case {
	return []Case{
		{Name: "initialize", Method: "initialize", Want: "litmus-lite"},
		{Name: "tools-list", Method: "tools/list", Want: "run_test"},
	}
}

func Eval() (passed, failed int, report string) {
	var buf bytes.Buffer
	for i, c := range EvalSuite() {
		raw, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": i + 1, "method": c.Method, "params": c.Params,
		})
		var r req
		_ = json.Unmarshal(raw, &r)
		resp := handle(r)
		b, _ := json.Marshal(resp)
		if !strings.Contains(string(b), c.Want) {
			failed++
			fmt.Fprintf(&buf, "FAIL %s: want substring %q\n", c.Name, c.Want)
			continue
		}
		passed++
		fmt.Fprintf(&buf, "PASS %s\n", c.Name)
	}
	return passed, failed, buf.String()
}
