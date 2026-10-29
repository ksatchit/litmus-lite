package stream

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/litmuschaos/litmus-lite/internal/report"
)

const fixtureDir = "../../docs/stream"

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, "schema", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("urn:litmus-lite:stream:v1", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("urn:litmus-lite:stream:v1")
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}
	return sch
}

func fixtures(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixtureDir, "fixtures", "*.ndjson"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found (%v)", err)
	}
	return files
}

// Every fixture line validates against the published schema, and every
// fixture passes the structural checks the tools apply to recordings.
func TestFixturesMatchSchema(t *testing.T) {
	sch := compileSchema(t)
	for _, path := range fixtures(t) {
		rec, err := ReadFile(path)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for i, raw := range rec.Raw {
			v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Errorf("%s event %d: %v", filepath.Base(path), i+1, err)
				continue
			}
			if err := sch.Validate(v); err != nil {
				t.Errorf("%s event %d (%s): %v", filepath.Base(path), i+1, rec.Events[i].Type, err)
			}
		}
	}
}

// An event of a type this version does not know about still validates, which
// is what lets a newer litmus-lite talk to an older consumer.
func TestSchemaAllowsUnknownTypesAndFields(t *testing.T) {
	sch := compileSchema(t)
	for _, line := range []string{
		`{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"something.new","data":{"x":1}}`,
		`{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":2,"ts":"2026-10-14T08:00:00Z","offsetS":0,"future":true,"type":"phase.changed","data":{"phase":"inject","extra":1}}`,
	} {
		v, _ := jsonschema.UnmarshalJSON(strings.NewReader(line))
		if err := sch.Validate(v); err != nil {
			t.Errorf("%s: %v", line, err)
		}
	}
}

func TestSchemaRejectsMalformedEvents(t *testing.T) {
	sch := compileSchema(t)
	for name, line := range map[string]string{
		"wrong schema":      `{"schema":"x","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"phase.changed","data":{"phase":"inject"}}`,
		"seq zero":          `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":0,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"phase.changed","data":{"phase":"inject"}}`,
		"bad phase":         `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"phase.changed","data":{"phase":"nope"}}`,
		"point missing p95": `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"point","data":{"offsetS":0,"phase":"steady","errorRate":0,"availability":1,"rps":1,"intensity":0}}`,
		"error rate > 1":    `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"point","data":{"offsetS":0,"phase":"steady","p95Ms":1,"errorRate":4,"availability":1,"rps":1,"intensity":0}}`,
		"load point no rps": `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"load.point","data":{"offsetS":1,"errorRate":0}}`,
		"load error rate>1": `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"load.point","data":{"offsetS":1,"rps":1,"errorRate":2}}`,
		"load started none": `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"load.started","data":{"tool":"vegaload"}}`,
		"failed no reason":  `{"schema":"litmus-lite.io/stream/v1","runId":"r","seq":1,"ts":"2026-10-14T08:00:00Z","offsetS":0,"type":"run.failed","data":{}}`,
	} {
		v, _ := jsonschema.UnmarshalJSON(strings.NewReader(line))
		if err := sch.Validate(v); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
}

// Each recorded report converts to exactly the committed fixture. The version
// field is the one thing that depends on the build, so it is ignored.
func TestFixturesAreWhatFromReportWrites(t *testing.T) {
	reports, _ := filepath.Glob(filepath.Join(fixtureDir, "fixtures", "*.report.json"))
	if len(reports) == 0 {
		t.Fatal("no recorded reports found")
	}
	for _, rp := range reports {
		name := strings.TrimSuffix(filepath.Base(rp), ".report.json")
		res, err := report.LoadJSON(rp)
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		if err := Write(&got, FromReport(res, FromReportOptions{})); err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(fixtureDir, "fixtures", name+".ndjson"))
		if err != nil {
			t.Fatal(err)
		}
		if a, b := withoutVersion(t, got.Bytes()), withoutVersion(t, want); a != b {
			t.Errorf("%s.ndjson is not what from-report writes for %s.report.json; re-run docs/stream/fixtures/generate.sh or regenerate with `litmus-lite stream from-report`", name, name)
		}
	}
}

func withoutVersion(t *testing.T, ndjson []byte) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(ndjson)), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		if ev["type"] == TypeRunStarted {
			delete(ev["data"].(map[string]any), "version")
		}
		b, _ := json.Marshal(ev)
		out = append(out, string(b))
	}
	return strings.Join(out, "\n")
}

// A committed fixture never carries a credential from a fault's parameters.
func TestFixturesHaveNoCredentials(t *testing.T) {
	for _, path := range fixtures(t) {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), "cashier-demo") {
			t.Errorf("%s contains the failpoint token", filepath.Base(path))
		}
	}
}

func TestFixtureStatuses(t *testing.T) {
	want := map[string]string{
		"ignite.ndjson": "passed", "ignite-load.ndjson": "passed", "ignite-load-live.ndjson": "passed", "ignite-failed.ndjson": "failed",
		"cashier-charge.ndjson": "passed", "cashier-charge-slow.ndjson": "passed", "run-failed.ndjson": "errored",
	}
	for name, status := range want {
		rec, err := ReadFile(filepath.Join(fixtureDir, "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := Summary(rec).Status; got != status {
			t.Errorf("%s: status %s, want %s", name, got, status)
		}
	}
}
