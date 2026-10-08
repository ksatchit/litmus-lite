package faults

import (
	"context"
	"fmt"
	"sync"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// Injector is one fault kind. Start must be reversible by Stop.
type Injector interface {
	Name() string
	OS() []string // empty means all
	Validate(f scenario.Fault) error
	Start(ctx context.Context, f scenario.Fault) (Running, error)
}

type Running interface {
	// Intensity is the injected signal for the overlay (e.g. delay milliseconds, or error percent).
	Intensity() float64
	Stop() error
}

type intensityFunc func() float64

func (f intensityFunc) Intensity() float64 { return f() }

type stopPair struct {
	intensity float64
	stop      func() error
}

func (s stopPair) Intensity() float64 { return s.intensity }
func (s stopPair) Stop() error        { return s.stop() }

var (
	mu       sync.RWMutex
	registry = map[string]Injector{}
)

func Register(in Injector) {
	mu.Lock()
	defer mu.Unlock()
	registry[in.Name()] = in
}

func Lookup(kind string) (Injector, error) {
	mu.RLock()
	defer mu.RUnlock()
	in, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("unknown fault kind %q", kind)
	}
	return in, nil
}

func List() []Injector {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Injector, 0, len(registry))
	for _, v := range registry {
		out = append(out, v)
	}
	return out
}

func init() {
	Register(httpProxy{})
	Register(processPause{})
	Register(processKill{})
}
