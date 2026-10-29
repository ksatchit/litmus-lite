package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func bodyServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func httpProbe(url string, body map[string]any) scenario.Probe {
	return scenario.Probe{Name: "b", Type: "http", URL: url, Expect: map[string]any{"body": body}}
}

// A 200 with the wrong body is the failure this exists to catch: a gateway
// that answers 200 with an error page, or a cache that serves stale data.
func TestBodyAssertionsFailA200(t *testing.T) {
	srv := bodyServer(200, `{"status":"degraded","token":"s3cr3t-value"}`)
	defer srv.Close()
	for name, tc := range map[string]struct {
		body map[string]any
		ok   bool
		want string
	}{
		"contains holds":      {map[string]any{"contains": "degraded"}, true, ""},
		"contains fails":      {map[string]any{"contains": "healthy"}, false, `body does not contain "healthy"`},
		"notContains holds":   {map[string]any{"notContains": "healthy"}, true, ""},
		"notContains fails":   {map[string]any{"notContains": "degraded"}, false, `which it must not`},
		"matches holds":       {map[string]any{"matches": `"status":"[a-z]+"`}, true, ""},
		"matches fails":       {map[string]any{"matches": `^ok$`}, false, "does not match"},
		"every entry is held": {map[string]any{"contains": []any{"degraded", "missing"}}, false, `"missing"`},
	} {
		s := Once(context.Background(), httpProbe(srv.URL, tc.body))
		if s.OK != tc.ok || (tc.want != "" && !strings.Contains(s.Error, tc.want)) {
			t.Errorf("%s: ok=%v error=%q", name, s.OK, s.Error)
		}
		if s.Status != 200 {
			t.Errorf("%s: the status is not recorded: %d", name, s.Status)
		}
		// The failure goes into the report; the body (a token here) must not.
		if strings.Contains(s.Error, "s3cr3t-value") {
			t.Errorf("%s: the reason quotes the body: %q", name, s.Error)
		}
	}
}

// The status is still judged first, and its reason is the one recorded.
func TestWrongStatusIsReportedBeforeTheBody(t *testing.T) {
	srv := bodyServer(500, "ok")
	defer srv.Close()
	s := Once(context.Background(), httpProbe(srv.URL, map[string]any{"contains": "ok"}))
	if s.OK || s.Status != 500 || !strings.Contains(s.Error, "500") {
		t.Errorf("%+v", s)
	}
}

// Only the first MaxBodyBytes are judged: a response that is much larger must
// not be held in memory, and what is past the cap cannot satisfy an assertion.
func TestBodyAssertionsReadAtMostTheCap(t *testing.T) {
	pad := strings.Repeat("x", scenario.MaxBodyBytes)
	srv := bodyServer(200, pad+"TAIL")
	defer srv.Close()
	if s := Once(context.Background(), httpProbe(srv.URL, map[string]any{"contains": "TAIL"})); s.OK {
		t.Error("an assertion was satisfied by bytes past the cap")
	}
	if s := Once(context.Background(), httpProbe(srv.URL, map[string]any{"contains": "xxx"})); !s.OK {
		t.Errorf("the start of the body was not read: %+v", s)
	}
}

// Without a body assertion nothing changes: any body passes on status alone.
func TestNoBodyAssertionLeavesTheProbeAlone(t *testing.T) {
	srv := bodyServer(200, "anything")
	defer srv.Close()
	if s := Once(context.Background(), scenario.Probe{Name: "b", Type: "http", URL: srv.URL, Expect: map[string]any{"status": 200}}); !s.OK {
		t.Errorf("%+v", s)
	}
}

func TestLintKnowsBodyForHTTPOnly(t *testing.T) {
	if got := Lint(scenario.Probe{Name: "a", Type: "http", Expect: map[string]any{"body": map[string]any{"contains": "x"}}}); len(got) != 0 {
		t.Errorf("http reads body: %v", got)
	}
	got := Lint(scenario.Probe{Name: "a", Type: "cmd", Expect: map[string]any{"body": map[string]any{"contains": "x"}}})
	if len(got) != 1 || !strings.Contains(got[0], `unknown expect key "body"`) {
		t.Errorf("a command probe has no body: %v", got)
	}
}
