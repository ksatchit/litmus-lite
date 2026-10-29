package k8s

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func scenarioNamed(t *testing.T, name string) (*scenario.Scenario, []byte) {
	t.Helper()
	raw := []byte("apiVersion: litmus-lite.io/v1\nkind: Scenario\nmetadata:\n  name: " + name + "\nfaults:\n  - name: a\n    kind: http.latency\n    duration: 1s\n    params: { listen: \"127.0.0.1:1\", upstream: \"127.0.0.1:2\" }\nprobes:\n  - name: p\n    type: http\n    url: http://127.0.0.1:1/\n")
	s, err := scenario.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, raw
}

// docs parses every document of the output, as kubectl would.
func docs(t *testing.T, out string) []map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(out))
	var all []map[string]any
	for {
		var m map[string]any
		if err := dec.Decode(&m); err == io.EOF {
			return all
		} else if err != nil {
			t.Fatalf("the output is not valid YAML: %v\n%s", err, out)
		}
		all = append(all, m)
	}
}

func TestManifestsAreAConfigMapAndAJob(t *testing.T) {
	s, raw := scenarioNamed(t, "Demo_App")
	out, err := Manifests(s, JobOptions{Image: "registry.example/litmus-lite:1.2.3", Scenario: raw})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ChaosEngine") {
		t.Fatal("the export is plain Kubernetes, no CRD")
	}
	d := docs(t, out)
	if len(d) != 2 || d[0]["kind"] != "ConfigMap" || d[1]["kind"] != "Job" {
		t.Fatalf("want a ConfigMap and a Job, got %v", d)
	}
	// The Job mounts a ConfigMap that is in the same output, with the scenario
	// exactly as written: the Job used to name a ConfigMap nobody created.
	cm := d[0]
	if cm["metadata"].(map[string]any)["name"] != "demo-app-scenario" {
		t.Fatalf("configmap %v", cm["metadata"])
	}
	if got := cm["data"].(map[string]any)["scenario.chaos.yaml"]; got != string(raw) {
		t.Fatalf("the scenario in the ConfigMap changed:\n%q\n%q", got, raw)
	}
	job := out[strings.Index(out, "apiVersion: batch/v1"):]
	if !strings.Contains(job, "name: demo-app-scenario") || !strings.Contains(job, `image: "registry.example/litmus-lite:1.2.3"`) {
		t.Fatal(job)
	}
	if !strings.Contains(job, `args: ["run", "-output", "json", "/scenario.chaos.yaml"]`) {
		t.Fatalf("args:\n%s", job)
	}
}

// No image is published by the project, so none is assumed: the old output
// named one that does not exist, and the Job failed on its first run.
func TestManifestsNeedAnImage(t *testing.T) {
	s, raw := scenarioNamed(t, "a")
	for _, img := range []string{"", " ", "-evil", "a b", "img\nkind: Secret", `x"; rm`, "a:b:c\"d"} {
		if _, err := Manifests(s, JobOptions{Image: img, Scenario: raw}); err == nil {
			t.Errorf("image %q accepted", img)
		}
	}
	for _, img := range []string{"litmus-lite", "ghcr.io/org/litmus-lite:1.2.3", "r.example:5000/a/b@sha256:0123456789abcdef"} {
		if _, err := Manifests(s, JobOptions{Image: img, Scenario: raw}); err != nil {
			t.Errorf("image %q refused: %v", img, err)
		}
	}
	if _, err := Manifests(s, JobOptions{Image: "litmus-lite"}); err == nil {
		t.Error("an empty scenario was exported")
	}
}

func TestJobPassesYesAndTargetsOnlyWhenAsked(t *testing.T) {
	s, raw := scenarioNamed(t, "a")
	out, _ := Manifests(s, JobOptions{Image: "i", Scenario: raw})
	if strings.Contains(out, "-yes") || strings.Contains(out, "-allow-target") {
		t.Fatalf("the Job turns a guard off on its own:\n%s", out)
	}
	out, err := Manifests(s, JobOptions{Image: "i", Scenario: raw, Yes: true, AllowTargets: []string{"checkout.shop.svc", "10.0.0.5:8080"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `args: ["run", "-output", "json", "-allow-target", "checkout.shop.svc", "-allow-target", "10.0.0.5:8080", "-yes", "/scenario.chaos.yaml"]`
	if !strings.Contains(out, want) {
		t.Fatalf("want %s in\n%s", want, out)
	}
	for _, h := range []string{"", "a b", "x\"y", "-h", "a\nb", "h;rm"} {
		if _, err := Manifests(s, JobOptions{Image: "i", Scenario: raw, AllowTargets: []string{h}}); err == nil {
			t.Errorf("allow-target %q accepted", h)
		}
	}
}

func TestJobNameIsSafeInYAML(t *testing.T) {
	for in, want := range map[string]string{
		"Demo_App":                 "demo-app",
		"":                         "litmus-lite",
		"---":                      "litmus-lite",
		"a: b\nkind: Evil":         "a-b-kind-evil",
		`x"; rm -rf /`:             "x-rm-rf",
		strings.Repeat("abc-", 30): strings.Trim(strings.Repeat("abc-", 30)[:50], "-"),
	} {
		got := dnsLabel(in)
		if got != want {
			t.Errorf("dnsLabel(%q) = %q, want %q", in, got, want)
		}
		if len(got)+len("-scenario") > 63 {
			t.Errorf("%q: %q is too long for the ConfigMap name", in, got)
		}
	}
	raw := []byte("apiVersion: litmus-lite.io/v1\nkind: Scenario\nmetadata:\n  name: \"evil\\nkind: Secret\"\nfaults:\n  - name: a\n    kind: http.latency\n    duration: 1s\n    params: { listen: \"127.0.0.1:1\", upstream: \"127.0.0.1:2\" }\nprobes:\n  - name: p\n    type: http\n    url: http://127.0.0.1:1/\n")
	s, err := scenario.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Manifests(s, JobOptions{Image: "i", Scenario: raw})
	if err != nil {
		t.Fatal(err)
	}
	d := docs(t, out)
	if len(d) != 2 || d[0]["kind"] != "ConfigMap" || d[1]["kind"] != "Job" {
		t.Fatalf("a scenario name changed the manifest:\n%s", out)
	}
}

// A scenario file can hold anything YAML allows. It is data in the ConfigMap
// and never becomes structure.
func TestAHostileScenarioStaysDataInTheConfigMap(t *testing.T) {
	s, _ := scenarioNamed(t, "a")
	nasty := []byte("# --- \n---\nkind: Secret\n  leading: space\n\ttab\n|\n>\n'\"\n")
	out, err := Manifests(s, JobOptions{Image: "i", Scenario: nasty})
	if err != nil {
		t.Fatal(err)
	}
	d := docs(t, out)
	if len(d) != 2 || d[0]["kind"] != "ConfigMap" || d[1]["kind"] != "Job" {
		t.Fatalf("documents: %v", d)
	}
	if got := d[0]["data"].(map[string]any)["scenario.chaos.yaml"]; !bytes.Equal([]byte(got.(string)), nasty) {
		t.Fatalf("the scenario text changed: %q", got)
	}
}
