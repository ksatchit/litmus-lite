package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenNoLockUsesEmbeddedOnly(t *testing.T) {
	t.Setenv("LITMUS_LITE_HOME", t.TempDir())
	c := Open(t.TempDir())
	items, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(catalog) {
		t.Fatalf("got %d want %d", len(items), len(catalog))
	}
	if _, err := c.Get("http.latency"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteGitPinEmbeddedWins(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("LITMUS_LITE_HOME", t.TempDir())
	repo := initChartRepo(t)
	proj := t.TempDir()
	writeLock(t, proj, repo, "main", "")

	c := Open(proj)
	items, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	var sawRemote, sawCollision bool
	var latency Item
	for _, it := range items {
		if it.ID == "git.example" {
			sawRemote = true
			if it.Source != "git" {
				t.Fatalf("source %q", it.Source)
			}
		}
		if it.ID == "http.latency" {
			latency = it
			if it.Source != "embedded" {
				t.Fatalf("collision must keep embedded: %+v", it)
			}
		}
		if it.ID == "http.latency" && it.Source == "git" {
			sawCollision = true
		}
	}
	if !sawRemote {
		t.Fatal("missing git.example")
	}
	if sawCollision || latency.Title != "HTTP latency" {
		t.Fatalf("embedded lost the collision: %+v", latency)
	}

	got, err := c.Get("git.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Template, "kind: Scenario") {
		t.Fatal(got.Template)
	}
	dir := t.TempDir()
	p, err := c.Import("git.example", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "git-example") {
		t.Fatal(string(b))
	}

	// Cached clone: no git required on the next list.
	t.Setenv("PATH", t.TempDir())
	c2 := Open(proj)
	items2, err := c2.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items2 {
		if it.ID == "git.example" {
			found = true
		}
	}
	if !found {
		t.Fatal("cached remote missing")
	}
}

func TestGetEmbeddedDoesNotNeedGit(t *testing.T) {
	t.Setenv("LITMUS_LITE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	proj := t.TempDir()
	writeLock(t, proj, "/no/such/repo.git", "main", "")
	c := Open(proj)
	if _, err := c.Get("http.latency"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("git.example"); err == nil {
		t.Fatal("remote get should fail without git/cache")
	}
}

func writeLock(t *testing.T, proj, repo, ref, path string) {
	t.Helper()
	dir := filepath.Join(proj, ".litmus-lite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "repo: " + repo + "\nref: " + ref + "\n"
	if path != "" {
		body += "path: " + path + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.lock"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initChartRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	catalog := `items:
  - id: git.example
    title: Git example
    kind: http.latency
    os: [linux, darwin, windows]
    summary: Remote chart used in tests
    file: git-example.chaos.yaml
  - id: http.latency
    title: SHOULD-NOT-WIN
    kind: http.latency
    summary: collision
    template: |
      apiVersion: litmus-lite.io/v1
      kind: Scenario
      metadata: { name: collision }
`
	scenario := `apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: git-example
target:
  kind: http
  baseUrl: http://127.0.0.1:8080
steadyState:
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
faults:
  - name: slow
    kind: http.latency
    duration: 1s
    params:
      listen: "127.0.0.1:18080"
      upstream: "127.0.0.1:8080"
      delay: 10ms
probes:
  - name: list
    type: http
    url: http://127.0.0.1:18080/
rollback: always
`
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "git-example.chaos.yaml"), []byte(scenario), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "charts")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}
