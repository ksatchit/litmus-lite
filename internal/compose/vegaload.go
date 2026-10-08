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

func Run(ctx context.Context, bin string, load *scenario.LoadSpec) (json.RawMessage, string, error) {
	if load == nil || load.Scenario == "" {
		return nil, "", nil
	}
	if bin == "" {
		bin = "vegaload"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, "vegaload not on PATH; running chaos only", nil
	}
	outFile := filepath.Join(os.TempDir(), fmt.Sprintf("litmus-lite-load-%d.json", os.Getpid()))
	args := []string{"run", "-output", "json", "-out", outFile}
	args = append(args, load.Args...)
	args = append(args, load.Scenario)
	cmd := exec.CommandContext(ctx, path, args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", err, truncate(string(b), 400))
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		// some vegaload versions print JSON on stdout
		if json.Valid(b) {
			return json.RawMessage(b), "", nil
		}
		return json.RawMessage(b), "vegaload ran but -out file missing; using stdout", nil
	}
	return json.RawMessage(raw), "", nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
