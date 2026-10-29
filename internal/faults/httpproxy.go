package faults

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type httpProxy struct{}

func (httpProxy) Name() string     { return "http.latency" }
func (httpProxy) OS() []string     { return nil }
func (httpProxy) EarlyStart() bool { return true }

// httpParamKeys are the params all three http kinds read.
var httpParamKeys = []string{
	"listen", "upstream", "delay", "jitter", "timeout", "percent", "code",
	"statusOverride", "statusOverride.percent", "statusOverride.code",
}

func (httpProxy) ParamKeys() []string    { return httpParamKeys }
func (statusInject) ParamKeys() []string { return httpParamKeys }
func (httpTimeout) ParamKeys() []string  { return httpParamKeys }

func (httpProxy) Validate(f scenario.Fault) error {
	if scenario.StringParam(f.Params, "listen") == "" {
		return fmt.Errorf("%s: params.listen is required", f.Kind)
	}
	if scenario.StringParam(f.Params, "upstream") == "" {
		return fmt.Errorf("%s: params.upstream is required", f.Kind)
	}
	// A value that does not parse used to become 0 and inject nothing, so a
	// run with "delay: 800" (no unit) passed having injected no delay.
	for _, k := range []string{"delay", "jitter", "timeout"} {
		raw := strings.TrimSpace(scenario.StringParam(f.Params, k))
		if raw == "" {
			continue
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("%s: params.%s %q is not a duration (write a unit, for example 800ms or 2s)", f.Kind, k, raw)
		}
		if d < 0 {
			return fmt.Errorf("%s: params.%s %q must not be negative", f.Kind, k, raw)
		}
	}
	check := func(where string, params map[string]any) error {
		if params == nil {
			return nil
		}
		if v, ok := params["percent"]; ok && !isNumber(v) {
			return fmt.Errorf("%s: %s.percent %v must be a number from 0 to 100", f.Kind, where, v)
		}
		if p := scenario.FloatParam(params, "percent", 0); p < 0 || p > 100 {
			return fmt.Errorf("%s: %s.percent %v must be from 0 to 100", f.Kind, where, p)
		}
		if v, ok := params["code"]; ok && !isNumber(v) {
			return fmt.Errorf("%s: %s.code %v must be an HTTP status code", f.Kind, where, v)
		}
		if c, ok := params["code"]; ok {
			if n := scenario.IntParam(params, "code", 0); n < 100 || n > 599 {
				return fmt.Errorf("%s: %s.code %v must be an HTTP status code from 100 to 599", f.Kind, where, c)
			}
		}
		return nil
	}
	if err := check("params", f.Params); err != nil {
		return err
	}
	if raw, ok := f.Params["statusOverride"]; ok {
		m, isMap := raw.(map[string]any)
		if !isMap {
			return fmt.Errorf("%s: params.statusOverride must be a map with percent and code", f.Kind)
		}
		return check("params.statusOverride", m)
	}
	return nil
}

// isNumber is true for a YAML number or a string that is one.
func isNumber(v any) bool {
	switch t := v.(type) {
	case int, int64, float64:
		return true
	case string:
		_, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return err == nil
	default:
		return false
	}
}

func (h httpProxy) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	if err := h.Validate(f); err != nil {
		return nil, err
	}
	return startHTTPProxy(ctx, f)
}

type statusInject struct{}

func (statusInject) Name() string     { return "http.status-inject" }
func (statusInject) OS() []string     { return nil }
func (statusInject) EarlyStart() bool { return true }
func (statusInject) Validate(f scenario.Fault) error {
	return httpProxy{}.Validate(f)
}
func (statusInject) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	return startHTTPProxy(ctx, f)
}

type httpTimeout struct{}

func (httpTimeout) Name() string     { return "http.timeout" }
func (httpTimeout) OS() []string     { return nil }
func (httpTimeout) EarlyStart() bool { return true }
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
	// Validate has already refused a delay, jitter or timeout that is not a
	// duration (it used to inject nothing), so the errors are not lost here.
	// Unknown param keys are warnings (ParamKeys, lint.go); when they become
	// errors is deferred (see TODO(strictness) in fault.go).
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

	run := &proxyRun{delay: delay, jitter: jitter, stall: stall, percent: percent, code: code}
	run.armed.Store(true)
	if delay > 0 {
		run.intensity = delay.Seconds() * 1000
	} else if percent > 0 {
		run.intensity = percent
	} else if stall > 0 {
		run.intensity = stall.Seconds() * 1000
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if run.armed.Load() {
			run.requests.Add(1)
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
	run.stop = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		_ = ln.Close()
		wg.Wait()
		return err
	}
	return run, nil
}

type proxyRun struct {
	armed     atomic.Bool
	requests  atomic.Int64
	delay     time.Duration
	jitter    time.Duration
	stall     time.Duration
	percent   float64
	code      int
	intensity float64
	stop      func() error
}

func (p *proxyRun) Intensity() float64 {
	if !p.armed.Load() {
		return 0
	}
	return p.intensity
}
func (p *proxyRun) Requests() int64 { return p.requests.Load() }
func (p *proxyRun) Arm()            { p.armed.Store(true) }
func (p *proxyRun) Disarm()         { p.armed.Store(false) }
func (p *proxyRun) Stop() error {
	if p.stop != nil {
		return p.stop()
	}
	return nil
}
