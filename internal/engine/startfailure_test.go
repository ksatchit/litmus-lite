package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/probe"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func startFailureOf(t *testing.T, p scenario.Probe) string {
	t.Helper()
	sc := &scenario.Scenario{
		Metadata: scenario.Metadata{Name: "x"}, Baseline: "10ms", Recover: "10ms",
		SteadyState: []scenario.Probe{p},
	}
	_, err := Run(context.Background(), sc, Options{})
	if err == nil {
		t.Fatal("want an error")
	}
	return err.Error()
}

// The first failure a new user meets is a service that is not running. The
// message used to be only `steady-state probe "healthz" failed`.
func TestStartFailureSaysWhereTheProbeLookedAndWhy(t *testing.T) {
	addr := freeAddr(t)
	got := startFailureOf(t, scenario.Probe{Name: "healthz", Type: "http", URL: "http://" + addr + "/healthz", Timeout: "500ms"})
	for _, want := range []string{`steady-state probe "healthz" failed`, "GET http://" + addr + "/healthz", "refused", "Is the service running"} {
		if !strings.Contains(got, want) {
			t.Errorf("message lacks %q:\n%s", want, got)
		}
	}
}

// Windows words the same failure differently, and the hint must follow.
func TestStartFailureKnowsTheWindowsWordingOfARefusedConnection(t *testing.T) {
	err := startFailure(scenario.Probe{Name: "healthz", Type: "http", URL: "http://127.0.0.1:1/healthz"}, probe.Sample{
		Error: `Get "http://127.0.0.1:1/healthz": dial tcp 127.0.0.1:1: connectex: No connection could be made because the target machine actively refused it.`,
	})
	if !strings.Contains(err.Error(), "Is the service running") {
		t.Errorf("%v", err)
	}
	if refused("503 Service Unavailable") {
		t.Error("a status is not a refused connection")
	}
}

func TestStartFailureNamesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	got := startFailureOf(t, scenario.Probe{Name: "healthz", Type: "http", URL: srv.URL + "/healthz", Method: "HEAD"})
	if !strings.Contains(got, "HEAD "+srv.URL+"/healthz") || !strings.Contains(got, "503") {
		t.Errorf("%s", got)
	}
	if strings.Contains(got, "Is the service running") {
		t.Errorf("a 503 is not an unreachable service: %s", got)
	}
}

// The message goes to stderr, to a stream event and to a CI log: a token in
// the probe's URL must not.
func TestStartFailureDoesNotRepeatCredentialsFromTheURL(t *testing.T) {
	addr := freeAddr(t)
	got := startFailureOf(t, scenario.Probe{
		Name: "healthz", Type: "http", Timeout: "500ms",
		URL: "http://user:hunter2@" + addr + "/healthz?token=s3cr3t#frag",
	})
	for _, secret := range []string{"hunter2", "s3cr3t", "token=", "user:", "frag"} {
		if strings.Contains(got, secret) {
			t.Errorf("the message repeats %q:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, "GET http://"+addr+"/healthz") || !strings.Contains(got, "refused") {
		t.Errorf("%s", got)
	}
}

// A command probe's command can hold a secret; only its own output explains
// the failure.
func TestStartFailureDoesNotRepeatACommand(t *testing.T) {
	got := startFailureOf(t, scenario.Probe{Name: "db", Type: "cmd", Command: "exit 7 # topsecret"})
	if strings.Contains(got, "topsecret") || !strings.Contains(got, `steady-state probe "db" failed`) {
		t.Errorf("%s", got)
	}
}

func TestStartFailureIsOneShortLine(t *testing.T) {
	got := startFailureOf(t, scenario.Probe{Name: "db", Type: "cmd", Command: "printf 'line one\\nline two'; exit 1"})
	if strings.Contains(got, "\n") || strings.Contains(got, "line two") {
		t.Errorf("%q", got)
	}
	long := firstLine(strings.Repeat("x", 1000))
	if len(long) > maxFailureText+3 {
		t.Errorf("not cut: %d", len(long))
	}
}
