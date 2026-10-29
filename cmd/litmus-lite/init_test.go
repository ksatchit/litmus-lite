package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// mcpEnv reads the env of the litmus-lite server in both MCP files.
func mcpEnv(t *testing.T, dir string) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for _, f := range []string{".cursor/mcp.json", ".mcp.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		var root struct {
			Servers map[string]struct {
				Command string            `json:"command"`
				Env     map[string]string `json:"env"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &root); err != nil {
			t.Fatal(err)
		}
		out[f] = root.Servers["litmus-lite"].Env
	}
	return out
}

func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func TestInitEnvIsWrittenIntoTheMCPEntry(t *testing.T) {
	dir := inTempDir(t)
	code := cmdInit([]string{"-editor", "cursor",
		"-env", "LITMUS_LITE_LIVE_URL=http://127.0.0.1:7080",
		"-env", "LITMUS_LITE_CONFIG_DIR=shared/live"})
	if code != 0 {
		t.Fatalf("init exit %d", code)
	}
	want, _ := filepath.Abs("shared/live") // a relative directory would mean something else to the editor
	for f, env := range mcpEnv(t, dir) {
		if env["LITMUS_LITE_LIVE_URL"] != "http://127.0.0.1:7080" {
			t.Errorf("%s: LITMUS_LITE_LIVE_URL = %q", f, env["LITMUS_LITE_LIVE_URL"])
		}
		if env["LITMUS_LITE_CONFIG_DIR"] != want {
			t.Errorf("%s: LITMUS_LITE_CONFIG_DIR = %q, want %q", f, env["LITMUS_LITE_CONFIG_DIR"], want)
		}
	}

	// A re-run without -env keeps what was there; KEY= removes one.
	if code := cmdInit([]string{"-editor", "cursor"}); code != 0 {
		t.Fatal("re-run")
	}
	for f, env := range mcpEnv(t, dir) {
		if env["LITMUS_LITE_LIVE_URL"] == "" {
			t.Errorf("%s: a re-run dropped the env", f)
		}
	}
	if code := cmdInit([]string{"-editor", "cursor", "-env", "LITMUS_LITE_LIVE_URL="}); code != 0 {
		t.Fatal("remove")
	}
	for f, env := range mcpEnv(t, dir) {
		if _, ok := env["LITMUS_LITE_LIVE_URL"]; ok || env["LITMUS_LITE_CONFIG_DIR"] == "" {
			t.Errorf("%s: after KEY= the env is %v", f, env)
		}
	}
}

func TestInitWritesACleanCommandPath(t *testing.T) {
	dir := inTempDir(t)
	old := executable
	executable = func() (string, error) {
		return dir + string(filepath.Separator) + "proj" + string(filepath.Separator) + ".." + string(filepath.Separator) + "bin" + string(filepath.Separator) + "litmus-lite", nil
	}
	t.Cleanup(func() { executable = old })
	if code := cmdInit([]string{"-editor", "cursor"}); code != 0 {
		t.Fatal("init")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var root struct {
		Servers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &root)
	if c, want := root.Servers["litmus-lite"].Command, filepath.Join(dir, "bin", "litmus-lite"); c != want {
		t.Fatalf("command %q, want %q", c, want)
	}
}

func TestInitRefusesWhatDoesNotBelongInAProjectFile(t *testing.T) {
	for _, bad := range []string{
		"NOEQUALS",
		"1BAD=x",
		"LITMUS_LITE_STREAM_TOKEN=abc",
		"MY_API_KEY=abc",
		"LITMUS_LITE_STREAM_PUSH=https://user:pw@ingest.example/v1",
	} {
		dir := inTempDir(t)
		if code := cmdInit([]string{"-env", bad}); code != 2 {
			t.Errorf("-env %s: exit %d, want 2", bad, code)
		}
		if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err == nil {
			t.Errorf("-env %s: a file was written", bad)
		}
	}
}

func TestInitNoLongerKnowsLiveDir(t *testing.T) {
	inTempDir(t)
	if code := cmdInit([]string{"-live-dir", "x"}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestAbsDirExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this one on Windows
	got, err := absDir("~/rt-live")
	if err != nil || got != filepath.Join(home, "rt-live") {
		t.Fatalf("absDir = %q, %v", got, err)
	}
}
