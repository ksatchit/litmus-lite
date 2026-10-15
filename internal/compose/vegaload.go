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
	args := []string{"run", "-output", "json", "-out", outFile}
	var stopTail func()
	if opts.Progress != nil {
		// The run is streamed: ask vegaload for its per-second points in a file
		// and read them as they are written. A stream file also keeps vegaload
		// out of the live view, and -no-open keeps its report out of the browser:
		// the page that shows this run is litmus-lite's.
		streamFile := filepath.Join(dir, "stream.ndjson")
		// Its own HTML report goes to the temporary folder too, so a streamed run
		// leaves no vegaload-report-*.html in the working directory.
		args = append(args, "-stream-file", streamFile, "-no-open", "-report", filepath.Join(dir, "report.html"))
		stopTail = tailVegaLoad(streamFile, opts.Progress)
	}
	args = append(args, load.Args...)
	args = append(args, scen)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = childEnv("vegaload", opts)
	b, err := cmd.CombinedOutput()
	if stopTail != nil {
		stopTail()
	}
	if err != nil && opts.Progress != nil && strings.Contains(string(b), "flag provided but not defined") {
		// An older vegaload does not know -stream-file. Run it as before: the
		// series still arrives at the end, only the live points are missing.
		opts.Progress = nil
		out, err := runVegaLoad(ctx, binOverride, baseDir, load, opts)
		if out.Warn == "" {
			out.Warn = "this vegaload has no -stream-file; load points arrive at the end"
		}
		return out, err
	}
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
