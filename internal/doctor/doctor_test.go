package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadToolFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.chaos.yaml")
	body := []byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - { name: a, kind: http.latency, duration: 1s }
probes:
  - { name: p, type: http, url: http://127.0.0.1:1/ }
load:
  tool: command
  command: litmus-lite-no-such-load
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, c := range Run("", path) {
		if c.Name == "command" {
			got = c.Level + " " + c.Message
		}
	}
	if !strings.Contains(got, "warn") || !strings.Contains(got, "not found") {
		t.Fatalf("load check %q", got)
	}
}
