package safety

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

const DefaultMaxDuration = 60 * time.Second

type Options struct {
	AllowTargets []string
	AllowPIDs    []int
	Yes          bool
}

func Check(s *scenario.Scenario, opt Options) error {
	var max time.Duration
	for _, f := range s.Faults {
		d := f.DurationTime()
		if d > max {
			max = d
		}
		if needsYes(f.Kind) && !opt.Yes {
			return fmt.Errorf("fault %s (%s) requires -yes", f.Name, f.Kind)
		}
		if host := scenario.StringParam(f.Params, "upstream"); host != "" {
			if err := hostAllowed(host, opt); err != nil {
				return err
			}
		}
		// TODO(non-loopback-listen): today a non-loopback listen address is accepted
		// if it appears in -allow-target. The documented intent is that host proxies
		// stay loopback-only. Containerized apps are to be served by a Docker
		// sidecar proxy instead. Reaching the proxy from VMs, WSL2, local Kubernetes
		// or LAN devices is a deferred alternative: a guarded non-loopback bind that
		// needs -yes, an explicit address and a mandatory allowFrom CIDR list
		// enforced at accept time. See docs/ROADMAP.md. Revisit this check when
		// either is built.
		if listen := scenario.StringParam(f.Params, "listen"); listen != "" {
			if err := hostAllowed(listen, opt); err != nil {
				return err
			}
		}
		if err := checkProcessTarget(f, opt.AllowPIDs); err != nil {
			return err
		}
		if f.Kind == "failpoint" {
			if raw := scenario.StringParam(f.Params, "url"); raw != "" {
				if err := hostAllowed(raw, opt); err != nil {
					return fmt.Errorf("fault %s: %w", f.Name, err)
				}
			}
		}
	}
	if max > DefaultMaxDuration && !opt.Yes {
		return fmt.Errorf("fault duration %s exceeds default cap %s (pass -yes)", max, DefaultMaxDuration)
	}
	for _, p := range append(append([]scenario.Probe{}, s.SteadyState...), s.Probes...) {
		if p.URL != "" {
			if err := hostAllowed(p.URL, opt); err != nil {
				return err
			}
		}
	}
	if s.Target.BaseURL != "" {
		if err := hostAllowed(s.Target.BaseURL, opt); err != nil {
			return err
		}
	}
	return nil
}

type allowPIDKey struct{}

// WithAllowPIDs attaches the -allow-pid list to ctx so fault plugins can
// refuse to signal anything else. The CLI checks the same list before run.
func WithAllowPIDs(ctx context.Context, pids []int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cp := append([]int(nil), pids...)
	return context.WithValue(ctx, allowPIDKey{}, cp)
}

func AllowPIDsFrom(ctx context.Context) []int {
	if ctx == nil {
		return nil
	}
	pids, _ := ctx.Value(allowPIDKey{}).([]int)
	return pids
}

func PIDAllowed(pid int, allow []int) bool {
	for _, a := range allow {
		if a == pid {
			return true
		}
	}
	return false
}

// ParsePIDs parses a comma-separated -allow-pid value. Empty is an empty list.
func ParsePIDs(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("-allow-pid %q: want a positive integer", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func checkProcessTarget(f scenario.Fault, allow []int) error {
	if f.Kind != "process.pause" && f.Kind != "process.kill" {
		return nil
	}
	pid := scenario.IntParam(f.Params, "pid", 0)
	if pid > 0 {
		if pid <= 1 || pid == os.Getpid() {
			return fmt.Errorf("fault %s refuses to signal pid %d", f.Name, pid)
		}
		if !PIDAllowed(pid, allow) {
			return fmt.Errorf("fault %s pid %d is not allowed; pass -allow-pid %d", f.Name, pid, pid)
		}
		return nil
	}
	if scenario.StringParam(f.Params, "command") == "" {
		return nil
	}
	if len(allow) == 0 {
		return fmt.Errorf("fault %s (%s) resolves a process by command; pass -allow-pid for the PID it may signal", f.Name, f.Kind)
	}
	return nil
}

func needsYes(kind string) bool {
	switch kind {
	case "process.kill", "disk.fill":
		return true
	default:
		return false
	}
}

// hostAllowed says whether the scenario may touch raw (a URL, host or
// host:port). Loopback always. Any other host must be named in -allow-target.
// A Kubernetes service host (.svc) must be named too and the run must have -yes:
// it is a service inside a cluster, not a machine the person is sitting at.
func hostAllowed(raw string, opt Options) error {
	allow := opt.AllowTargets
	host := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("url %q: %w", raw, err)
		}
		host = u.Hostname()
		if host == "" {
			host = u.Host
		}
	} else if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if isLoopback(host) {
		return nil
	}
	lc := strings.ToLower(host)
	named := false
	for _, a := range allow {
		if strings.EqualFold(a, host) || strings.EqualFold(a, raw) {
			named = true
		}
	}
	if strings.HasSuffix(lc, ".svc") || strings.HasSuffix(lc, ".svc.cluster.local") {
		if named && opt.Yes {
			return nil
		}
		return fmt.Errorf("refusing Kubernetes service host %q: it needs both -allow-target %s and -yes", host, host)
	}
	if named {
		return nil
	}
	return fmt.Errorf("host %q is not loopback; pass -allow-target %s", host, host)
}

func isLoopback(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0:0:0:0:0:0:0:1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func NeedsYesForK8s(raw string) bool {
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	lc := strings.ToLower(host)
	return strings.Contains(lc, ".svc")
}
