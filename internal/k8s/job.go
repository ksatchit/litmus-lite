package k8s

import (
	"fmt"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// JobYAML renders a Kubernetes Job that runs litmus-lite (no CRD, no ChaosEngine).
func JobYAML(s *scenario.Scenario) string {
	name := s.Metadata.Name
	if name == "" {
		name = "litmus-lite"
	}
	name = strings.ReplaceAll(strings.ToLower(name), "_", "-")
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: litmus-lite
          image: ghcr.io/litmuschaos/litmus-lite:latest
          args: ["run", "/scenario.chaos.yaml", "-yes"]
          volumeMounts:
            - name: scenario
              mountPath: /scenario.chaos.yaml
              subPath: scenario.chaos.yaml
      volumes:
        - name: scenario
          configMap:
            name: %s-scenario
`, name, name)
}
