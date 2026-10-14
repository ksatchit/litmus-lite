package main

import "testing"

func TestHTMLPathFor(t *testing.T) {
	cases := map[string]string{
		"report.json":     "report.html",
		"baseline.json":   "baseline.html",
		"out/run-1.json":  "out/run-1.html",
		"noext":           "noext.html",
		"already.html":    "already.html.html",
		"dir.v2/result":   "dir.v2/result.html",
		"/tmp/a.b.c.json": "/tmp/a.b.c.html",
	}
	for in, want := range cases {
		if got := htmlPathFor(in); got != want {
			t.Errorf("htmlPathFor(%q) = %q, want %q", in, got, want)
		}
	}
}
