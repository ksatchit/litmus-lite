package faults

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type httpProxy struct{}

func (httpProxy) Name() string { return "http.latency" }
func (httpProxy) OS() []string { return nil }

func (httpProxy) Validate(f scenario.Fault) error {
	if scenario.StringParam(f.Params, "listen") == "" {
		return fmt.Errorf("%s: params.listen is required", f.Kind)
	}
	if scenario.StringParam(f.Params, "upstream") == "" {
		return fmt.Errorf("%s: params.upstream is required", f.Kind)
	}
	return nil
}

func (h httpProxy) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	if err := h.Validate(f); err != nil {
		return nil, err
	}
	return startHTTPProxy(ctx, f)
}

type statusInject struct{}

func (statusInject) Name() string { return "http.status-inject" }
func (statusInject) OS() []string { return nil }
func (statusInject) Validate(f scenario.Fault) error {
	return httpProxy{}.Validate(f)
}
func (statusInject) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	return startHTTPProxy(ctx, f)
}

type httpTimeout struct{}

func (httpTimeout) Name() string { return "http.timeout" }
func (httpTimeout) OS() []string { return nil }
func (httpTimeout) Validate(f scenario.Fault) error {
	return httpProxy{}.Validate(f)
}
func (httpTimeout) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	return startHTTPProxy(ctx, f)
}

func init() {
	Register(statusInject{})
	Register(httpTimeout{})
}

func startHTTPProxy(_ context.Context, f scenario.Fault) (Running, error) {
	listen := scenario.StringParam(f.Params, "listen")
	upstream := scenario.StringParam(f.Params, "upstream")
	if !strings.Contains(upstream, "://") {
		upstream = "http://" + upstream
	}
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("upstream: %w", err)
	}
	delay, _ := time.ParseDuration(scenario.StringParam(f.Params, "delay"))
	jitter, _ := time.ParseDuration(scenario.StringParam(f.Params, "jitter"))
	stall, _ := time.ParseDuration(scenario.StringParam(f.Params, "timeout"))
	ov := scenario.MapParam(f.Params, "statusOverride")
	percent := 0.0
	code := 500
	if ov != nil {
		percent = scenario.FloatParam(ov, "percent", 0)
		code = scenario.IntParam(ov, "code", 500)
	}
	if f.Kind == "http.status-inject" && percent == 0 {
		percent = scenario.FloatParam(f.Params, "percent", 100)
		code = scenario.IntParam(f.Params, "code", 500)
	}
	if f.Kind == "http.timeout" && stall == 0 {
		stall = f.DurationTime()
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	orig := proxy.Director
	proxy.Director = func(r *http.Request) {
		orig(r)
		r.Host = u.Host
	}

	intensity := delay.Seconds() * 1000
	if intensity == 0 && percent > 0 {
		intensity = percent
	}
	if intensity == 0 && stall > 0 {
		intensity = stall.Seconds() * 1000
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall > 0 {
			timer := time.NewTimer(stall)
			select {
			case <-r.Context().Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if delay > 0 || jitter > 0 {
			d := delay
			if jitter > 0 {
				d += time.Duration(rand.Int63n(int64(jitter) + 1))
			}
			time.Sleep(d)
		}
		if percent > 0 && rand.Float64()*100 < percent {
			http.Error(w, "litmus-lite injected status", code)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", listen, err)
	}
	srv := &http.Server{Handler: handler}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ln)
	}()

	return stopPair{
		intensity: intensity,
		stop: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := srv.Shutdown(ctx)
			_ = ln.Close()
			wg.Wait()
			return err
		},
	}, nil
}
