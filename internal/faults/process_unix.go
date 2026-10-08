//go:build unix

package faults

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ksatchit/litmus-lite/internal/safety"
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

func (p processPause) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	if err := p.Validate(f); err != nil {
		return nil, err
	}
	pid, proc, err := signalTarget(ctx, f)
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

func (p processKill) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	if err := p.Validate(f); err != nil {
		return nil, err
	}
	pid, proc, err := signalTarget(ctx, f)
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

func signalTarget(ctx context.Context, f scenario.Fault) (int, *os.Process, error) {
	pid, err := resolvePID(ctx, f)
	if err != nil {
		return 0, nil, err
	}
	if pid <= 1 || pid == os.Getpid() {
		return 0, nil, fmt.Errorf("refusing to signal pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, nil, err
	}
	return pid, proc, nil
}

func resolvePID(ctx context.Context, f scenario.Fault) (int, error) {
	allow := safety.AllowPIDsFrom(ctx)
	if pid := scenario.IntParam(f.Params, "pid", 0); pid > 0 {
		if !safety.PIDAllowed(pid, allow) {
			return 0, fmt.Errorf("pid %d is not allowed; pass -allow-pid %d", pid, pid)
		}
		return pid, nil
	}
	want := scenario.StringParam(f.Params, "command")
	found, err := pidsForCommand(want)
	if err != nil {
		return 0, err
	}
	var allowed []int
	for _, pid := range found {
		if safety.PIDAllowed(pid, allow) {
			allowed = append(allowed, pid)
		}
	}
	switch len(allowed) {
	case 1:
		return allowed[0], nil
	case 0:
		return 0, fmt.Errorf("command %q matched pid(s) %s; none are listed in -allow-pid", want, formatPIDs(found))
	default:
		return 0, fmt.Errorf("command %q matches more than one allowed pid (%s); set params.pid to the one process to signal", want, formatPIDs(allowed))
	}
}

func pidsForCommand(command string) ([]int, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}
	var found []int
	if ents, err := os.ReadDir("/proc"); err == nil {
		found = pidsFromProc(ents, command)
	}
	if len(found) == 0 {
		return pidsFromPS(command)
	}
	return found, nil
}

func pidsFromProc(ents []os.DirEntry, command string) []int {
	_ = runtime.GOOS
	var found []int
	seen := map[int]struct{}{}
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
		if !commandMatches(string(b), command) {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		found = append(found, pid)
	}
	return found
}

func commandMatches(name, want string) bool {
	name = strings.TrimSpace(name)
	want = strings.TrimSpace(want)
	if name == "" || want == "" {
		return false
	}
	return name == want || strings.Contains(name, want)
}

func formatPIDs(pids []int) string {
	const max = 12
	n := len(pids)
	if n > max {
		pids = pids[:max]
	}
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	s := strings.Join(parts, ", ")
	if n > max {
		s += fmt.Sprintf(", and %d more", n-max)
	}
	return s
}
