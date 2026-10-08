package compare

import (
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/report"
)

func TestRegress(t *testing.T) {
	base := &report.Result{Phases: []report.Phase{{Name: "inject", ErrorRate: 0, P95: time.Millisecond}}}
	cand := &report.Result{Phases: []report.Phase{{Name: "inject", ErrorRate: 0.5, P95: time.Second}}}
	_, failed := Phases(base, cand)
	if !failed {
		t.Fatal("expected regression")
	}
}
