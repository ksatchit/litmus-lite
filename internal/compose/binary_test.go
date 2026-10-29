package compose

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"vegaload 0.9.0\n":                          "0.9.0",
		"vegaload 0.0.0-dev":                        "0.0.0-dev",
		"k6 v0.49.0 (go1.21.4, darwin/arm64)\nmore": "0.49.0",
		"": "",
		"unknown command \"version\"\nrelease 1.2.3": "", // only the first line counts
	} {
		if got := parseVersion(in); got != want {
			t.Errorf("parseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.9.0", "0.10.0", -1, true}, // numeric, not text, order
		{"0.10.0", "0.9.0", 1, true},
		{"1.2.3", "1.2.3", 0, true},
		{"0.0.0-dev", "0.9.0", 0, false}, // a development build cannot be ordered
		{"0.9.0", "", 0, false},
		{"1.0.0-rc1", "1.0.0", 0, false},
	}
	for _, c := range cases {
		got, ok := compareVersions(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

// besideDir makes a folder act as the one litmus-lite runs from.
func besideDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := executableDir
	executableDir = func() string { return dir }
	t.Cleanup(func() { executableDir = old })
	return dir
}

// The copy that PATH gives is the one that runs, and the run records which it
// was: tool, version, where from, and how it was found.
func TestInspectAndRunRecordTheGenerator(t *testing.T) {
	installFakeVegaLoad(t, true)
	besideDir(t) // nothing next to litmus-lite
	st := Inspect(context.Background(), "", loadSpec(), "")
	if !st.Found || st.Version != "1.2.3" || st.Via != "PATH" || !st.Caps.has("stream-file") || st.Warn != "" {
		t.Fatalf("%+v", st)
	}
	if !strings.Contains(st.Message(), "vegaload 1.2.3 at ") || !strings.Contains(st.Message(), "(PATH)") || st.Degraded() {
		t.Fatalf("message %q", st.Message())
	}
	out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g := out.Generator
	if g == nil || g.Tool != "vegaload" || g.Version != "1.2.3" || g.Via != "PATH" || filepath.Base(g.Path) != "vegaload" {
		t.Fatalf("generator %+v", g)
	}
}

// A missing generator is reported as missing, and nothing is recorded.
func TestInspectMissingGenerator(t *testing.T) {
	besideDir(t)
	t.Setenv("PATH", t.TempDir())
	st := Inspect(context.Background(), "", loadSpec(), "")
	if st.Found || st.Generator() != nil || st.Bin != "vegaload" {
		t.Fatalf("%+v", st)
	}
}

// A copy next to litmus-lite is found when PATH has none, and says so.
func TestInspectFindsTheCopyNextToLitmusLite(t *testing.T) {
	dir := besideDir(t)
	writeFakeVegaLoad(t, dir, "2.0.0", true)
	t.Setenv("FAKE_LOG", filepath.Join(t.TempDir(), "log"))
	t.Setenv("PATH", t.TempDir())
	st := Inspect(context.Background(), "", loadSpec(), "")
	if !st.Found || st.Via != "next to litmus-lite" || st.Version != "2.0.0" {
		t.Fatalf("%+v", st)
	}
}

// The scenario of the stale copy: PATH holds an old vegaload, a newer one sits
// next to litmus-lite. PATH still wins (the lookup order is unchanged), the run
// says so once, and `doctor` shows it as a warning.
func TestStaleCopyOnPathIsReportedButStillUsed(t *testing.T) {
	pathDir := t.TempDir()
	writeFakeVegaLoad(t, pathDir, "0.9.0", false)
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", filepath.Join(t.TempDir(), "log"))
	t.Setenv("FAKE_BROWSER", filepath.Join(t.TempDir(), "browser"))
	writeFakeVegaLoad(t, besideDir(t), "0.10.0", true)

	st := Inspect(context.Background(), "", loadSpec(), "")
	if st.Version != "0.9.0" || st.Via != "PATH" {
		t.Fatalf("PATH must still win: %+v", st)
	}
	if !strings.Contains(st.Warn, "0.9.0") || !strings.Contains(st.Warn, "version 0.10.0") || !strings.Contains(st.Warn, "newer") {
		t.Fatalf("warn %q", st.Warn)
	}
	if !st.Degraded() || !strings.Contains(st.Message(), "no -stream-file") {
		t.Fatalf("message %q", st.Message())
	}

	old, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Generator.Version != "0.9.0" {
		t.Fatalf("the PATH copy ran: %+v", out.Generator)
	}
	if strings.Count(out.Warn, "next to litmus-lite") != 1 {
		t.Fatalf("the stale copy must be said once: %q", out.Warn)
	}
}

// No warning when there is nothing better: the other copy is older, the same,
// a development build that cannot be compared, or the very same file.
func TestNoStaleWarningWhenThereIsNothingBetter(t *testing.T) {
	cases := []struct {
		name                 string
		pathVer, besideVer   string
		pathStream, besideSt bool
		link                 bool
	}{
		{"beside is older", "1.0.0", "0.9.0", true, true, false},
		{"same version", "1.0.0", "1.0.0", true, true, false},
		{"beside is a dev build, same abilities", "1.0.0", "0.0.0-dev", true, true, false},
		{"beside is the same file", "1.0.0", "1.0.0", true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pathDir := t.TempDir()
			target := writeFakeVegaLoad(t, pathDir, c.pathVer, c.pathStream)
			t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			beside := besideDir(t)
			if c.link {
				if err := os.Symlink(target, filepath.Join(beside, "vegaload")); err != nil {
					t.Skip("no symlinks here")
				}
			} else {
				writeFakeVegaLoad(t, beside, c.besideVer, c.besideSt)
			}
			st := Inspect(context.Background(), "", loadSpec(), "")
			if st.Version != c.pathVer {
				t.Fatalf("the check did not see the PATH copy's version: %+v", st)
			}
			if st.Warn != "" {
				t.Fatalf("unexpected warning: %q", st.Warn)
			}
		})
	}
}

// A dev build cannot be compared by number, but if it can stream live points
// and the PATH copy cannot, that is a reason to say so.
func TestStaleWarningWhenTheOtherCopyCanStream(t *testing.T) {
	pathDir := t.TempDir()
	writeFakeVegaLoad(t, pathDir, "0.9.0", false)
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeFakeVegaLoad(t, besideDir(t), "0.0.0-dev", true)
	st := Inspect(context.Background(), "", loadSpec(), "")
	if !strings.Contains(st.Warn, "able to stream live load points") {
		t.Fatalf("warn %q", st.Warn)
	}
}

// The note for an old generator names the copy, so a user with two knows which.
func TestOldGeneratorNoteNamesTheCopy(t *testing.T) {
	installFakeVegaLoad(t, false)
	besideDir(t)
	var got collected
	out, err := RunWith(context.Background(), "", "", loadSpec(), RunOptions{Progress: got.add})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Warn, "vegaload 1.2.3 (") || !strings.Contains(out.Warn, "has no -stream-file") {
		t.Fatalf("note %q", out.Warn)
	}
}

func TestTildeShortensHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("home layout differs")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home")
	}
	if got := tilde(filepath.Join(home, "bin", "vegaload")); got != "~/bin/vegaload" {
		t.Fatalf("%q", got)
	}
	if got := tilde("/opt/x/vegaload"); got != "/opt/x/vegaload" {
		t.Fatalf("%q", got)
	}
}
