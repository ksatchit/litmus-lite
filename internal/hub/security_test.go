package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hub.lock is a file of the project, so it can come from a repository the user
// has just cloned. Its "repo" must never be read by git as an option.
func TestHubLockCannotSmuggleAGitOption(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("LITMUS_LITE_HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "ran")
	for name, lock := range map[string]Lock{
		"repo is an option": {Repo: "--upload-pack=touch " + marker, Ref: "main"},
		"ref is an option":  {Repo: "/no/such/repo", Ref: "--upload-pack=touch " + marker},
		"newline in repo":   {Repo: "/no/such\nrepo", Ref: "main"},
		"path leaves clone": {Repo: "/no/such/repo", Ref: "main", Path: "../../etc"},
	} {
		_, err := fetchRemote(lock)
		if err == nil || !strings.Contains(err.Error(), "hub.lock") {
			t.Errorf("%s: got %v, want a hub.lock refusal before git runs", name, err)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("git ran the command from hub.lock")
	}
}

func TestRemoteCatalogFileMustStayInTheClone(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(secret, []byte("not a template"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := "items:\n  - id: x.y\n    title: X\n    kind: http.latency\n    file: ../outside.txt\n"
	if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), []byte(cat), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRemoteCatalog(root); err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("a catalog entry read a file outside the clone: %v", err)
	}
}

func TestImportNameIsAFileNameNotAPath(t *testing.T) {
	dir := t.TempDir()
	beside := filepath.Join(dir, "scenarios")
	for _, name := range []string{"../escape", "a/b", `a\b`, "..", "."} {
		if _, err := Import("http.latency", beside, name); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.chaos.yaml")); err == nil {
		t.Fatal("a file was written outside the directory")
	}
}

func TestImportNeverReplacesAnExistingScenario(t *testing.T) {
	dir := t.TempDir()
	p, err := Import("http.latency", dir, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("hand-made changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Import("http.latency", dir, "mine"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second import: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "hand-made changes\n" {
		t.Fatalf("the existing scenario was overwritten: %q", b)
	}
}
