package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func runCommand(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec) (Output, error) {
	name := load.Command
	path, err := resolveBin(baseDir, name, binOverride)
	if err != nil {
		return Output{Tool: "command", Warn: fmt.Sprintf("%s not found; running chaos only", name)}, nil
	}
	args := append([]string{}, load.Args...)
	if scen, err := resolveScenario(baseDir, load.Scenario); err != nil {
		return Output{}, err
	} else if scen != "" {
		args = append(args, scen)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	stdout, err := cmd.Output()
	out := Output{Tool: "command", Raw: asRaw(stdout)}
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok {
			msg = truncate(string(ee.Stderr), 400)
		}
		return out, fmt.Errorf("%s: %s", name, msg)
	}
	pts, perr := seriesFromCommand(stdout)
	if perr != nil {
		out.Warn = perr.Error()
		return out, nil
	}
	out.Series = pts
	return out, nil
}

type commandDoc struct {
	TimeSeries []LoadPoint `json:"time_series"`
}

func seriesFromCommand(raw []byte) ([]LoadPoint, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("load command returned empty stdout; want JSON time_series")
	}
	var doc commandDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("load command stdout is not JSON time_series: %w", err)
	}
	if len(doc.TimeSeries) == 0 {
		return nil, fmt.Errorf("load command JSON has no time_series")
	}
	return doc.TimeSeries, nil
}
