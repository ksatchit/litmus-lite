//go:build windows

package faults

import (
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestProcessKillUnsupported(t *testing.T) {
	err := processKill{}.Validate(scenario.Fault{
		Params: map[string]any{"pid": 1},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want validate-time error, got %v", err)
	}
}
