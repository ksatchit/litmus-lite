package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EvalSuiteVersion = "v1"

type Case struct {
	Name     string
	Method   string
	Params   any
	Want     string
	MustFile bool // tool output JSON includes path; that file must exist
}

func EvalSuite() []Case {
	return evalSuite("")
}

func evalSuite(work string) []Case {
	if work == "" {
		work = os.TempDir()
	}
	root, _ := moduleRoot()
	ignite := filepath.Join(root, "examples", "launchpad", "ignite.chaos.yaml")
	return []Case{
		{Name: EvalSuiteVersion + "/initialize", Method: "initialize", Want: "litmus-lite"},
		{Name: EvalSuiteVersion + "/tools-list", Method: "tools/list", Want: "run_test"},
		{
			Name:   EvalSuiteVersion + "/validate_scenario",
			Method: "tools/call",
			Params: toolCall{Name: "validate_scenario", Args: map[string]any{"file": ignite}},
			Want:   "launchpad-http-latency",
		},
		{
			Name:   EvalSuiteVersion + "/list_faults",
			Method: "tools/call",
			Params: toolCall{Name: "list_faults", Args: map[string]any{}},
			Want:   "http.latency",
		},
		{
			Name:     EvalSuiteVersion + "/hub_import",
			Method:   "tools/call",
			Params:   toolCall{Name: "hub_import", Args: map[string]any{"id": "http.latency", "beside": work}},
			Want:     ".chaos.yaml",
			MustFile: true,
		},
		{
			Name:     EvalSuiteVersion + "/create_scenario",
			Method:   "tools/call",
			Params:   toolCall{Name: "create_scenario", Args: map[string]any{"beside": work, "name": "mcp-eval"}},
			Want:     "mcp-eval.chaos.yaml",
			MustFile: true,
		},
	}
}

func Eval() (passed, failed int, report string) {
	var buf bytes.Buffer
	if err := toolsHaveCLI(); err != nil {
		failed++
		fmt.Fprintf(&buf, "FAIL %s/tools-cli-equivalent: %v\n", EvalSuiteVersion, err)
	} else {
		passed++
		fmt.Fprintf(&buf, "PASS %s/tools-cli-equivalent\n", EvalSuiteVersion)
	}

	work, err := os.MkdirTemp("", "litmus-lite-mcp-eval-")
	if err != nil {
		failed++
		fmt.Fprintf(&buf, "FAIL %s/workdir: %v\n", EvalSuiteVersion, err)
		return passed, failed, buf.String()
	}
	defer os.RemoveAll(work)

	listed := false
	for i, c := range evalSuite(work) {
		raw, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": i + 1, "method": c.Method, "params": c.Params,
		})
		var r req
		_ = json.Unmarshal(raw, &r)
		resp := handle(r)
		b, _ := json.Marshal(resp)
		text := string(b)
		ok := strings.Contains(text, c.Want)
		if c.Method == "tools/call" && hasToolError(resp) {
			ok = false
		}
		if c.Name == EvalSuiteVersion+"/tools-list" {
			listed = true
			for _, t := range allTools() {
				if !strings.Contains(text, t.Name) {
					ok = false
					c.Want = t.Name
					break
				}
			}
		}
		if ok && c.MustFile {
			if err := requireWrittenFile(contentText(b)); err != nil {
				ok = false
				fmt.Fprintf(&buf, "FAIL %s: %v\n", c.Name, err)
				failed++
				continue
			}
		}
		if !ok {
			failed++
			fmt.Fprintf(&buf, "FAIL %s: want substring %q\n  got %s\n", c.Name, c.Want, contentText(b))
			continue
		}
		passed++
		fmt.Fprintf(&buf, "PASS %s\n", c.Name)
	}
	if !listed {
		failed++
		fmt.Fprintf(&buf, "FAIL %s/tools-list: suite missing tools/list\n", EvalSuiteVersion)
	}
	return passed, failed, buf.String()
}

func hasToolError(resp map[string]any) bool {
	result, _ := resp["result"].(map[string]any)
	err, _ := result["isError"].(bool)
	return err
}

func contentText(raw []byte) string {
	var wrap struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &wrap) != nil || len(wrap.Result.Content) == 0 {
		return string(raw)
	}
	return wrap.Result.Content[0].Text
}

func requireWrittenFile(toolOut string) error {
	var m map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(toolOut)), &m); err != nil {
		return fmt.Errorf("tool output is not path JSON: %w", err)
	}
	p := m["path"]
	if p == "" || !strings.HasSuffix(p, ".chaos.yaml") {
		return fmt.Errorf("missing .chaos.yaml path in %q", toolOut)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), "kind: Scenario") {
		return fmt.Errorf("%s is not a scenario file", p)
	}
	return nil
}
