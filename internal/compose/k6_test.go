package compose

import (
	"context"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestSeriesFromK6BucketsHTTP(t *testing.T) {
	raw := []byte(`
{"type":"Metric","data":{"name":"http_reqs","type":"counter"},"metric":"http_reqs"}
{"metric":"http_reqs","type":"Point","data":{"time":"2024-01-01T00:00:00.000Z","value":1,"tags":{"status":"200"}}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2024-01-01T00:00:00.100Z","value":0}}
{"metric":"http_reqs","type":"Point","data":{"time":"2024-01-01T00:00:00.200Z","value":1}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2024-01-01T00:00:00.200Z","value":1}}
{"metric":"http_reqs","type":"Point","data":{"time":"2024-01-01T00:00:01.100Z","value":1}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2024-01-01T00:00:01.100Z","value":0}}
{"metric":"http_req_duration","type":"Point","data":{"time":"2024-01-01T00:00:01.100Z","value":12}}
`)
	pts, err := seriesFromK6(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("%+v", pts)
	}
	if pts[0].OffsetS != 0 || pts[0].RPS != 2 || pts[0].ErrorRate != 0.5 {
		t.Fatalf("second 0: %+v", pts[0])
	}
	if pts[1].OffsetS != 1 || pts[1].RPS != 1 || pts[1].ErrorRate != 0 {
		t.Fatalf("second 1: %+v", pts[1])
	}
}

func TestSeriesFromCommand(t *testing.T) {
	pts, err := seriesFromCommand([]byte(`{"time_series":[{"offset_s":0,"rps":9,"error_rate":0.1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].RPS != 9 || pts[0].ErrorRate != 0.1 {
		t.Fatalf("%+v", pts)
	}
	if _, err := seriesFromCommand([]byte(`{"ok":true}`)); err == nil {
		t.Fatal("expected missing time_series")
	}
}

func TestMissingCommandWarns(t *testing.T) {
	out, err := Run(context.Background(), "", "", &scenario.LoadSpec{Tool: "command", Command: "litmus-lite-no-such-load"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Warn == "" || len(out.Series) != 0 {
		t.Fatalf("%+v", out)
	}
}
