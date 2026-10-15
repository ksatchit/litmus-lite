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
	return RunWith(ctx, binOverride, baseDir, load, RunOptions{})
}

// RunWith is Run with the options of a streamed run: the run id exported to
// the generator and, for vegaload, live progress.
func RunWith(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec, opts RunOptions) (Output, error) {
	if !Active(load) {
		return Output{}, nil
	}
	tool := strings.ToLower(strings.TrimSpace(load.Tool))
	if tool == "" {
		tool = "vegaload"
	}
	switch tool {
	case "vegaload":
		return runVegaLoad(ctx, binOverride, baseDir, load, opts)
	case "k6":
		return runK6(ctx, binOverride, baseDir, load, opts)
	case "command":
		return runCommand(ctx, binOverride, baseDir, load, opts)
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
	if err == nil {
		return path, nil
	}
	// Not on PATH: look next to this executable. A GUI app such as an editor
	// often starts the MCP server with a short PATH, and two tools installed
	// into one folder should find each other either way.
	if dir := executableDir(); dir != "" && !strings.ContainsAny(name, `/\`) {
		for _, cand := range []string{name, name + ".exe"} {
			p := filepath.Join(dir, cand)
			if st, serr := os.Stat(p); serr == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", err
}

// executableDir is the folder of the running litmus-lite binary. It is a
// variable so that tests can point it elsewhere.
var executableDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
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

// childEnv is the environment of a generator: the parent's, plus the run id so
// that anything the generator records can be joined to this run. A generator
// that streams to the live view on its own is switched off when vegaload is
// the tool, because litmus-lite owns the view of a composed run.
func childEnv(tool string, opts RunOptions) []string {
	env := os.Environ()
	if opts.RunID != "" {
		env = append(env, "LITMUS_LITE_RUN_ID="+opts.RunID)
		if tool == "vegaload" {
			env = append(env, "VEGALOAD_RUN_ID="+opts.RunID)
		}
	}
	if tool == "vegaload" {
		env = append(env, "VEGALOAD_LIVE=off")
	}
	return env
}
