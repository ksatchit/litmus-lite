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

func TestDiskFillRefusesExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(p, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := diskFill{}.Start(context.Background(), scenario.Fault{
		Params: map[string]any{"path": p, "size": "1KB"},
	})
	if err == nil {
		t.Fatal("expected refuse")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "keep-me" {
		t.Fatalf("file changed: %q %v", b, err)
	}
}

func TestDiskFillExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := expandHome("~/work/app.db")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "work", "app.db") {
		t.Fatalf("expanded %q", got)
	}
	err = diskFill{}.Validate(scenario.Fault{Params: map[string]any{"path": "~", "size": "1KB"}})
	if err == nil {
		t.Fatal("home directory itself must be refused")
	}
}

func TestDiskFillRefusesRoot(t *testing.T) {
	err := diskFill{}.Validate(scenario.Fault{Params: map[string]any{"path": "/", "size": "1KB"}})
	if err == nil {
		t.Fatal("root path")
	}
	err = diskFill{}.Validate(scenario.Fault{Params: map[string]any{"path": `C:\`, "size": "1KB"}})
	if err == nil {
		t.Fatal("drive root")
	}
}

func TestDiskFillDoesNotCreateParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	p := filepath.Join(parent, "fill.bin")
	_, err := diskFill{}.Start(context.Background(), scenario.Fault{
		Params: map[string]any{"path": p, "size": "1KB"},
	})
	if err == nil {
		t.Fatal("expected missing parent")
	}
	if _, statErr := os.Stat(parent); !os.IsNotExist(statErr) {
		t.Fatalf("parent created: %v", statErr)
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
