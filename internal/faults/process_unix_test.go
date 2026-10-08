//go:build unix

package faults

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/safety"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestProcessKillTerminatesHelper(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	f := scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "2s",
		Params: map[string]any{"pid": cmd.Process.Pid, "sigkillAfter": "50ms"},
	}
	ctx := safety.WithAllowPIDs(context.Background(), []int{cmd.Process.Pid})
	r, err := processKill{}.Start(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err == nil {
			t.Fatal("expected non-zero exit from killed helper")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("helper still running")
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessKillUnknown(t *testing.T) {
	_, err := processKill{}.Start(context.Background(), scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "1s",
		Params: map[string]any{"command": "no-such-litmus-lite-process-xyz"},
	})
	if err == nil {
		t.Fatal("expected unknown command")
	}
	ctx := safety.WithAllowPIDs(context.Background(), []int{999999})
	_, err = processKill{}.Start(ctx, scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "1s",
		Params: map[string]any{"pid": 999999},
	})
	if err == nil {
		t.Fatal("expected unknown pid")
	}
}

func TestProcessKillRefusesUnlistedPID(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})

	ctx := safety.WithAllowPIDs(context.Background(), []int{os.Getpid()})
	_, err := processKill{}.Start(ctx, scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "1s",
		Params: map[string]any{"pid": cmd.Process.Pid},
	})
	if err == nil || !strings.Contains(err.Error(), "allow-pid") {
		t.Fatalf("expected allow-pid refusal, got %v", err)
	}
	select {
	case <-done:
		t.Fatal("process exited")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestProcessPauseRefusesSelf(t *testing.T) {
	ctx := safety.WithAllowPIDs(context.Background(), []int{os.Getpid()})
	_, err := processPause{}.Start(ctx, scenario.Fault{
		Params: map[string]any{"pid": os.Getpid()},
	})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessKillCommandUsesAllowList(t *testing.T) {
	cmd := uniqueHelper(t)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	select {
	case <-done:
		t.Fatal("helper exited before the fault")
	case <-time.After(50 * time.Millisecond):
	}

	ctx := safety.WithAllowPIDs(context.Background(), []int{os.Getpid()})
	_, err := processKill{}.Start(ctx, scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "1s",
		Params: map[string]any{"command": "ll-pause-target"},
	})
	if err == nil {
		t.Fatal("expected refusal when the matched pid is not allowlisted")
	}
	select {
	case <-done:
		t.Fatal("process exited")
	case <-time.After(200 * time.Millisecond):
	}
}

func uniqueHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ll-pause-target")
	if err := os.Symlink(sleepBin, bin); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestProcessKillValidate(t *testing.T) {
	err := processKill{}.Validate(scenario.Fault{Params: map[string]any{}})
	if err == nil {
		t.Fatal("need pid or command")
	}
	err = processKill{}.Validate(scenario.Fault{Params: map[string]any{"pid": 1234, "sigkillAfter": "bogus"}})
	if err == nil {
		t.Fatal("bad sigkillAfter")
	}
}
