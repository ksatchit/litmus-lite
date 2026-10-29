package faults

import (
	"strings"
	"testing"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// The container name goes to the docker CLI as an argument, so it must not be
// able to be an option. This is decided before docker is looked up, so it holds
// on a machine without docker too.
func TestDockerPauseRefusesAnOptionAsAName(t *testing.T) {
	for _, params := range []map[string]any{
		{"name": "--help"},
		{"name": " -a"},
		{"id": "-f"},
	} {
		err := dockerPause{}.Validate(scenario.Fault{Params: params})
		if err == nil || !strings.Contains(err.Error(), "must not start with '-'") {
			t.Errorf("%v: got %v", params, err)
		}
	}
}
