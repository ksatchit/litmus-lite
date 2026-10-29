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

func runVegaLoad(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec, opts RunOptions) (Output, error) {
	binName := "vegaload"
	path, err := resolveBin(baseDir, binName, binOverride)
	if err != nil {
		return Output{Tool: "vegaload", Warn: "vegaload not on PATH; running chaos only"}, nil
	}
	scen, err := resolveScenario(baseDir, load.Scenario)
	if err != nil {
		return Output{}, err
	}
	dir, derr := os.MkdirTemp("", "litmus-lite-vegaload-")
	if derr != nil {
		return Output{}, derr
	}
	defer os.RemoveAll(dir)
	outFile := filepath.Join(dir, "result.json")
	env := childEnv("vegaload", opts)
	// What this vegaload can do is read from its help before it runs, so every
	// run takes one path (see vegaLoadArgs).
	caps := probeCaps(ctx, path, env)
	who := fmt.Sprintf("%s (%s)", Status{Tool: "vegaload", Version: probeVersion(ctx, path, env)}.Name(), tilde(path))
	args, streamFile, note := vegaLoadArgs(caps, who, dir, outFile, opts.Progress != nil)
	var stopTail func()
	if streamFile != "" {
		// Read the per-second points as vegaload writes them.
		stopTail = tailVegaLoad(streamFile, opts.Progress)
	}
	args = append(args, load.Args...)
	args = append(args, scen)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	b, err := cmd.CombinedOutput()
	if stopTail != nil {
		stopTail()
	}
	raw, readErr := os.ReadFile(outFile)
	if readErr != nil {
		raw = b
	}
	out := Output{Tool: "vegaload", Raw: asRaw(raw), Warn: note}
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, truncate(string(b), 400))
	}
	if readErr != nil && !json.Valid(b) {
		out.Warn = joinNotes(out.Warn, "vegaload ran but -out file missing; using stdout")
	}
	pts, perr := seriesFromVegaLoad(out.Raw)
	if perr != nil {
		out.Warn = joinNotes(out.Warn, perr.Error())
		return out, nil
	}
	if len(pts) == 0 {
		out.Warn = joinNotes(out.Warn, "vegaload JSON has no time_series")
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
