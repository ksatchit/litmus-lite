package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// move-to offers the adapters that exist: ir and litmus. A vendor-named
// adapter is not one of them.
func TestMoveToKnowsOnlyIRAndLitmus(t *testing.T) {
	file := filepath.Join("..", "..", "examples", "launchpad", "ignite.chaos.yaml")
	for _, adapter := range []string{"ir", "litmus"} {
		if code, errOut := captureRun(t, "move-to", "-adapter", adapter, file); code != 0 {
			t.Fatalf("adapter %s: exit %d: %s", adapter, code, errOut)
		}
	}
	code, errOut := captureRun(t, "move-to", "-adapter", "harness", file)
	if code != 1 || !strings.Contains(errOut, "unknown adapter") {
		t.Fatalf("a removed adapter must be unknown: exit %d: %s", code, errOut)
	}
}
