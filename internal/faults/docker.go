package faults

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

type dockerPause struct{}

func (dockerPause) Name() string { return "docker.pause" }
func (dockerPause) OS() []string { return nil }

// TODO(allow-container): docker.pause is currently ungated: any container name
// or id in a scenario is paused without an allowlist check. The newer container
// faults (kill, stop, restart, network disconnect) are planned to require the
// target to be allowlisted via -allow-container or containers.allow in
// .litmus-lite/config.yaml, mirroring -allow-pid for process faults. Retrofitting
// docker.pause the same way is deliberately deferred and will be revisited; when
// it lands it should come with a deprecation message first.
func (dockerPause) ParamKeys() []string { return []string{"name", "id"} }

func (dockerPause) Validate(f scenario.Fault) error {
	if scenario.StringParam(f.Params, "name") == "" && scenario.StringParam(f.Params, "id") == "" {
		return fmt.Errorf("docker.pause needs params.name or params.id")
	}
	// The name or id is an argument of the docker CLI: one that starts with '-'
	// would be read as an option (docker pause --help, or worse).
	for _, k := range []string{"name", "id"} {
		if v := scenario.StringParam(f.Params, k); strings.HasPrefix(strings.TrimSpace(v), "-") {
			return fmt.Errorf("docker.pause: params.%s %q must not start with '-'", k, v)
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker.pause: docker CLI not found on PATH")
	}
	return nil
}

func (d dockerPause) Start(_ context.Context, f scenario.Fault) (Running, error) {
	if err := d.Validate(f); err != nil {
		return nil, err
	}
	target := scenario.StringParam(f.Params, "name")
	if target == "" {
		target = scenario.StringParam(f.Params, "id")
	}
	if out, err := exec.Command("docker", "pause", target).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker pause %s: %s: %w", target, strings.TrimSpace(string(out)), err)
	}
	return stopPair{
		intensity: 100,
		stop: func() error {
			out, err := exec.Command("docker", "unpause", target).CombinedOutput()
			if err == nil {
				return nil
			}
			msg := strings.ToLower(string(out))
			if strings.Contains(msg, "is not paused") || strings.Contains(msg, "no such container") {
				return nil
			}
			return fmt.Errorf("docker unpause %s: %s: %w", target, strings.TrimSpace(string(out)), err)
		},
	}, nil
}
