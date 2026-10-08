package faults

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

const maxMemoryHog = 256 << 20 // 256 MiB

type cpuHog struct{}

func (cpuHog) Name() string { return "cpu.hog" }
func (cpuHog) OS() []string { return nil }

func (cpuHog) Validate(f scenario.Fault) error {
	n := scenario.IntParam(f.Params, "workers", 1)
	if n < 1 {
		return fmt.Errorf("cpu.hog workers must be >= 1")
	}
	if n > 16 {
		return fmt.Errorf("cpu.hog workers cap is 16")
	}
	return nil
}

func (c cpuHog) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := c.Validate(f); err != nil {
		return nil, err
	}
	n := scenario.IntParam(f.Params, "workers", 1)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var once sync.Once
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var x uint64
			for {
				select {
				case <-stop:
					return
				default:
					for j := 0; j < 1<<18; j++ {
						x += uint64(j)
					}
				}
			}
		}()
	}
	return stopPair{
		intensity: float64(n),
		stop: func() error {
			once.Do(func() { close(stop) })
			wg.Wait()
			return nil
		},
	}, nil
}

type memoryHog struct{}

func (memoryHog) Name() string { return "memory.hog" }
func (memoryHog) OS() []string { return nil }

func (memoryHog) Validate(f scenario.Fault) error {
	n, err := parseBytes(scenario.StringParam(f.Params, "size"), 32<<20)
	if err != nil {
		return fmt.Errorf("memory.hog size: %w", err)
	}
	if n <= 0 {
		return fmt.Errorf("memory.hog size must be > 0")
	}
	if n > maxMemoryHog {
		return fmt.Errorf("memory.hog size cap is 256MB")
	}
	return nil
}

func (m memoryHog) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := m.Validate(f); err != nil {
		return nil, err
	}
	n, _ := parseBytes(scenario.StringParam(f.Params, "size"), 32<<20)
	buf := make([]byte, n)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	return stopPair{
		intensity: float64(n),
		stop: func() error {
			for i := range buf {
				buf[i] = 0
			}
			buf = nil
			runtime.GC()
			return nil
		},
	}, nil
}
