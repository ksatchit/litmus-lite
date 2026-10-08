package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateRejectsBadAllowPID(t *testing.T) {
	path := writeScenario(t, `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: a
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:1", upstream: "127.0.0.1:2" }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`)
	code, errText := captureRun(t, "validate", "-allow-pid", "nope", path)
	if code != 2 || !strings.Contains(errText, "allow-pid") {
		t.Fatalf("code %d err %q", code, errText)
	}
}

func TestValidateProcessRequiresAllowPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process.pause is rejected as unsupported before the pid allowlist")
	}
	path := writeScenario(t, `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: freeze
    kind: process.pause
    duration: 1s
    params: { pid: 4242 }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`)
	code, errText := captureRun(t, "validate", path)
	if code == 0 || !strings.Contains(errText, "allow-pid") {
		t.Fatalf("code %d err %q", code, errText)
	}
	code, errText = captureRun(t, "validate", "-allow-pid", "4242", path)
	if code != 0 {
		t.Fatalf("code %d err %q", code, errText)
	}
}

func TestValidateDiskFillRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "app.db")
	if err := os.WriteFile(target, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := writeScenario(t, `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - name: fill
    kind: disk.fill
    duration: 1s
    params: { path: "`+filepath.ToSlash(target)+`", size: 1KB }
probes:
  - name: p
    type: http
    url: http://127.0.0.1:1/
`)
	code, errText := captureRun(t, "validate", "-yes", path)
	if code == 0 || !strings.Contains(errText, "overwrite") {
		t.Fatalf("code %d err %q", code, errText)
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "keep-me" {
		t.Fatalf("file changed: %q %v", b, err)
	}
}

func writeScenario(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.chaos.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func captureRun(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := run(append([]string{"litmus-lite"}, args...))
	_ = w.Close()
	os.Stderr = old
	b, _ := io.ReadAll(r)
	_ = r.Close()
	return code, string(b)
}
