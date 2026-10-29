package faults

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// TrafficCounter is implemented by a fault that acts on requests passing
// through it (the HTTP proxies). Requests is how many arrived while the fault
// was armed. A fault that saw none affected nothing, however it was
// configured: the run measured the service with no fault in its path.
type TrafficCounter interface {
	Requests() int64
}

// proxyKinds are the faults that sit between a client and the service, and so
// only change what goes through their listen address.
var proxyKinds = map[string]bool{"http.latency": true, "http.status-inject": true, "http.timeout": true}

// lintTraffic warns about an HTTP fault that no probe goes through. The
// proxy listens on params.listen; a probe that calls the upstream address
// directly never meets it, so every hypothesis is judged on a service that
// was not disturbed and the run passes. It is the commonest mistake with these
// faults.
//
// It stays quiet when it cannot tell: a load generator or a command probe may
// well call the proxy.
func lintTraffic(sc *scenario.Scenario) []string {
	if sc.Load != nil {
		return nil
	}
	var probes []scenario.Probe
	probes = append(probes, sc.SteadyState...)
	probes = append(probes, sc.Probes...)
	var called []string
	for _, p := range probes {
		switch strings.ToLower(p.Type) {
		case "", "http":
			called = append(called, p.URL)
		default:
			return nil
		}
	}
	var out []string
	for _, f := range sc.Faults {
		if !proxyKinds[f.Kind] {
			continue
		}
		listen := strings.TrimSpace(scenario.StringParam(f.Params, "listen"))
		if listen == "" {
			continue
		}
		through := false
		var seen []string
		for _, raw := range called {
			if sameAddress(listen, raw) {
				through = true
				break
			}
			if a := hostPort(raw); a != "" && !contains(seen, a) {
				seen = append(seen, a)
			}
		}
		if through {
			continue
		}
		sort.Strings(seen)
		out = append(out, fmt.Sprintf(
			"fault %q (%s) listens on %s, but no probe calls that address (probes call %s), so no probe will see this fault and the hypotheses are judged on an undisturbed service. "+
				"Point a probe at the listen address, not at the upstream",
			f.Name, f.Kind, listen, strings.Join(seen, ", ")))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// hostPort is the host:port a probe URL calls, with the default port filled in.
func hostPort(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// sameAddress reports whether the probe URL calls the proxy's listen address.
// The loopback names are one address: a proxy on 127.0.0.1 is reached as
// localhost.
func sameAddress(listen, probeURL string) bool {
	call := hostPort(probeURL)
	if call == "" {
		return false
	}
	lh, lp, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	ch, cp, err := net.SplitHostPort(call)
	if err != nil || lp != cp {
		return false
	}
	if lh == "" || strings.EqualFold(lh, ch) {
		return true
	}
	return isLoopbackName(lh) && isLoopbackName(ch)
}

func isLoopbackName(h string) bool {
	switch strings.ToLower(strings.Trim(h, "[]")) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
