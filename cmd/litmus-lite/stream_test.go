package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLabelFlag(t *testing.T) {
	l := labelFlag{}
	if err := l.Set("service=checkout"); err != nil {
		t.Fatal(err)
	}
	if err := l.Set("env=a=b"); err != nil || l["env"] != "a=b" {
		t.Fatalf("env label: %v %v", err, l)
	}
	if err := l.Set("novalue"); err == nil {
		t.Fatal("want an error for a label without =")
	}
}

func TestStreamFromReportAndValidate(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "run.ndjson")
	fixture := filepath.Join("..", "..", "docs", "stream", "fixtures", "ignite.report.json")
	if code := run([]string{"litmus-lite", "stream", "from-report", fixture, "-out", out, "-label", "service=launchpad", "-run-id", "fixed"}); code != 0 {
		t.Fatalf("from-report exit %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"runId":"fixed"`) || !strings.Contains(string(b), `"service":"launchpad"`) {
		t.Fatalf("flags after the file were ignored: %.200s", b)
	}
	if code := run([]string{"litmus-lite", "stream", "validate", out}); code != 0 {
		t.Fatalf("validate exit %d", code)
	}
	bad := filepath.Join(dir, "bad.ndjson")
	_ = os.WriteFile(bad, []byte("{nope}\n"), 0o644)
	if code := run([]string{"litmus-lite", "stream", "validate", bad}); code != 1 {
		t.Fatalf("validate of a bad file exit %d, want 1", code)
	}
	if code := run([]string{"litmus-lite", "stream"}); code != 2 {
		t.Fatalf("stream with no subcommand exit %d, want 2", code)
	}
}

func TestStreamReplayRefusesOpenBindWithoutToken(t *testing.T) {
	t.Setenv("LITMUS_LITE_STREAM_TOKEN", "")
	fixture := filepath.Join("..", "..", "docs", "stream", "fixtures", "run-failed.ndjson")
	if code := run([]string{"litmus-lite", "stream", "replay", fixture, "-stream-pull", "0.0.0.0:0"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
