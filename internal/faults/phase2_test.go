package faults

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestCPUHogStops(t *testing.T) {
	r, err := cpuHog{}.Start(context.Background(), scenario.Fault{
		Params: map[string]any{"workers": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryHogCap(t *testing.T) {
	err := memoryHog{}.Validate(scenario.Fault{Params: map[string]any{"size": "1GB"}})
	if err == nil {
		t.Fatal("cap")
	}
	r, err := memoryHog{}.Start(context.Background(), scenario.Fault{
		Params: map[string]any{"size": "1MB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskFillRollback(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fill.bin")
	r, err := diskFill{}.Start(context.Background(), scenario.Fault{
		Params: map[string]any{"path": p, "size": "64KB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() < 64*1024 {
		t.Fatalf("size %v %v", st, err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be removed")
	}
}

func TestDiskFillCap(t *testing.T) {
	err := diskFill{}.Validate(scenario.Fault{Params: map[string]any{"size": "1GB"}})
	if err == nil {
		t.Fatal("cap")
	}
}

func TestDockerPauseValidate(t *testing.T) {
	err := dockerPause{}.Validate(scenario.Fault{Params: map[string]any{}})
	if err == nil {
		t.Fatal("need name")
	}
	err = dockerPause{}.Validate(scenario.Fault{Params: map[string]any{"name": "litmus-lite-no-such"}})
	if _, look := exec.LookPath("docker"); look != nil {
		if err == nil || !strings.Contains(err.Error(), "docker CLI") {
			t.Fatalf("want missing docker error, got %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestLookupPhase2Kinds(t *testing.T) {
	for _, k := range []string{"docker.pause", "cpu.hog", "memory.hog", "disk.fill"} {
		if _, err := Lookup(k); err != nil {
			t.Fatal(err)
		}
	}
}
