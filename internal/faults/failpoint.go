package faults

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// failpointFault arms a named hook in another process for the inject window.
// The target imports failpoint.Handler and allowlists the name. This plugin
// only speaks HTTP; it does not import the SDK.
type failpointFault struct{}

func (failpointFault) Name() string { return "failpoint" }
func (failpointFault) OS() []string { return nil }

func (failpointFault) ParamKeys() []string { return []string{"url", "name", "token"} }

func (failpointFault) Validate(f scenario.Fault) error {
	raw := strings.TrimSpace(scenario.StringParam(f.Params, "url"))
	if raw == "" {
		return fmt.Errorf("failpoint: params.url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("failpoint: params.url must be an absolute http(s) URL")
	}
	if strings.TrimSpace(scenario.StringParam(f.Params, "name")) == "" {
		return fmt.Errorf("failpoint: params.name is required")
	}
	if strings.TrimSpace(scenario.StringParam(f.Params, "token")) == "" {
		return fmt.Errorf("failpoint: params.token is required")
	}
	return nil
}

func (p failpointFault) Start(ctx context.Context, f scenario.Fault) (Running, error) {
	if err := p.Validate(f); err != nil {
		return nil, err
	}
	if err := callFailpoint(ctx, f, true); err != nil {
		return nil, fmt.Errorf("fault %s: %w", f.Name, err)
	}
	return stopPair{
		intensity: 100,
		stop: func() error {
			if err := callFailpoint(context.Background(), f, false); err != nil {
				return fmt.Errorf("fault %s: %w", f.Name, err)
			}
			return nil
		},
	}, nil
}

func callFailpoint(ctx context.Context, f scenario.Fault, arm bool) error {
	name := strings.TrimSpace(scenario.StringParam(f.Params, "name"))
	payload, err := json.Marshal(map[string]any{
		"name":  name,
		"armed": arm,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scenario.StringParam(f.Params, "url"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Litmus-Lite-Token", scenario.StringParam(f.Params, "token"))
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	verb := "arm"
	if !arm {
		verb = "disarm"
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", verb, name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s %s: %s", verb, name, msg)
	}
	return nil
}
