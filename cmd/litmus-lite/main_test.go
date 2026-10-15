package main

import (
	"os"
	"testing"
)

// TestMain keeps the tests away from the machine they run on: the live view is
// off unless a test turns it on, and its identity goes to a throwaway
// directory. Without this, `go test` would stream to a bridge running on the
// developer's machine and write into their real config directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "litmus-lite-test-config-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("LITMUS_LITE_CONFIG_DIR", dir)
	os.Setenv("LITMUS_LITE_LIVE", "off")
	os.Setenv("LITMUS_LITE_OPEN", "off")
	os.Unsetenv("LITMUS_LITE_LIVE_URL")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
