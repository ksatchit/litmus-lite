package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/compose"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
	"github.com/litmuschaos/litmus-lite/internal/stream"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Level   string `json:"level"` // fail | warn | ok
	Message string `json:"message"`
}

func Run(target, scenarioFile string) []Check {
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
	if scenarioFile != "" {
		out = append(out, loadCheck(scenarioFile))
	} else {
		// No scenario to ask: look at the default generator, which is what a
		// load: block without a tool runs.
		out = append(out, generatorCheck(".", &scenario.LoadSpec{Tool: "vegaload", Scenario: "x"}))
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

// Stream checks a streaming destination. A URL is a push endpoint: it must
// answer an empty test batch with 2xx. Anything else is a listen address for
// -stream-pull: the bind rule must allow it and the port must be free.
func Stream(dest, token string, insecure bool) Check {
	const name = "stream"
	if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
		if err := stream.CheckPush(dest, token, insecure); err != nil {
			return Check{name, false, "fail", err.Error()}
		}
		return Check{name, true, "ok", "push endpoint accepts a test batch"}
	}
	if err := stream.CheckBind(dest, token); err != nil {
		return Check{name, false, "fail", err.Error()}
	}
	ln, err := net.Listen("tcp", dest)
	if err != nil {
		return Check{name, false, "fail", err.Error()}
	}
	ln.Close()
	return Check{name, true, "ok", "can listen on " + dest}
}

func loadCheck(path string) Check {
	sc, err := scenario.Load(path)
	if err != nil {
		return Check{"load", false, "fail", err.Error()}
	}
	if sc.Load == nil {
		return Check{"load", true, "ok", "no load block"}
	}
	return generatorCheck(filepath.Dir(path), sc.Load)
}

// generatorCheck says which copy of a load generator a run would use, its
// version and where it came from, and warns when it is missing, cannot stream
// live load points, or another copy looks better. The check never runs a load.
func generatorCheck(baseDir string, load *scenario.LoadSpec) Check {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st := compose.Inspect(ctx, baseDir, load, "")
	name := st.Tool
	if name == "" {
		name = "load"
	}
	if !st.Found {
		return Check{name, true, "warn", fmt.Sprintf("%s not found; load: will run chaos only", st.Bin)}
	}
	if st.Degraded() {
		return Check{name, true, "warn", st.Message()}
	}
	return Check{name, true, "ok", st.Message()}
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
