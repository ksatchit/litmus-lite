package stream

import (
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

var (
	// key=value or key: value inside a command line, where key looks secret.
	inlineSecret = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:token|secret|password|passwd|key)[A-Za-z0-9_.-]*)(\s*[=:]\s*|\s+)("[^"]*"|'[^']*'|\S+)`)
	secretFlag   = regexp.MustCompile(`(?i)^--?[A-Za-z0-9_.-]*(token|secret|password|passwd|key)[A-Za-z0-9_.-]*$`)
)

// RedactedScenarioYAML renders a scenario for run.started (-stream-scenario),
// with what looks like a credential masked. It is best effort, which is why it
// is off by default:
//
//   - fault params: the same rule as Redact (keys that look secret);
//   - URLs: user:password and query values of keys that look secret;
//   - load args: the value after a flag whose name looks secret, and
//     name=value pairs of that kind;
//   - command text of a probe or load tool: name=value and name: value pairs
//     whose name looks secret.
//
// A secret written in some other shape, for example a bare token in a shell
// command, cannot be recognised.
func RedactedScenarioYAML(sc *scenario.Scenario) string {
	c := *sc
	c.Faults = make([]scenario.Fault, len(sc.Faults))
	for i, f := range sc.Faults {
		f.Params = Redact(f.Params)
		c.Faults[i] = f
	}
	c.SteadyState = redactProbes(sc.SteadyState)
	c.Probes = redactProbes(sc.Probes)
	if sc.Load != nil {
		l := *sc.Load
		l.Command = redactCommand(l.Command)
		l.Args = redactArgs(l.Args)
		c.Load = &l
	}
	if sc.Observability != nil && sc.Observability.Prometheus != nil {
		o := *sc.Observability
		p := *o.Prometheus
		p.URL = redactURL(p.URL)
		o.Prometheus = &p
		c.Observability = &o
	}
	c.Target.BaseURL = redactURL(c.Target.BaseURL)
	b, err := yaml.Marshal(&c)
	if err != nil {
		return ""
	}
	return string(b)
}

func redactProbes(in []scenario.Probe) []scenario.Probe {
	if in == nil {
		return nil
	}
	out := make([]scenario.Probe, len(in))
	for i, p := range in {
		p.URL = redactURL(p.URL)
		p.Command = redactCommand(p.Command)
		p.Expect = Redact(p.Expect)
		out[i] = p
	}
	return out
}

func redactURL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return redactCommand(raw)
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	q := u.Query()
	changed := false
	for k := range q {
		if secretKey.MatchString(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func redactCommand(s string) string {
	if s == "" {
		return s
	}
	return inlineSecret.ReplaceAllStringFunc(s, func(m string) string {
		sub := inlineSecret.FindStringSubmatch(m)
		return sub[1] + sub[2] + `"***"`
	})
}

func redactArgs(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	mask := false
	for i, a := range in {
		switch {
		case mask:
			out[i] = "***"
			mask = false
		case strings.HasPrefix(a, "-") && strings.Contains(a, "="):
			k, v, _ := strings.Cut(a, "=")
			if secretFlag.MatchString(k) {
				v = "***"
			} else {
				v = redactCommand(v)
			}
			out[i] = k + "=" + v
		case secretFlag.MatchString(a):
			out[i] = a
			mask = true
		default:
			out[i] = redactCommand(a)
		}
	}
	return out
}
