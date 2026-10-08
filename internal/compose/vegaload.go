package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func runVegaLoad(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec) (Output, error) {
	binName := "vegaload"
	path, err := resolveBin(baseDir, binName, binOverride)
	if err != nil {
		return Output{Tool: "vegaload", Warn: "vegaload not on PATH; running chaos only"}, nil
	}
	scen, err := resolveScenario(baseDir, load.Scenario)
	if err != nil {
		return Output{}, err
	}
	outFile := filepath.Join(os.TempDir(), fmt.Sprintf("litmus-lite-vegaload-%d.json", os.Getpid()))
	defer os.Remove(outFile)
	args := []string{"run", "-output", "json", "-out", outFile}
	args = append(args, load.Args...)
	args = append(args, scen)
	cmd := exec.CommandContext(ctx, path, args...)
	b, err := cmd.CombinedOutput()
	raw, readErr := os.ReadFile(outFile)
	if readErr != nil {
		raw = b
	}
	out := Output{Tool: "vegaload", Raw: asRaw(raw)}
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, truncate(string(b), 400))
	}
	if readErr != nil && !json.Valid(b) {
		out.Warn = "vegaload ran but -out file missing; using stdout"
	}
	pts, perr := seriesFromVegaLoad(out.Raw)
	if perr != nil {
		out.Warn = perr.Error()
		return out, nil
	}
	if len(pts) == 0 {
		out.Warn = "vegaload JSON has no time_series"
		return out, nil
	}
	out.Series = pts
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
