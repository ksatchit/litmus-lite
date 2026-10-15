package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitLiveDirIsWrittenAbsoluteIntoTheMCPEntry(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	if code := cmdInit([]string{"-editor", "cursor", "-live-dir", "shared/live"}); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	want, _ := filepath.Abs("shared/live")
	for _, f := range []string{".cursor/mcp.json", ".mcp.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		var root struct {
			Servers map[string]struct {
				Env map[string]string `json:"env"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &root); err != nil {
			t.Fatal(err)
		}
		if got := root.Servers["litmus-lite"].Env["LITMUS_LITE_CONFIG_DIR"]; got != want {
			t.Errorf("%s: LITMUS_LITE_CONFIG_DIR = %q, want %q", f, got, want)
		}
	}
}

func TestAbsDirExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := absDir("~/rt-live")
	if err != nil || got != filepath.Join(home, "rt-live") {
		t.Fatalf("absDir = %q, %v", got, err)
	}
}
