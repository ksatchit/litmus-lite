//go:build windows

package faults

import (
	"context"
	"fmt"
	"runtime"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type processPause struct{}

func (processPause) Name() string { return "process.pause" }
func (processPause) OS() []string { return []string{"linux", "darwin"} }
func (processPause) Validate(scenario.Fault) error {
	return fmt.Errorf("process.pause is not supported on %s", runtime.GOOS)
}
func (processPause) Start(context.Context, scenario.Fault) (Running, error) {
	return nil, processPause{}.Validate(scenario.Fault{})
}

type processKill struct{}

func (processKill) Name() string { return "process.kill" }
func (processKill) OS() []string { return []string{"linux", "darwin"} }
func (processKill) Validate(scenario.Fault) error {
	return fmt.Errorf("process.kill is not supported on %s", runtime.GOOS)
}
func (processKill) Start(context.Context, scenario.Fault) (Running, error) {
	return nil, processKill{}.Validate(scenario.Fault{})
}
