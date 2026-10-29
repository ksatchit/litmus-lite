package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/litmuschaos/litmus-lite/internal/k8s"
	"github.com/litmuschaos/litmus-lite/internal/safety"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// releaseRepo is where the release archives live. `ci github` downloads from
// it, so it has to be right the day the project moves.
const releaseRepo = "litmuschaos/litmus-lite"

// versionTag is what -version may be: a release tag. It goes into a shell
// command in the generated workflow, so nothing else is accepted.
var versionTag = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)

func cmdCI(args []string) int {
	if len(args) < 1 || args[0] != "github" {
		fmt.Fprintln(os.Stderr, "ci github [-version TAG] [-force]")
		return 2
	}
	fs := flag.NewFlagSet("ci github", flag.ContinueOnError)
	version := fs.String("version", "", "release tag to install (default: the latest release)")
	force := fs.Bool("force", false, "overwrite an existing workflow file")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	tag := strings.TrimSpace(*version)
	if tag != "" {
		if !versionTag.MatchString(tag) {
			fmt.Fprintf(os.Stderr, "-version %q is not a release tag such as v1.2.3\n", tag)
			return 2
		}
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
	}
	dir := filepath.Join(".github", "workflows")
	path := filepath.Join(dir, "litmus-lite.yml")
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "%s exists; pass -force to overwrite it\n", path)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(path, []byte(ciWorkflow(tag)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(path)
	return 0
}

// ciWorkflow is a workflow that needs nothing from this repository's source:
// it installs a release (checked against the published checksums) and
// validates every *.chaos.yaml in the repository it is added to.
func ciWorkflow(tag string) string {
	tagArg := ""
	if tag != "" {
		tagArg = tag + " "
	}
	return `name: litmus-lite
on: [push, pull_request]
permissions:
  contents: read
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install litmus-lite
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release download ` + tagArg + `--repo ` + releaseRepo + ` --pattern 'litmus-lite_*_linux_amd64.tar.gz' --pattern checksums.txt
          sha256sum --check --ignore-missing checksums.txt
          tar -xzf litmus-lite_*_linux_amd64.tar.gz litmus-lite
          mkdir -p "$HOME/.local/bin"
          mv litmus-lite "$HOME/.local/bin/"
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Validate scenarios
        run: |
          litmus-lite version
          git ls-files '*.chaos.yaml' | while IFS= read -r f; do
            litmus-lite validate -strict "$f" || exit 1
          done
      # To run a scenario in CI (it needs a target that is up), add a step like:
      # - run: litmus-lite run chaos/checkout.chaos.yaml -junit chaos.xml
`
}

func cmdExport(args []string) int {
	if len(args) < 1 || args[0] != "job" {
		fmt.Fprintln(os.Stderr, "export job -image IMAGE [-allow-target HOSTS] [-yes] FILE")
		return 2
	}
	fs := flag.NewFlagSet("export job", flag.ContinueOnError)
	image := fs.String("image", "", "container image that has litmus-lite on its PATH (required)")
	allow := fs.String("allow-target", "", "comma-separated extra hosts the Job may reach")
	yes := fs.Bool("yes", false, "pass -yes to the Job (long or destructive runs, Kubernetes service hosts)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "export job -image IMAGE [-allow-target HOSTS] [-yes] FILE")
		return 2
	}
	if strings.TrimSpace(*image) == "" {
		fmt.Fprintln(os.Stderr, "export job: -image is required. The project publishes no image: pass the one you built or mirrored, for example -image registry.example/litmus-lite:1.2.3")
		return 2
	}
	file := fs.Arg(0)
	sc, err := scenario.Load(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	opt, err := safetyFromFlags(*allow, "", *yes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	// The Job runs `litmus-lite run` with these flags and nothing else. If
	// that would be refused, say so now and not as a failed Job.
	if err := safety.Check(sc, opt); err != nil {
		fmt.Fprintf(os.Stderr, "the Job would refuse this scenario: %v\n", err)
		return 1
	}
	out, err := k8s.Manifests(sc, k8s.JobOptions{Image: *image, Scenario: raw, AllowTargets: opt.AllowTargets, Yes: *yes})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Print(out)
	return 0
}
