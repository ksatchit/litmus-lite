package litmus

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/moveto"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestIgniteRoundTripNoChaosEngine(t *testing.T) {
	path := filepath.Join(repoRoot(t), "examples", "launchpad", "ignite.chaos.yaml")
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ex := moveto.FromScenario(sc)
	if ex.Name != "launchpad-http-latency" || len(ex.Faults) == 0 || len(ex.Probes) == 0 {
		t.Fatalf("IR incomplete: %+v", ex)
	}
	doc := FromIR(ex)
	y := string(YAML(doc))
	j := string(JSON(doc))
	for _, body := range []string{y, j} {
		if strings.Contains(body, "ChaosEngine") || strings.Contains(body, "ChaosExperiment") {
			t.Fatalf("legacy CRD string in document:\n%s", body)
		}
	}
	if doc.APIVersion != APIVersion || doc.Kind != Kind {
		t.Fatalf("want %s %s, got %s %s", APIVersion, Kind, doc.APIVersion, doc.Kind)
	}
	if doc.Metadata.Name != "launchpad-http-latency" {
		t.Fatal(doc.Metadata.Name)
	}
	if doc.Spec.Target.Kind != "http" || doc.Spec.Target.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("target: %+v", doc.Spec.Target)
	}
	if len(doc.Spec.Faults) != 1 || doc.Spec.Faults[0].Kind != "http.latency" || doc.Spec.Faults[0].Duration != "12s" {
		t.Fatalf("faults: %+v", doc.Spec.Faults)
	}
	if doc.Spec.Faults[0].Tunables["listen"] != "127.0.0.1:18080" {
		t.Fatalf("tunables: %+v", doc.Spec.Faults[0].Tunables)
	}
	modes := map[string]string{}
	for _, p := range doc.Spec.Probes {
		modes[p.Name] = p.Mode
		if p.Type != "http" {
			t.Fatalf("probe type %s: %s", p.Name, p.Type)
		}
	}
	if modes["healthz"] != "SOT" || modes["list-launches"] != "Continuous" {
		t.Fatalf("probe modes: %v", modes)
	}
	if len(doc.Spec.Hypotheses) != 2 || doc.Spec.Rollback != "always" {
		t.Fatalf("hypotheses/rollback: %+v %s", doc.Spec.Hypotheses, doc.Spec.Rollback)
	}
	if !strings.Contains(y, "tunables:") || !strings.Contains(y, "kind: Experiment") {
		t.Fatal(y)
	}
}

func TestCmdProbeAndOnChaos(t *testing.T) {
	sc, err := scenario.Parse([]byte(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: cmd-check }
faults:
  - name: a
    kind: http.latency
    duration: 1s
    params: { listen: "127.0.0.1:1", upstream: "127.0.0.1:2" }
probes:
  - name: ping
    type: cmd
    command: echo ok
    mode: on-chaos
    expect: { exit: 0 }
`))
	if err != nil {
		t.Fatal(err)
	}
	doc := FromIR(moveto.FromScenario(sc))
	if len(doc.Spec.Probes) != 1 {
		t.Fatal(doc.Spec.Probes)
	}
	p := doc.Spec.Probes[0]
	if p.Type != "cmd" || p.Mode != "OnChaos" || p.Command != "echo ok" {
		t.Fatalf("%+v", p)
	}
	if strings.Contains(string(YAML(doc)), "ChaosEngine") {
		t.Fatal("ChaosEngine")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found")
	return ""
}
