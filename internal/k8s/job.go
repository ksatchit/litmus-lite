package k8s

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// dnsLabel turns a scenario name into a name Kubernetes accepts and that is
// safe to write unquoted into YAML: lowercase letters, digits and '-', starting
// and ending with a letter or digit, short enough to take the "-scenario"
// suffix of the ConfigMap name (63 characters in all).
func dnsLabel(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 50 {
		out = strings.Trim(out[:50], "-")
	}
	if out == "" {
		return "litmus-lite"
	}
	return out
}

// JobOptions is what the manifests need besides the scenario.
type JobOptions struct {
	// Image is the container image that has litmus-lite on its entrypoint
	// path as `litmus-lite`. There is no default: no image is published by
	// the project, so the one to run is the one you built or mirrored.
	Image string
	// Scenario is the scenario file as written; it goes into the ConfigMap
	// the Job mounts.
	Scenario []byte
	// AllowTargets are forwarded as -allow-target, for a scenario that
	// reaches hosts that are not loopback.
	AllowTargets []string
	// Yes forwards -yes. The Job never passes it unless asked to here: an
	// unattended run keeps the guards that ask for it (disk.fill,
	// process.kill, faults over a minute, a Kubernetes service host), and
	// says so when one of them stops it.
	Yes bool
}

var (
	imageRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,254}$`)
	hostArg  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]{0,254}$`)
)

// Manifests renders the ConfigMap that holds the scenario and the Job that
// runs it, as one multi-document YAML. It is plain Kubernetes: no CRD, no
// ChaosEngine. The Job prints the report as JSON on its log.
func Manifests(s *scenario.Scenario, o JobOptions) (string, error) {
	if !imageRef.MatchString(o.Image) {
		return "", fmt.Errorf("image %q: pass -image with an image reference such as registry.example/litmus-lite:1.2.3", o.Image)
	}
	if len(o.Scenario) == 0 {
		return "", fmt.Errorf("the scenario file is empty")
	}
	args := []string{"run", "-output", "json"}
	for _, h := range o.AllowTargets {
		if !hostArg.MatchString(h) {
			return "", fmt.Errorf("allow-target %q is not a host name or address", h)
		}
		args = append(args, "-allow-target", h)
	}
	if o.Yes {
		args = append(args, "-yes")
	}
	args = append(args, "/scenario.chaos.yaml")

	name := dnsLabel(s.Metadata.Name)
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": name + "-scenario"},
		"data":       map[string]any{"scenario.chaos.yaml": string(o.Scenario)},
	}
	cmYAML, err := yaml.Marshal(cm)
	if err != nil {
		return "", err
	}
	// Every argument is one of the fixed words above, a host that passed
	// hostArg, or a fixed path, so none holds a character that %q would write
	// in a way YAML reads differently.
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = fmt.Sprintf("%q", a)
	}
	job := fmt.Sprintf(`apiVersion: batch/v1
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
          image: %s
          command: ["litmus-lite"]
          args: [%s]
          volumeMounts:
            - name: scenario
              mountPath: /scenario.chaos.yaml
              subPath: scenario.chaos.yaml
      volumes:
        - name: scenario
          configMap:
            name: %s-scenario
`, name, fmt.Sprintf("%q", o.Image), strings.Join(quoted, ", "), name)
	return string(cmYAML) + "---\n" + job, nil
}
