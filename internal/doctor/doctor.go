package doctor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Level   string `json:"level"` // fail | warn | ok
	Message string `json:"message"`
}

func Run(target string) []Check {
	var out []Check
	if _, err := os.Executable(); err != nil {
		out = append(out, Check{"binary", false, "fail", err.Error()})
	} else {
		out = append(out, Check{"binary", true, "ok", "executable located"})
	}
	if p, err := exec.LookPath("litmus-lite"); err != nil {
		out = append(out, Check{"PATH", true, "warn", "litmus-lite not on PATH (use the built binary)"})
	} else {
		out = append(out, Check{"PATH", true, "ok", p})
	}
	if _, err := exec.LookPath("vegaload"); err != nil {
		out = append(out, Check{"vegaload", true, "warn", "vegaload not on PATH; load: blocks will run chaos only"})
	} else {
		out = append(out, Check{"vegaload", true, "ok", "vegaload found"})
	}
	cwd, _ := os.Getwd()
	mcp := filepath.Join(cwd, ".cursor", "mcp.json")
	if _, err := os.Stat(mcp); err != nil {
		out = append(out, Check{"mcp", true, "warn", "no .cursor/mcp.json; run litmus-lite init"})
	} else {
		out = append(out, Check{"mcp", true, "ok", mcp})
	}
	if target != "" {
		c := http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get(target)
		if err != nil {
			out = append(out, Check{"target", false, "fail", err.Error()})
		} else {
			resp.Body.Close()
			out = append(out, Check{"target", true, "ok", fmt.Sprintf("HTTP %d", resp.StatusCode)})
		}
	}
	return out
}

func JSON(checks []Check) []byte {
	b, _ := json.MarshalIndent(checks, "", "  ")
	return b
}

func Failed(checks []Check) bool {
	for _, c := range checks {
		if c.Level == "fail" {
			return true
		}
	}
	return false
}
