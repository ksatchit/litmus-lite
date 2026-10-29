package probe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type Sample struct {
	At      time.Time     `json:"at"`
	Name    string        `json:"name"`
	OK      bool          `json:"ok"`
	Latency time.Duration `json:"latency_ns"`
	Status  int           `json:"status,omitempty"`
	Error   string        `json:"error,omitempty"`
}

func Once(ctx context.Context, p scenario.Probe) Sample {
	s := Sample{At: time.Now(), Name: p.Name}
	timeout := 2 * time.Second
	if p.Timeout != "" {
		if d, err := time.ParseDuration(p.Timeout); err == nil {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	switch strings.ToLower(p.Type) {
	case "cmd", "command":
		s.OK, s.Error = runCmd(ctx, p)
	default:
		s.Status, s.OK, s.Error = runHTTP(ctx, p)
	}
	s.Latency = time.Since(start)
	return s
}

func runHTTP(ctx context.Context, p scenario.Probe) (status int, ok bool, errStr string) {
	method := p.Method
	if method == "" {
		method = http.MethodGet
	}
	checks, err := scenario.BodyExpect(p.Expect)
	if err != nil {
		return 0, false, err.Error()
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL, nil)
	if err != nil {
		return 0, false, err.Error()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, false, err.Error()
	}
	// The body is read only for an assertion about it, and only up to
	// MaxBodyBytes; otherwise it is drained and dropped, as before.
	var body []byte
	if checks.Any() {
		body, err = io.ReadAll(io.LimitReader(resp.Body, scenario.MaxBodyBytes))
	} else {
		_, err = io.Copy(io.Discard, resp.Body)
	}
	resp.Body.Close()
	want := 200
	if p.Expect != nil {
		if v, ok := p.Expect["status"]; ok {
			if n := statusOf(v); n != 0 {
				want = n
			}
		}
	}
	if resp.StatusCode != want {
		return resp.StatusCode, false, resp.Status
	}
	if checks.Any() {
		if err != nil {
			return resp.StatusCode, false, "reading the body: " + err.Error()
		}
		if reason, ok := checks.Check(body); !ok {
			return resp.StatusCode, false, reason
		}
	}
	return resp.StatusCode, true, ""
}

func runCmd(ctx context.Context, p scenario.Probe) (bool, string) {
	if strings.TrimSpace(p.Command) == "" {
		return false, "empty command"
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return false, err.Error() + " " + buf.String()
	}
	if p.Expect != nil {
		if sub, ok := p.Expect["contains"].(string); ok && sub != "" {
			if !strings.Contains(buf.String(), sub) {
				return false, "output missing substring"
			}
		}
	}
	return true, ""
}
