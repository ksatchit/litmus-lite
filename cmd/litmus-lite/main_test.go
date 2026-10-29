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
	// A test can run this binary as the CLI (see interrupt_unix_test.go), to
	// test what only a real process shows: signals and exit codes.
	if os.Getenv("LITMUS_LITE_TEST_AS_CLI") == "1" {
		code := run(os.Args)
		os.RemoveAll(dir)
		os.Exit(code)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
