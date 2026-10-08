//go:build unix

package faults

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

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
	if raw := scenario.StringParam(f.Params, "sigkillAfter"); raw != "" {
		if _, err := time.ParseDuration(raw); err != nil {
			return fmt.Errorf("process.kill sigkillAfter: %w", err)
		}
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
	if pid <= 1 || pid == os.Getpid() {
		return nil, fmt.Errorf("refusing to kill pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return nil, fmt.Errorf("SIGTERM %d: %w", pid, err)
	}
	wait := 2 * time.Second
	if raw := scenario.StringParam(f.Params, "sigkillAfter"); raw != "" {
		wait, _ = time.ParseDuration(raw)
	}
	stopCh := make(chan struct{})
	var once sync.Once
	escalate := func() {
		_ = ignoreGone(proc.Signal(syscall.SIGKILL))
	}
	if wait <= 0 {
		escalate()
	} else {
		go func() {
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-t.C:
				escalate()
			case <-stopCh:
			}
		}()
	}
	return stopPair{
		intensity: 100,
		stop: func() error {
			once.Do(func() { close(stopCh) })
			return nil
		},
	}, nil
}

func ignoreGone(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.ESRCH {
		return nil
	}
	return err
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
