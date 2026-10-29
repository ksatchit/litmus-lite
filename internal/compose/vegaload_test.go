package compose

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// fakeGenerator is a shell script named vegaload, first on PATH. It behaves as
// a vegaload that lists -stream-file in `run -h` only when stream is true (the
// older one has -no-open and -report only). It records its command lines, notes
// when it would open a browser (no -no-open) and writes a report into its
// working directory when it is not told where to put one (no -report).
type fakeGenerator struct {
	log     string // one line per call
	browser string // exists when the generator "opened a browser"
}

// fakeScript is the vegaload stand-in; __VERSION__ and __STREAM__ (1 or 0) are
// filled in per copy, so that two copies on one machine can differ.
const fakeScript = `#!/bin/sh
[ -n "$FAKE_LOG" ] && echo "$*" >> "$FAKE_LOG"
if [ "$1" = "version" ]; then
  echo "vegaload __VERSION__"
  exit 0
fi
if [ "$1" = "run" ] && [ "$2" = "-h" ]; then
  echo "Usage: vegaload run [flags] [scenario-file]"
  echo "  -no-open"
  printf '    \tdo not open the report\n'
  echo "  -out string"
  echo "  -report string"
  [ "__STREAM__" = "1" ] && echo "  -stream-file string"
  exit 2
fi
out=""; report=""; stream=""; noopen=0
while [ $# -gt 0 ]; do
  case "$1" in
    -out) out="$2"; shift;;
    -report) report="$2"; shift;;
    -stream-file) stream="$2"; shift;;
    -no-open) noopen=1;;
  esac
  shift
done
[ $noopen = 0 ] && echo opened >> "$FAKE_BROWSER"
[ -z "$report" ] && echo "<html></html>" > vegaload-report-fake.html
[ -n "$stream" ] && printf '%s\n' '{"schema":"vegaload.io/stream/v1","runId":"r","seq":1,"type":"point","data":{"offsetS":1,"rps":7,"requests":7,"failed":0,"errorRate":0}}' >> "$stream"
printf '%s' '{"time_series":[{"offset_ns":0,"rps":10,"error_rate":0},{"offset_ns":1000000000,"rps":12,"error_rate":0.5}]}' > "$out"
`

// writeFakeVegaLoad writes a stand-in named vegaload into dir. It lists
// -stream-file in `run -h` only when stream is true and says version for
// `vegaload version`.
func writeFakeVegaLoad(t *testing.T, dir, version string, stream bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake generator is a shell script")
	}
	flag := "0"
	if stream {
		flag = "1"
	}
	body := strings.NewReplacer("__VERSION__", version, "__STREAM__", flag).Replace(fakeScript)
	path := filepath.Join(dir, "vegaload")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// Start it once now. The first start of a new executable can be slow on
	// macOS (the system checks it), and a test must not measure that.
	_ = exec.Command(path, "version").Run()
	return path
}

// installFakeVegaLoad puts a fake vegaload (version 1.2.3) first on PATH and
// moves the test into a scratch working directory, where a stray report would
// show.
func installFakeVegaLoad(t *testing.T, stream bool) fakeGenerator {
	t.Helper()
	dir := t.TempDir()
	f := fakeGenerator{log: filepath.Join(dir, "calls.log"), browser: filepath.Join(dir, "browser")}
	writeFakeVegaLoad(t, dir, "1.2.3", stream)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", f.log)
	t.Setenv("FAKE_BROWSER", f.browser)
	// The generator writes into its working directory: make that a scratch one.
	old, _ := os.Getwd()
	work := t.TempDir()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return f
}

func (f fakeGenerator) calls(t *testing.T) []string {
	t.Helper()
	b, _ := os.ReadFile(f.log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (f fakeGenerator) runs(t *testing.T) []string {
	var runs []string
	for _, c := range f.calls(t) {
		if strings.HasPrefix(c, "run ") && c != "run -h" {
			runs = append(runs, c)
		}
	}
	return runs
}

func (f fakeGenerator) opened() bool {
	_, err := os.Stat(f.browser)
	return err == nil
}

func strayReports(t *testing.T) []string {
	t.Helper()
	m, _ := filepath.Glob("vegaload-report-*.html")
	return m
}

func loadSpec() *scenario.LoadSpec {
	return &scenario.LoadSpec{Tool: "vegaload", Scenario: "x.vl.js"}
}

// A vegaload without -stream-file, in a streamed run: it is started once, with
// -no-open and a temporary report, so it neither opens a browser nor leaves a
// report behind; the series arrives at the end and one note says why there are
// no live points.
func TestOldVegaLoadStreamedRunStaysQuiet(t *testing.T) {
	g := installFakeVegaLoad(t, false)
	var got collected
	out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{RunID: "r1", Progress: got.add})
	if err != nil {
		t.Fatal(err)
	}
	runs := g.runs(t)
	if len(runs) != 1 {
		t.Fatalf("want one run, got %q", runs)
	}
	if strings.Contains(runs[0], "-stream-file") {
		t.Fatalf("an old vegaload must not be asked for -stream-file: %s", runs[0])
	}
	for _, want := range []string{"-no-open", "-report "} {
		if !strings.Contains(runs[0], want) {
			t.Fatalf("missing %q: %s", want, runs[0])
		}
	}
	if g.opened() {
		t.Fatal("vegaload would have opened a browser")
	}
	if r := strayReports(t); len(r) != 0 {
		t.Fatalf("report left in the working directory: %v", r)
	}
	if got.n() != 0 {
		t.Fatalf("an old vegaload has no live points, got %d", got.n())
	}
	if !strings.Contains(out.Warn, "live load points are not available") {
		t.Fatalf("note: %q", out.Warn)
	}
	if strings.Count(out.Warn, "no -stream-file") != 1 {
		t.Fatalf("the note must be said once: %q", out.Warn)
	}
	if len(out.Series) != 2 || out.Series[1].RPS != 12 {
		t.Fatalf("series: %+v", out.Series)
	}
}

// A run that is not streamed gets the same quiet flags, and no note: nobody
// asked for live points.
func TestVegaLoadPlainRunStaysQuiet(t *testing.T) {
	for _, stream := range []bool{false, true} {
		g := installFakeVegaLoad(t, stream)
		out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		runs := g.runs(t)
		if len(runs) != 1 || !strings.Contains(runs[0], "-no-open") || !strings.Contains(runs[0], "-report ") {
			t.Fatalf("stream=%v: %q", stream, runs)
		}
		if strings.Contains(runs[0], "-stream-file") {
			t.Fatalf("stream=%v: no stream asked for, got %s", stream, runs[0])
		}
		if g.opened() || len(strayReports(t)) != 0 {
			t.Fatalf("stream=%v: noisy run", stream)
		}
		if out.Warn != "" || len(out.Series) != 2 {
			t.Fatalf("stream=%v: %+v", stream, out)
		}
	}
}

// A vegaload with -stream-file is started once, with all three flags, and its
// points are read as they are written.
func TestNewVegaLoadStreamedRunGivesLivePoints(t *testing.T) {
	g := installFakeVegaLoad(t, true)
	var got collected
	out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{Progress: got.add})
	if err != nil {
		t.Fatal(err)
	}
	runs := g.runs(t)
	if len(runs) != 1 {
		t.Fatalf("want one run, got %q", runs)
	}
	for _, want := range []string{"-stream-file ", "-no-open", "-report "} {
		if !strings.Contains(runs[0], want) {
			t.Fatalf("missing %q: %s", want, runs[0])
		}
	}
	if g.opened() || len(strayReports(t)) != 0 {
		t.Fatal("noisy run")
	}
	if got.n() != 1 || got.pts[0].RPS != 7 {
		t.Fatalf("live points: %+v", got.pts)
	}
	if out.Warn != "" {
		t.Fatalf("no note expected: %q", out.Warn)
	}
}

// The help is read once per binary, not once per run.
func TestCapabilitiesAreProbedOncePerBinary(t *testing.T) {
	g := installFakeVegaLoad(t, true)
	for i := 0; i < 3; i++ {
		if _, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	probes := 0
	for _, c := range g.calls(t) {
		if c == "run -h" {
			probes++
		}
	}
	if probes != 1 {
		t.Fatalf("probed %d times", probes)
	}
}

// The command line is a function of what the generator lists, so it can be
// checked on every platform.
func TestVegaLoadArgs(t *testing.T) {
	has := func(args []string, flag string) bool {
		for _, a := range args {
			if a == flag {
				return true
			}
		}
		return false
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "result.json")
	full := parseFlags("Usage:\n  -no-open\n    \tx\n  -report string\n  -stream-file string\n")
	oldOne := parseFlags("Usage:\n  -no-open\n    \tx\n  -report string\n")
	unknown := parseFlags("")
	ancient := parseFlags("Usage:\n  -out string\n")

	cases := []struct {
		name     string
		caps     genCaps
		streamed bool
		flags    []string
		absent   []string
		stream   bool
		note     string
	}{
		{"new, streamed", full, true, []string{"-no-open", "-report", "-stream-file"}, nil, true, ""},
		{"new, plain", full, false, []string{"-no-open", "-report"}, []string{"-stream-file"}, false, ""},
		{"old, streamed", oldOne, true, []string{"-no-open", "-report"}, []string{"-stream-file"}, false, "no -stream-file"},
		{"unknown help, streamed", unknown, true, []string{"-no-open", "-report"}, []string{"-stream-file"}, false, "did not list its flags"},
		{"ancient, plain", ancient, false, nil, []string{"-no-open", "-report", "-stream-file"}, false, "no -no-open"},
		{"ancient, streamed", ancient, true, nil, []string{"-no-open", "-report", "-stream-file"}, false, "live load points are not available"},
	}
	for _, c := range cases {
		args, sf, note := vegaLoadArgs(c.caps, "vegaload 1.2.3 (~/bin/vegaload)", dir, out, c.streamed)
		for _, f := range c.flags {
			if !has(args, f) {
				t.Errorf("%s: missing %s in %v", c.name, f, args)
			}
		}
		for _, f := range c.absent {
			if has(args, f) {
				t.Errorf("%s: unexpected %s in %v", c.name, f, args)
			}
		}
		if (sf != "") != c.stream {
			t.Errorf("%s: stream file %q", c.name, sf)
		}
		if !strings.Contains(note, c.note) || (c.note == "" && note != "") {
			t.Errorf("%s: note %q, want %q", c.name, note, c.note)
		}
	}
}

func TestParseFlags(t *testing.T) {
	goStyle := "Usage: vegaload run [flags]\n  -no-open\n    \tdon't auto-open\n  -report string\n    \t-stream-file looks like a flag here but is usage text\n"
	c := parseFlags(goStyle)
	if !c.has("no-open") || !c.has("report") || c.has("stream-file") {
		t.Fatalf("go style: %+v", c.flags)
	}
	cobra := "Flags:\n  -h, --help            help\n      --stream-file string   file\n"
	c = parseFlags(cobra)
	if !c.has("help") || !c.has("stream-file") {
		t.Fatalf("cobra style: %+v", c.flags)
	}
	if parseFlags("command not found").known {
		t.Fatal("no flags must be not known")
	}
}
