package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/live"
)

// fakeBridge stands in for rt-bridge: it answers the health check and records
// what is pushed to a project.
type fakeBridge struct {
	srv *httptest.Server

	mu       sync.Mutex
	pushes   int
	auth     []string
	paths    []string
	queries  []string
	received strings.Builder
}

func newFakeBridge(t *testing.T) *fakeBridge {
	t.Helper()
	b := &fakeBridge{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/health":
			_, _ = w.Write([]byte(`{"status":"ok","projects":true}`))
		case strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.HasSuffix(r.URL.Path, "/ingest"):
			body, _ := io.ReadAll(r.Body)
			b.mu.Lock()
			b.pushes++
			b.auth = append(b.auth, r.Header.Get("Authorization"))
			b.paths = append(b.paths, r.URL.Path)
			b.queries = append(b.queries, r.URL.RawQuery)
			b.received.Write(body)
			b.mu.Unlock()
			_, _ = w.Write([]byte(`{"accepted":1,"duplicates":0,"ignored":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBridge) snapshot() (pushes int, auth, paths []string, body string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pushes, append([]string(nil), b.auth...), append([]string(nil), b.paths...), b.received.String()
}

// liveEnv turns the live view on for one test (TestMain turns it off for the
// package), points it at base, isolates its files, and captures the browser.
func liveEnv(t *testing.T, base string) (opened *[]string, home string) {
	t.Helper()
	clearStreamEnv(t)
	t.Setenv("LITMUS_LITE_CONFIG_DIR", t.TempDir())
	t.Setenv("LITMUS_LITE_LIVE", "")
	t.Setenv("LITMUS_LITE_OPEN", "")
	t.Setenv("LITMUS_LITE_LIVE_URL", base)
	t.Setenv("CI", "")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var urls []string
	prev := live.Opener
	live.Opener = func(u string) error { urls = append(urls, u); return nil }
	t.Cleanup(func() { live.Opener = prev })
	return &urls, home
}

func runLive(t *testing.T, extra ...string) (code int, report string) {
	t.Helper()
	dir := t.TempDir()
	scen := writeStreamScenario(t, dir)
	report = filepath.Join(dir, "report.json")
	args := append([]string{"litmus-lite", "run", "-out", report}, extra...)
	return run(append(args, scen)), report
}

func TestRunShowsTheLiveViewByDefault(t *testing.T) {
	bridge := newFakeBridge(t)
	opened, home := liveEnv(t, bridge.srv.URL)

	code, report := runLive(t)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	id, err := live.Load()
	if err != nil || id.Created {
		t.Fatalf("the run should have made the identity: %+v %v", id, err)
	}

	// The browser opens on the project page.
	if len(*opened) != 1 || (*opened)[0] != bridge.srv.URL+"/p/"+id.ProjectID {
		t.Fatalf("opened %v", *opened)
	}

	// The events went to that project, with its own token and no other.
	pushes, auth, paths, body := bridge.snapshot()
	if pushes == 0 {
		t.Fatal("nothing was pushed")
	}
	for i := range auth {
		if auth[i] != "Bearer "+id.PushToken {
			t.Fatalf("push %d authorised with %q", i, auth[i])
		}
		if paths[i] != "/v1/projects/"+id.ProjectID+"/ingest" {
			t.Fatalf("push %d to %s", i, paths[i])
		}
	}
	for _, want := range []string{`"type":"run.started"`, `"type":"run.finished"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("no %s in what was pushed", want)
		}
	}

	// The token is nowhere it should not be.
	rep, _ := os.ReadFile(report)
	audit, err := os.ReadFile(filepath.Join(home, ".litmus-lite", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), "live:127.0.0.1") {
		t.Fatalf("audit does not name the live view: %s", audit)
	}
	for name, content := range map[string]string{"report": string(rep), "audit": string(audit), "events": body} {
		if strings.Contains(content, id.PushToken) {
			t.Fatalf("the push token is in the %s", name)
		}
	}
	if strings.Contains(string(audit), id.ProjectID) {
		t.Fatalf("the audit log holds the project id, which is the page's secret: %s", audit)
	}
}

func TestEveryRunOpensTheBrowserOnTheSameProject(t *testing.T) {
	bridge := newFakeBridge(t)
	opened, _ := liveEnv(t, bridge.srv.URL)
	for i := 0; i < 2; i++ {
		if code, _ := runLive(t); code != 0 {
			t.Fatalf("run %d: exit %d", i, code)
		}
	}
	if len(*opened) != 2 || (*opened)[0] != (*opened)[1] {
		t.Fatalf("opened %v: want the same page twice", *opened)
	}
}

func TestLiveViewCanBeTurnedOff(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  map[string]string
	}{
		"flag":               {args: []string{"-no-live"}},
		"env":                {env: map[string]string{"LITMUS_LITE_LIVE": "off"}},
		"ci":                 {env: map[string]string{"CI": "true"}},
		"stream-file chosen": {args: []string{"-stream-file", "auto"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			bridge := newFakeBridge(t)
			opened, _ := liveEnv(t, bridge.srv.URL)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			wd, _ := os.Getwd()
			t.Cleanup(func() { _ = os.Chdir(wd) })
			_ = os.Chdir(t.TempDir()) // -stream-file auto writes under the cwd
			if code, _ := runLive(t, c.args...); code != 0 {
				t.Fatalf("exit %d", code)
			}
			if pushes, _, _, _ := bridge.snapshot(); pushes != 0 {
				t.Fatalf("%d pushes with the live view off", pushes)
			}
			if len(*opened) != 0 {
				t.Fatalf("opened %v", *opened)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("LITMUS_LITE_CONFIG_DIR"), "live.json")); err == nil {
				t.Fatal("an identity was made although the live view is off")
			}
		})
	}
}

func TestLiveFlagAddsTheViewToAnotherDestinationAndBeatsCI(t *testing.T) {
	bridge := newFakeBridge(t)
	opened, _ := liveEnv(t, bridge.srv.URL)
	t.Setenv("CI", "true")
	file := filepath.Join(t.TempDir(), "run.ndjson")
	if code, _ := runLive(t, "-live", "-stream-file", file); code != 0 {
		t.Fatal("exit")
	}
	if pushes, _, _, _ := bridge.snapshot(); pushes == 0 {
		t.Fatal("-live did not push")
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "run.finished") {
		t.Fatalf("the file destination was dropped: %v", err)
	}
	// CI still never opens a browser: -live asks for the stream, not a window.
	if len(*opened) != 0 {
		t.Fatalf("opened %v in CI", *opened)
	}
}

func TestNoOpenStreamsWithoutABrowser(t *testing.T) {
	bridge := newFakeBridge(t)
	opened, _ := liveEnv(t, bridge.srv.URL)
	if code, _ := runLive(t, "-no-open"); code != 0 {
		t.Fatal("exit")
	}
	if pushes, _, _, _ := bridge.snapshot(); pushes == 0 {
		t.Fatal("nothing pushed")
	}
	if len(*opened) != 0 {
		t.Fatalf("opened %v", *opened)
	}
}

func TestUnreachableBridgeNeverFailsTheRun(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()
	opened, _ := liveEnv(t, url)

	start := time.Now()
	code, report := runLive(t)
	if code != 0 {
		t.Fatalf("exit %d: a missing bridge must not fail the run", code)
	}
	if len(*opened) != 0 {
		t.Fatalf("opened %v for a bridge that is not there", *opened)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("no report: %v", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("took %s", d)
	}
}

func TestRemoteLiveViewNeedsHTTPS(t *testing.T) {
	opened, _ := liveEnv(t, "http://live.example.com")
	if code, _ := runLive(t); code != 0 {
		t.Fatal("a refused live view must not fail the run")
	}
	if len(*opened) != 0 {
		t.Fatalf("opened %v", *opened)
	}
}

func TestLiveCommand(t *testing.T) {
	bridge := newFakeBridge(t)
	opened, _ := liveEnv(t, bridge.srv.URL)

	capture := func(args ...string) string {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		code := run(append([]string{"litmus-lite", "live"}, args...))
		w.Close()
		os.Stdout = old
		out, _ := io.ReadAll(r)
		if code != 0 {
			t.Fatalf("live %v: exit %d", args, code)
		}
		return strings.TrimSpace(string(out))
	}

	first := capture()
	id, _ := live.Load()
	if first != bridge.srv.URL+"/p/"+id.ProjectID {
		t.Fatalf("url %q", first)
	}
	if again := capture(); again != first {
		t.Fatalf("not stable: %q %q", again, first)
	}
	if len(*opened) != 0 {
		t.Fatal("opened without -open")
	}
	capture("-open")
	if len(*opened) != 1 || (*opened)[0] != first {
		t.Fatalf("opened %v", *opened)
	}
	if after := capture("reset"); after == first {
		t.Fatal("reset kept the project")
	}
	if code := run([]string{"litmus-lite", "live", "bogus"}); code != 2 {
		t.Fatalf("bogus argument: exit %d", code)
	}
}
