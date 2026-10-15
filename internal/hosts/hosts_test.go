package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	return root.Servers
}

func TestEntryHasNoEnvWithoutOne(t *testing.T) {
	root := t.TempDir()
	if err := WriteCursor(root, "/bin/ll", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, ".cursor", "mcp.json"))
	if strings.Contains(string(b), "env") {
		t.Fatalf("no env was asked for:\n%s", b)
	}
}

func TestEntryCarriesTheEnvAndRerunKeepsIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".cursor", "mcp.json")
	// Another tool's server, with its own env, is already registered.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	other := `{"mcpServers":{"vegaload":{"command":"/bin/vl","args":["mcp","serve"],"env":{"VEGALOAD_CONFIG_DIR":"/shared"}}}}`
	if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCursor(root, "/bin/ll", map[string]string{"LITMUS_LITE_CONFIG_DIR": "/shared"}); err != nil {
		t.Fatal(err)
	}
	s := readServers(t, path)
	if env, _ := s["litmus-lite"]["env"].(map[string]any); env["LITMUS_LITE_CONFIG_DIR"] != "/shared" {
		t.Fatalf("litmus-lite env = %v", s["litmus-lite"]["env"])
	}
	if env, _ := s["vegaload"]["env"].(map[string]any); env["VEGALOAD_CONFIG_DIR"] != "/shared" {
		t.Fatalf("the other server was disturbed: %v", s["vegaload"])
	}
	// A later init without the option must not undo it, and may move the binary.
	if err := WriteCursor(root, "/new/ll", nil); err != nil {
		t.Fatal(err)
	}
	s = readServers(t, path)
	if s["litmus-lite"]["command"] != "/new/ll" {
		t.Fatalf("command = %v", s["litmus-lite"]["command"])
	}
	if env, _ := s["litmus-lite"]["env"].(map[string]any); env["LITMUS_LITE_CONFIG_DIR"] != "/shared" {
		t.Fatalf("env lost on re-run: %v", s["litmus-lite"])
	}
}

func TestRuleTeachesTheLoadBlock(t *testing.T) {
	for _, want := range []string{"`scenario`", "`args`", "constant-arrival-rate", "proxy listen address"} {
		if !strings.Contains(RuleBody, want) {
			t.Errorf("rule does not mention %s", want)
		}
	}
}
