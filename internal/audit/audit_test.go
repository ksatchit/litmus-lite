package audit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The log names the scenario files a user ran, so it is for that user only.
func TestAuditLogIsPrivateAndAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	Append(Line{Command: "run", File: "a.chaos.yaml"})
	Append(Line{Command: "run", File: "b.chaos.yaml"})
	path := filepath.Join(home, ".litmus-lite", "audit.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Fatalf("%d lines: %s", n, b)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, want 0600", st.Mode().Perm())
		}
	}
}
