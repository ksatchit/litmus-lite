package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImport(t *testing.T) {
	dir := t.TempDir()
	p, err := Import("http.latency", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir {
		t.Fatal(p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 50 {
		t.Fatal("template")
	}
}

func TestImportProcessKill(t *testing.T) {
	dir := t.TempDir()
	p, err := Import("process.kill", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "kind: process.kill") {
		t.Fatal(string(b))
	}
}

func TestImportPhase2(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"docker.pause", "cpu.hog", "memory.hog", "disk.fill"} {
		p, err := Import(id, dir, "")
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "kind: "+id) {
			t.Fatalf("%s: %s", id, b)
		}
	}
}

func TestUnknown(t *testing.T) {
	if _, err := Get("nope"); err == nil {
		t.Fatal("expected error")
	}
}
