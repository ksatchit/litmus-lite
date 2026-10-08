//go:build unix

package faults

import (
	"context"
	"os/exec"
	"testing"
	"time"

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
	r, err := processKill{}.Start(context.Background(), f)
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
	_, err = processKill{}.Start(context.Background(), scenario.Fault{
		Name: "k", Kind: "process.kill", Duration: "1s",
		Params: map[string]any{"pid": 999999},
	})
	if err == nil {
		t.Fatal("expected unknown pid")
	}
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
