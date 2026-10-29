package mcp

import (
	"strings"
	"testing"
)

// The flags a tool call can carry are the person's consent. Without the
// person enabling them for the server, none is accepted, whatever its value.
func TestSafetyFlagsAreRefusedUnlessAPersonEnabledThem(t *testing.T) {
	t.Setenv(allowEnv, "")
	for name, args := range map[string]map[string]any{
		"yes":                  {"yes": true},
		"yes as text":          {"yes": "true"},
		"allow-pid":            {"allow-pid": "4242"},
		"allow-target":         {"allow-target": "example.com"},
		"allow-target as list": {"allow-target": "a.example.com,b.example.com"},
	} {
		args["file"] = "a.chaos.yaml"
		for _, tool := range []string{"run_test", "validate_scenario"} {
			_, err := runTool(tool, args)
			if err == nil {
				t.Errorf("%s %s: was not refused", tool, name)
				continue
			}
			// The refusal says what was refused and how a person lifts it.
			for _, want := range []string{allowEnv, "init -env"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s %s: the refusal does not mention %q: %v", tool, name, want, err)
				}
			}
		}
	}
}

func TestSafetyFlagsThatAreOffOrEmptyAreNotRefused(t *testing.T) {
	t.Setenv(allowEnv, "")
	for _, args := range []map[string]any{
		nil,
		{"yes": false},
		{"yes": "false"},
		{"allow-pid": ""},
		{"allow-target": nil},
		{"strict": true},
	} {
		if err := checkSafetyArgs(args); err != nil {
			t.Errorf("%v refused: %v", args, err)
		}
	}
}

func TestAPersonCanEnableEachFlagAndOnlyThatFlag(t *testing.T) {
	t.Setenv(allowEnv, "allow-pid, YES")
	if err := checkSafetyArgs(map[string]any{"allow-pid": "42", "yes": true}); err != nil {
		t.Fatalf("enabled flags refused: %v", err)
	}
	err := checkSafetyArgs(map[string]any{"allow-pid": "42", "allow-target": "example.com"})
	if err == nil || !strings.Contains(err.Error(), "allow-target") {
		t.Fatalf("a flag nobody enabled was accepted: %v", err)
	}
	// Nothing in the value turns on more than it names.
	t.Setenv(allowEnv, "allow-pid")
	if err := checkSafetyArgs(map[string]any{"yes": true}); err == nil {
		t.Fatal("enabling allow-pid enabled yes")
	}
	// Unknown names, and the empty entry from a trailing comma, enable nothing.
	t.Setenv(allowEnv, "all,*,,true,-yes")
	if err := checkSafetyArgs(map[string]any{"yes": true}); err == nil {
		t.Fatal("a wildcard-looking value enabled yes")
	}
}

// The refusal comes before the CLI is run: a refused call does nothing.
func TestARefusedCallRunsNothing(t *testing.T) {
	t.Setenv(allowEnv, "")
	t.Setenv("LITMUS_LITE_BIN", "/nonexistent/litmus-lite")
	_, err := runTool("run_test", map[string]any{"file": "a.chaos.yaml", "yes": true})
	if err == nil || strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("the CLI was reached: %v", err)
	}
}

func TestEveryAdvertisedSafetyFlagIsGated(t *testing.T) {
	for _, name := range safetyFlagNames {
		found := false
		for _, tl := range allTools() {
			if p, ok := tl.Props[name].(map[string]any); ok {
				found = true
				if d, _ := p["description"].(string); !strings.Contains(d, allowEnv) {
					t.Errorf("%s.%s: the description does not tell an agent it is gated", tl.Name, name)
				}
			}
		}
		if !found {
			t.Errorf("safety flag %s is not advertised by any tool", name)
		}
	}
	// A tool that forwards one of the flags must list it in safetyFlagNames,
	// or the gate does not know it exists.
	for _, tl := range allTools() {
		for name := range tl.Props {
			if strings.HasPrefix(name, "allow-") || name == "yes" {
				ok := false
				for _, g := range safetyFlagNames {
					ok = ok || g == name
				}
				if !ok {
					t.Errorf("%s.%s looks like a safety flag but is not in safetyFlagNames", tl.Name, name)
				}
			}
		}
	}
}
