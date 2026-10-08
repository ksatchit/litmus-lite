package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	cliOnce sync.Once
	cliPath string
	cliErr  error
)

func resolveCLI() (string, error) {
	if p := os.Getenv("LITMUS_LITE_BIN"); p != "" {
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if !isGoTestBinary(exe) {
		return exe, nil
	}
	cliOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			cliErr = err
			return
		}
		dest := filepath.Join(os.TempDir(), fmt.Sprintf("litmus-lite-mcp-eval-%d", os.Getpid()))
		cmd := exec.Command("go", "build", "-o", dest, "./cmd/litmus-lite")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			cliErr = fmt.Errorf("build litmus-lite for mcp eval: %s: %w", strings.TrimSpace(string(out)), err)
			return
		}
		cliPath = dest
	})
	return cliPath, cliErr
}

func isGoTestBinary(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".test") || strings.Contains(base, ".test")
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("go.mod not found from %s", dir)
}
