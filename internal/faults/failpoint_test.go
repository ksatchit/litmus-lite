package faults

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/litmuschaos/litmus-lite/failpoint"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

func TestFailpointValidate(t *testing.T) {
	inj, err := Lookup("failpoint")
	if err != nil {
		t.Fatal(err)
	}
	ok := scenario.Fault{Name: "a", Kind: "failpoint", Duration: "1s", Params: map[string]any{
		"url": "http://127.0.0.1:8091/_litmus/failpoint", "name": "charge", "token": "cashier-demo",
	}}
	if err := inj.Validate(ok); err != nil {
		t.Fatal(err)
	}
	for _, p := range []map[string]any{
		{"name": "charge", "token": "t"},
		{"url": "127.0.0.1:8091", "name": "charge", "token": "t"},
		{"url": "http://127.0.0.1:8091/x", "token": "t"},
		{"url": "http://127.0.0.1:8091/x", "name": "charge"},
	} {
		f := scenario.Fault{Name: "a", Kind: "failpoint", Params: p}
		if err := inj.Validate(f); err == nil {
			t.Fatalf("expected error for %+v", p)
		}
	}
}

func TestFailpointStartStop(t *testing.T) {
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "cashier-demo")
	srv := httptest.NewServer(failpoint.Handler())
	defer srv.Close()
	inj, err := Lookup("failpoint")
	if err != nil {
		t.Fatal(err)
	}
	f := scenario.Fault{Name: "charge-fails", Kind: "failpoint", Duration: "1s", Params: map[string]any{
		"url": srv.URL, "name": "charge", "token": "cashier-demo",
	}}
	bad := f
	bad.Params = map[string]any{"url": srv.URL, "name": "charge", "token": "nope"}
	if _, err := inj.Start(context.Background(), bad); err == nil {
		t.Fatal("expected token failure")
	}
	if err := failpoint.Fail("charge"); err != nil {
		t.Fatal(err)
	}
	running, err := inj.Start(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if err := failpoint.Fail("charge"); err == nil {
		t.Fatal("expected armed error")
	}
	if err := running.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := failpoint.Fail("charge"); err != nil {
		t.Fatal(err)
	}
}

func TestFailpointUnlistedName(t *testing.T) {
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "cashier-demo")
	srv := httptest.NewServer(failpoint.Handler())
	defer srv.Close()
	inj, err := Lookup("failpoint")
	if err != nil {
		t.Fatal(err)
	}
	f := scenario.Fault{Name: "a", Kind: "failpoint", Params: map[string]any{
		"url": srv.URL, "name": "other", "token": "cashier-demo",
	}}
	if _, err := inj.Start(context.Background(), f); err == nil {
		t.Fatal("expected unlisted name to fail")
	}
}
