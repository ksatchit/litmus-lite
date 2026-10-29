package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testFlags() (*flag.FlagSet, *bool, *string) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs, fs.Bool("strict", false, ""), fs.String("out", "", "")
}

func TestParseArgsReadsFlagsAfterPositionalArguments(t *testing.T) {
	fs, strict, out := testFlags()
	if err := parseArgs(fs, []string{"a.yaml", "-strict", "b.yaml", "-out", "r.json"}); err != nil {
		t.Fatal(err)
	}
	if !*strict || *out != "r.json" || !reflect.DeepEqual(fs.Args(), []string{"a.yaml", "b.yaml"}) {
		t.Fatalf("strict %v out %q args %v", *strict, *out, fs.Args())
	}
}

func TestParseArgsKeepsEverythingAfterTheTerminatorPositional(t *testing.T) {
	fs, strict, _ := testFlags()
	if err := parseArgs(fs, []string{"-out", "r.json", "--", "a.yaml", "-strict"}); err != nil {
		t.Fatal(err)
	}
	if *strict || !reflect.DeepEqual(fs.Args(), []string{"a.yaml", "-strict"}) {
		t.Fatalf("strict %v args %v", *strict, fs.Args())
	}
}

func TestParseArgsRefusesAnUnknownFlagAfterAPositionalArgument(t *testing.T) {
	fs, _, _ := testFlags()
	if err := parseArgs(fs, []string{"a.yaml", "-bogus"}); err == nil {
		t.Fatal("an unknown flag after the file was accepted")
	}
}

// `validate FILE -strict` was not strict: the flag after the file was dropped.
func TestValidateStrictAfterTheFileIsStrict(t *testing.T) {
	f := writeScenario(t, plainScenario) // has warnings
	if code, _, _ := run2(t, "validate", f, "-strict"); code != 1 {
		t.Fatalf("validate FILE -strict: exit %d, want 1", code)
	}
	if code, _, _ := run2(t, "validate", f, "-bogus"); code != 2 {
		t.Fatalf("validate FILE -bogus: exit %d, want 2", code)
	}
}

// `hub import ID -beside DIR` wrote into the current directory.
func TestHubImportBesideAfterTheID(t *testing.T) {
	inTempDir(t)
	dir := filepath.Join(t.TempDir(), "svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out, errText := run2(t, "hub", "import", "http.latency", "-beside", dir); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errText)
	}
	if _, err := os.Stat(filepath.Join(dir, "http-latency.chaos.yaml")); err != nil {
		t.Fatalf("not written beside %s: %v", dir, err)
	}
	if _, err := os.Stat("http-latency.chaos.yaml"); err == nil {
		t.Fatal("written into the current directory instead")
	}
}

// The MCP server passes the file after "--"; a name that looks like a flag
// must stay a file name.
func TestAFileAfterTheTerminatorIsNeverAFlag(t *testing.T) {
	inTempDir(t)
	code, _, errText := run2(t, "validate", "--", "-strict")
	if code != 1 || !strings.Contains(errText+"", "-strict") {
		t.Fatalf("exit %d, stderr %q", code, errText)
	}
}
