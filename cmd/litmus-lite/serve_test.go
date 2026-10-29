package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/litmuschaos/litmus-lite/internal/stream"
)

const fixtureDir = "../../docs/stream/fixtures"

func TestStreamReplayPushesARecording(t *testing.T) {
	clearStreamEnv(t)
	rx := t.TempDir()
	rcv, _ := stream.NewReceiver(rx)
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	fixture := filepath.Join(fixtureDir, "cashier-charge.ndjson")
	rec, _ := stream.ReadFile(fixture)

	var code int
	out := captureStderr(t, func() {
		code = run([]string{"litmus-lite", "stream", "replay", fixture, "-stream-push", srv.URL + "/v1/ingest", "-speed", "0", "-fresh"})
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "delivered") || !strings.Contains(out, "0 lost") {
		t.Fatalf("summary %q", out)
	}
	files, _ := filepath.Glob(filepath.Join(rx, "*.ndjson"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	got, err := stream.ReadFile(files[0])
	if err != nil || len(got.Events) != len(rec.Events) {
		t.Fatalf("stored %v %v", got, err)
	}
	if got.RunID() == rec.RunID() {
		t.Fatal("-fresh kept the run id")
	}
	if c := run([]string{"litmus-lite", "stream", "validate", files[0]}); c != 0 {
		t.Fatalf("what was pushed does not validate: %d", c)
	}
}

func TestStreamReplayPushExitsNonZeroWhenTheReceiverRejects(t *testing.T) {
	clearStreamEnv(t)
	rcv, _ := stream.NewReceiver(t.TempDir())
	rcv.Token = "right"
	srv := httptest.NewServer(rcv.Handler())
	defer srv.Close()
	t.Setenv("LITMUS_LITE_STREAM_TOKEN", "wrong")
	var code int
	out := captureStderr(t, func() {
		code = run([]string{"litmus-lite", "stream", "replay", filepath.Join(fixtureDir, "cashier-charge.ndjson"), "-stream-push", srv.URL + "/v1/ingest", "-speed", "0"})
	})
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, out)
	}
	if !strings.Contains(out, "401") || strings.Contains(out, "wrong") || strings.Contains(out, "right") {
		t.Fatalf("output %q", out)
	}
}

func TestStreamReplayPushFlagRules(t *testing.T) {
	clearStreamEnv(t)
	a := filepath.Join(fixtureDir, "cashier-charge.ndjson")
	b := filepath.Join(fixtureDir, "ignite-load.ndjson")
	cases := map[string][]string{
		"http to a remote host":       {"stream", "replay", a, "-stream-push", "http://collector.example.com/v1/ingest"},
		"run-id with two files":       {"stream", "replay", a, b, "-stream-push", "http://127.0.0.1:1/x", "-run-id", "one"},
		"run-id with fresh":           {"stream", "replay", a, "-stream-push", "http://127.0.0.1:1/x", "-run-id", "one", "-fresh"},
		"bad run id":                  {"stream", "replay", a, "-stream-push", "http://127.0.0.1:1/x", "-run-id", "../x"},
		"run-id without push":         {"stream", "replay", a, "-run-id", "one"},
		"fresh without push":          {"stream", "replay", a, "-fresh"},
		"serve on non-loopback":       {"stream", "replay", a, "-stream-pull", "0.0.0.0:0"},
		"push and non-loopback serve": {"stream", "replay", a, "-stream-push", "http://127.0.0.1:1/x", "-stream-pull", "0.0.0.0:0"},
	}
	for name, args := range cases {
		var code int
		_ = captureStderr(t, func() { code = run(append([]string{"litmus-lite"}, args...)) })
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func TestServeFlagRules(t *testing.T) {
	clearStreamEnv(t)
	var code int
	_ = captureStderr(t, func() { code = run([]string{"litmus-lite", "serve", "-listen", "0.0.0.0:0", "-dir", t.TempDir()}) })
	if code != 2 {
		t.Errorf("non-loopback without a token: exit %d, want 2", code)
	}
	_ = captureStderr(t, func() { code = run([]string{"litmus-lite", "serve", "somefile.ndjson"}) })
	if code != 2 {
		t.Errorf("a file argument: exit %d, want 2", code)
	}
	_ = captureStderr(t, func() { code = run([]string{"litmus-lite", "serve", "-listen", "not-an-address"}) })
	if code == 0 {
		t.Error("a bad address was accepted")
	}
}
