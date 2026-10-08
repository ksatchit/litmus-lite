package faults

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

const maxDiskFill = 256 << 20 // 256 MiB hard cap

type diskFill struct{}

func (diskFill) Name() string { return "disk.fill" }
func (diskFill) OS() []string { return nil }

func (diskFill) Validate(f scenario.Fault) error {
	n, err := parseBytes(scenario.StringParam(f.Params, "size"), 16<<20)
	if err != nil {
		return fmt.Errorf("disk.fill size: %w", err)
	}
	if n <= 0 {
		return fmt.Errorf("disk.fill size must be > 0")
	}
	if n > maxDiskFill {
		return fmt.Errorf("disk.fill size cap is 256MB")
	}
	p := scenario.StringParam(f.Params, "path")
	if p == "/" || p == "\\" || len(p) == 2 && p[1] == ':' {
		return fmt.Errorf("disk.fill refuses path %q", p)
	}
	return nil
}

func (d diskFill) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := d.Validate(f); err != nil {
		return nil, err
	}
	n, _ := parseBytes(scenario.StringParam(f.Params, "size"), 16<<20)
	p := scenario.StringParam(f.Params, "path")
	if p == "" {
		p = filepath.Join(os.TempDir(), "litmus-lite-fill.bin")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	fh, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	chunk := make([]byte, 1<<16)
	var written int64
	for written < n {
		want := int64(len(chunk))
		if n-written < want {
			want = n - written
		}
		if _, err := fh.Write(chunk[:want]); err != nil {
			fh.Close()
			_ = os.Remove(p)
			return nil, err
		}
		written += want
	}
	_ = fh.Sync()
	if err := fh.Close(); err != nil {
		_ = os.Remove(p)
		return nil, err
	}
	return stopPair{
		intensity: float64(n),
		stop: func() error {
			return os.Remove(p)
		},
	}, nil
}

func parseBytes(s string, def int64) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	u := strings.ToUpper(s)
	mult := int64(1)
	switch {
	case strings.HasSuffix(u, "GB"):
		return 0, fmt.Errorf("GB not allowed (cap 256MB)")
	case strings.HasSuffix(u, "MB"):
		mult = 1 << 20
		u = strings.TrimSuffix(u, "MB")
	case strings.HasSuffix(u, "KB"):
		mult = 1 << 10
		u = strings.TrimSuffix(u, "KB")
	case strings.HasSuffix(u, "B"):
		u = strings.TrimSuffix(u, "B")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(u), 64)
	if err != nil {
		return 0, err
	}
	return int64(n * float64(mult)), nil
}
