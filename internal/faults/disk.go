package faults

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	if _, err := prepareDiskPath(scenario.StringParam(f.Params, "path")); err != nil {
		return err
	}
	return nil
}

func (d diskFill) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := d.Validate(f); err != nil {
		return nil, err
	}
	n, err := parseBytes(scenario.StringParam(f.Params, "size"), 16<<20)
	if err != nil {
		return nil, err
	}
	p, err := prepareDiskPath(scenario.StringParam(f.Params, "path"))
	if err != nil {
		return nil, err
	}
	if p == "" {
		p, err = defaultFillPath()
		if err != nil {
			return nil, err
		}
	}
	// O_EXCL: never truncate a file that already exists. Stop deletes only this new file.
	fh, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("disk.fill refuses to overwrite existing path %q", p)
		}
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

// prepareDiskPath cleans a user path and refuses roots and existing files.
// An empty path means "use the litmus-lite temp file" and returns "".
func prepareDiskPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	p, err := cleanDiskPath(raw)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(p); err == nil {
		return "", fmt.Errorf("disk.fill refuses to overwrite existing path %q", p)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("disk.fill path: %w", err)
	}
	parent := filepath.Dir(p)
	st, err := os.Stat(parent)
	if err != nil {
		return "", fmt.Errorf("disk.fill parent directory: %w", err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("disk.fill parent %q is not a directory", parent)
	}
	return p, nil
}

func cleanDiskPath(p string) (string, error) {
	expanded, err := expandHome(p)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(expanded)
	if diskPathRefused(cleaned) {
		return "", fmt.Errorf("disk.fill refuses path %q", p)
	}
	return cleaned, nil
}

func diskPathRefused(p string) bool {
	if p == "" || p == "." || p == "/" || p == `\` || p == string(filepath.Separator) {
		return true
	}
	if len(p) == 2 && p[1] == ':' {
		return true
	}
	if len(p) == 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	rest = strings.Trim(rest, `/\`)
	return rest == ""
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("disk.fill home: %w", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

func defaultFillPath() (string, error) {
	dir := filepath.Join(os.TempDir(), "litmus-lite")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("fill-%d-%d.bin", os.Getpid(), time.Now().UnixNano())), nil
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
