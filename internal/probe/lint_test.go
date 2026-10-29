package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestLint(t *testing.T) {
	cases := []struct {
		name string
		p    scenario.Probe
		want string // a substring of the one warning; "" for none
	}{
		{"http ok", scenario.Probe{Name: "a", Type: "http", Expect: map[string]any{"status": 200}}, ""},
		{"no expect", scenario.Probe{Name: "a", Type: "http"}, ""},
		{"default type is http", scenario.Probe{Name: "a", Expect: map[string]any{"status": 204}}, ""},
		{"cmd ok", scenario.Probe{Name: "a", Type: "cmd", Expect: map[string]any{"contains": "ok"}}, ""},
		{"command ok", scenario.Probe{Name: "a", Type: "COMMAND", Expect: map[string]any{"contains": "ok"}}, ""},
		{"http reads status, not contains", scenario.Probe{Name: "a", Type: "http", Expect: map[string]any{"contains": "ok"}}, `unknown expect key "contains"`},
		{"cmd reads contains, not status", scenario.Probe{Name: "a", Type: "cmd", Expect: map[string]any{"status": 200}}, `unknown expect key "status"`},
		{"typo", scenario.Probe{Name: "a", Type: "http", Expect: map[string]any{"staus": 200}}, `unknown expect key "staus"`},
		{"status as text", scenario.Probe{Name: "a", Type: "http", Expect: map[string]any{"status": "503"}}, "not a number"},
	}
	for _, c := range cases {
		got := Lint(c.p)
		switch {
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: unexpected %v", c.name, got)
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
			t.Errorf("%s: got %v, want one containing %q", c.name, got, c.want)
		}
	}
}

// expect.status: "503" is a string; it used to be dropped, so the probe
// wanted 200 and failed on the very response the author asked for. It is
// still read as 200 (warned about by Lint), and a number still works.
func TestStatusExpectationIsReadAsANumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	for status, want := range map[any]bool{503: true, int64(503): true, float64(503): true, "503": false} {
		s := Once(context.Background(), scenario.Probe{Name: "a", Type: "http", URL: srv.URL, Expect: map[string]any{"status": status}})
		if s.OK != want {
			t.Errorf("expect.status %#v: ok=%v, want %v", status, s.OK, want)
		}
	}
	// The text is not a number, so it expects the default, 200 (Lint says so).
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	s := Once(context.Background(), scenario.Probe{Name: "a", Type: "http", URL: ok.URL, Expect: map[string]any{"status": "503"}})
	if !s.OK {
		t.Errorf("a text status fell back to something other than 200: %+v", s)
	}
}
