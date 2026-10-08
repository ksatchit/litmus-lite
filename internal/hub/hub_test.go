package hub

import (
	"os"
	"path/filepath"
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

func TestUnknown(t *testing.T) {
	if _, err := Get("nope"); err == nil {
		t.Fatal("expected error")
	}
}
