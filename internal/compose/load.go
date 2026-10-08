package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// LoadPoint is the generator-agnostic series merged onto the overlay.
// OffsetS is seconds from when the generator started.
type LoadPoint struct {
	OffsetS   float64 `json:"offset_s"`
	RPS       float64 `json:"rps"`
	ErrorRate float64 `json:"error_rate"`
}

// Output is one generator run. Raw is the tool's own JSON when it is valid.
// Series is what the overlay merges. Warn is set when chaos should continue
// without a successful merge.
type Output struct {
	Tool   string
	Raw    json.RawMessage
	Series []LoadPoint
	Warn   string
}

// Active reports whether the load block should exec a generator.
func Active(load *scenario.LoadSpec) bool {
	if load == nil {
		return false
	}
	tool := strings.ToLower(strings.TrimSpace(load.Tool))
	if tool == "" {
		tool = "vegaload"
	}
	if tool == "command" {
		return strings.TrimSpace(load.Command) != ""
	}
	return strings.TrimSpace(load.Scenario) != ""
}

// Run execs load.tool. An empty tool is vegaload. A missing binary returns a
// warning and a nil error so the chaos run continues.
// binOverride, when set, replaces the tool executable (tests and explicit paths).
func Run(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec) (Output, error) {
	if !Active(load) {
		return Output{}, nil
	}
	tool := strings.ToLower(strings.TrimSpace(load.Tool))
	if tool == "" {
		tool = "vegaload"
	}
	switch tool {
	case "vegaload":
		return runVegaLoad(ctx, binOverride, baseDir, load)
	case "k6":
		return runK6(ctx, binOverride, baseDir, load)
	case "command":
		return runCommand(ctx, binOverride, baseDir, load)
	default:
		return Output{}, fmt.Errorf("unknown load tool %q (want vegaload, k6, or command)", tool)
	}
}

// BinaryStatus reports whether the generator named by load is executable.
func BinaryStatus(baseDir string, load *scenario.LoadSpec) (name, message string, found bool) {
	if load == nil {
		return "", "", false
	}
	tool := strings.ToLower(strings.TrimSpace(load.Tool))
	if tool == "" {
		tool = "vegaload"
	}
	bin := tool
	if tool == "command" {
		bin = strings.TrimSpace(load.Command)
	}
	if _, err := resolveBin(baseDir, bin, ""); err != nil {
		return tool, fmt.Sprintf("%s not found; load: will run chaos only", bin), false
	}
	return tool, bin + " found", true
}

func resolveBin(baseDir, name, override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", err
		}
		return override, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty load command")
	}
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		cand := name
		if !filepath.IsAbs(name) && baseDir != "" {
			cand = filepath.Join(baseDir, name)
		}
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
		if filepath.IsAbs(name) {
			return "", fmt.Errorf("%s: not found", name)
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}

func resolveScenario(baseDir, scen string) (string, error) {
	scen = strings.TrimSpace(scen)
	if scen == "" {
		return "", nil
	}
	if filepath.IsAbs(scen) || baseDir == "" {
		return scen, nil
	}
	cand := filepath.Join(baseDir, scen)
	if _, err := os.Stat(cand); err == nil {
		return cand, nil
	}
	return scen, nil
}

func asRaw(b []byte) json.RawMessage {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return json.RawMessage(b)
}
