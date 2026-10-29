package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadToolFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.chaos.yaml")
	body := []byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: t }
faults:
  - { name: a, kind: http.latency, duration: 1s }
probes:
  - { name: p, type: http, url: http://127.0.0.1:1/ }
load:
  tool: command
  command: litmus-lite-no-such-load
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, c := range Run("", path) {
		if c.Name == "command" {
			got = c.Level + " " + c.Message
		}
	}
	if !strings.Contains(got, "warn") || !strings.Contains(got, "not found") {
		t.Fatalf("load check %q", got)
	}
}

// fakeGenerator writes a vegaload stand-in that prints version and lists
// -stream-file in `run -h` only when stream is true.
func fakeVegaLoad(t *testing.T, dir, version string, stream bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake generator is a shell script")
	}
	flag := "0"
	if stream {
		flag = "1"
	}
	body := "#!/bin/sh\n" +
		"[ \"$1\" = version ] && { echo 'vegaload " + version + "'; exit 0; }\n" +
		"[ \"$1\" = run ] && { echo '  -no-open'; echo '  -report string'; [ " + flag + " = 1 ] && echo '  -stream-file string'; exit 2; }\n"
	path := filepath.Join(dir, "vegaload")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// The first start of a new executable can be slow on macOS: do it now, not
	// inside the check under test.
	_ = exec.Command(path, "version").Run()
}

func vegaloadCheck(t *testing.T) Check {
	t.Helper()
	for _, c := range Run("", "") {
		if c.Name == "vegaload" {
			return c
		}
	}
	t.Fatal("no vegaload line in doctor")
	return Check{}
}

// doctor says which vegaload a run would use: its version, where it is and how
// it was found.
func TestDoctorShowsTheGeneratorVersionAndPath(t *testing.T) {
	dir := t.TempDir()
	fakeVegaLoad(t, dir, "1.4.0", true)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := vegaloadCheck(t)
	if c.Level != "ok" || !strings.Contains(c.Message, "vegaload 1.4.0 at ") || !strings.Contains(c.Message, "(PATH)") {
		t.Fatalf("%+v", c)
	}
}

// An old vegaload (no -stream-file) is a warning with the reason, not an ok.
func TestDoctorWarnsWhenTheGeneratorCannotStream(t *testing.T) {
	dir := t.TempDir()
	fakeVegaLoad(t, dir, "0.9.0", false)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := vegaloadCheck(t)
	if c.Level != "warn" || !strings.Contains(c.Message, "vegaload 0.9.0") || !strings.Contains(c.Message, "no -stream-file") {
		t.Fatalf("%+v", c)
	}
}

func TestDoctorWarnsWhenTheGeneratorIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := vegaloadCheck(t)
	if c.Level != "warn" || !strings.Contains(c.Message, "not found") {
		t.Fatalf("%+v", c)
	}
}
