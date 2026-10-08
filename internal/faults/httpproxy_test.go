package faults

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func TestHTTPLatencyIsEarlyStarter(t *testing.T) {
	inj, err := Lookup("http.latency")
	if err != nil {
		t.Fatal(err)
	}
	e, ok := inj.(EarlyStarter)
	if !ok || !e.EarlyStart() {
		t.Fatalf("http.latency %T should be EarlyStarter", inj)
	}
}

func TestProxyLatencyAndStop(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer up.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()
	f := scenario.Fault{Name: "s", Kind: "http.latency", Duration: "2s", Params: map[string]any{
		"listen": listen, "upstream": up.URL[len("http://"):], "delay": "80ms",
	}}
	r, err := startHTTPProxy(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := http.Get("http://" + listen + "/")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if time.Since(start) < 70*time.Millisecond {
		t.Fatalf("expected delay, got %s", time.Since(start))
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.Listen("tcp", listen); err != nil {
		t.Fatalf("port still bound: %v", err)
	}
}

func TestStatusInject(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer up.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()
	f := scenario.Fault{Name: "s", Kind: "http.status-inject", Duration: "2s", Params: map[string]any{
		"listen": listen, "upstream": up.URL[len("http://"):], "percent": 100, "code": 503,
	}}
	r, err := startHTTPProxy(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	resp, err := http.Get("http://" + listen + "/")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status %d want 503", resp.StatusCode)
	}
}
