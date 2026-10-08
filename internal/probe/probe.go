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
	req, err := http.NewRequestWithContext(ctx, method, p.URL, nil)
	if err != nil {
		return 0, false, err.Error()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, false, err.Error()
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	want := 200
	if p.Expect != nil {
		if v, ok := p.Expect["status"]; ok {
			switch t := v.(type) {
			case int:
				want = t
			case int64:
				want = int(t)
			case float64:
				want = int(t)
			}
		}
	}
	if resp.StatusCode != want {
		return resp.StatusCode, false, resp.Status
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
