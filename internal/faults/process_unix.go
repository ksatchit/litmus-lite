//go:build unix

package faults

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type processPause struct{}

func (processPause) Name() string { return "process.pause" }
func (processPause) OS() []string { return []string{"linux", "darwin"} }

func (processPause) Validate(f scenario.Fault) error {
	if scenario.IntParam(f.Params, "pid", 0) == 0 && scenario.StringParam(f.Params, "command") == "" {
		return fmt.Errorf("process.pause needs params.pid or params.command")
	}
	return nil
}

func (p processPause) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := p.Validate(f); err != nil {
		return nil, err
	}
	pid, err := resolvePID(f)
	if err != nil {
		return nil, err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	if err := proc.Signal(syscall.SIGSTOP); err != nil {
		return nil, fmt.Errorf("SIGSTOP %d: %w", pid, err)
	}
	return stopPair{
		intensity: 100,
		stop: func() error {
			return proc.Signal(syscall.SIGCONT)
		},
	}, nil
}

type processKill struct{}

func (processKill) Name() string { return "process.kill" }
func (processKill) OS() []string { return []string{"linux", "darwin"} }

func (processKill) Validate(f scenario.Fault) error {
	if scenario.IntParam(f.Params, "pid", 0) == 0 && scenario.StringParam(f.Params, "command") == "" {
		return fmt.Errorf("process.kill needs params.pid or params.command")
	}
	return nil
}

func (p processKill) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := p.Validate(f); err != nil {
		return nil, err
	}
	pid, err := resolvePID(f)
	if err != nil {
		return nil, err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return nil, err
	}
	return stopPair{
		intensity: 100,
		stop:      func() error { return nil },
	}, nil
}

func resolvePID(f scenario.Fault) (int, error) {
	if pid := scenario.IntParam(f.Params, "pid", 0); pid > 0 {
		return pid, nil
	}
	want := scenario.StringParam(f.Params, "command")
	ents, err := os.ReadDir("/proc")
	if err != nil {
		// macOS: best-effort via ps
		return pidFromPS(want)
	}
	_ = runtime.GOOS
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil || pid <= 0 {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == want {
			return pid, nil
		}
	}
	return pidFromPS(want)
}
