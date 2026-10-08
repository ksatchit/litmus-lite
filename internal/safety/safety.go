package safety

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
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
			if err := hostAllowed(host, opt.AllowTargets); err != nil {
				return err
			}
		}
		if listen := scenario.StringParam(f.Params, "listen"); listen != "" {
			if err := hostAllowed(listen, opt.AllowTargets); err != nil {
				return err
			}
		}
	}
	if max > DefaultMaxDuration && !opt.Yes {
		return fmt.Errorf("fault duration %s exceeds default cap %s (pass -yes)", max, DefaultMaxDuration)
	}
	for _, p := range append(append([]scenario.Probe{}, s.SteadyState...), s.Probes...) {
		if p.URL != "" {
			if err := hostAllowed(p.URL, opt.AllowTargets); err != nil {
				return err
			}
		}
	}
	if s.Target.BaseURL != "" {
		if err := hostAllowed(s.Target.BaseURL, opt.AllowTargets); err != nil {
			return err
		}
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

func hostAllowed(raw string, allow []string) error {
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
	if strings.HasSuffix(lc, ".svc") || strings.HasSuffix(lc, ".svc.cluster.local") {
		return fmt.Errorf("refusing Kubernetes service host %q without -allow-target and -yes", host)
	}
	for _, a := range allow {
		if strings.EqualFold(a, host) || strings.EqualFold(a, raw) {
			return nil
		}
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
