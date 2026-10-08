package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/audit"
	"github.com/ksatchit/litmus-lite/internal/compare"
	"github.com/ksatchit/litmus-lite/internal/diagnose"
	"github.com/ksatchit/litmus-lite/internal/doctor"
	"github.com/ksatchit/litmus-lite/internal/engine"
	"github.com/ksatchit/litmus-lite/internal/faults"
	"github.com/ksatchit/litmus-lite/internal/hosts"
	"github.com/ksatchit/litmus-lite/internal/hub"
	"github.com/ksatchit/litmus-lite/internal/k8s"
	"github.com/ksatchit/litmus-lite/internal/mcp"
	"github.com/ksatchit/litmus-lite/internal/moveto"
	litmusadapter "github.com/ksatchit/litmus-lite/internal/moveto/litmus"
	"github.com/ksatchit/litmus-lite/internal/report"
	"github.com/ksatchit/litmus-lite/internal/safety"
	"github.com/ksatchit/litmus-lite/internal/scenario"
)

const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	if len(args) < 2 {
		usage()
		return 2
	}
	switch args[1] {
	case "version", "-version", "--version":
		fmt.Println(version)
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	case "new":
		return cmdNew(args[2:])
	case "validate":
		return cmdValidate(args[2:])
	case "run":
		return cmdRun(args[2:])
	case "diagnose":
		return cmdDiagnose(args[2:])
	case "hub":
		return cmdHub(args[2:])
	case "init":
		return cmdInit(args[2:])
	case "doctor":
		return cmdDoctor(args[2:])
	case "compare":
		return cmdCompare(args[2:])
	case "mcp":
		return cmdMCP(args[2:])
	case "move-to":
		return cmdMoveTo(args[2:])
	case "ci":
		return cmdCI(args[2:])
	case "export":
		return cmdExport(args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", args[1])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `litmus-lite %s — chaos testing for agentic developers

Commands:
  new -beside DIR        write a co-located *.chaos.yaml
  validate FILE          schema + safety
  run FILE               execute a scenario
  diagnose FILE.json     explain a report
  hub list|search|show|import
  init                   register MCP + Cursor rules
  doctor                 check CLI / hosts / target
  compare BASE CAND      phase regression
  mcp serve | mcp eval
  move-to                emit IR or Litmus 4.0 experiment YAML (-adapter ir|litmus)
  ci github              write a GitHub Actions workflow
  export job FILE        Kubernetes Job YAML (no CRD)

`, version)
}

func outputFlag(fs *flag.FlagSet) *string {
	return fs.String("output", "text", "text|json")
}

func cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	beside := fs.String("beside", ".", "directory to write into")
	name := fs.String("name", "http-latency", "file stem")
	kind := fs.String("kind", "http.latency", "hub id to import")
	out := outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path, err := hub.Import(*kind, *beside, *name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *out == "json" {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(map[string]string{"path": path})
	} else {
		fmt.Println(path)
	}
	return 0
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	out := outputFlag(fs)
	allow := fs.String("allow-target", "", "comma-separated extra hosts")
	yes := fs.Bool("yes", false, "allow long/destructive runs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "validate FILE")
		return 2
	}
	sc, err := scenario.Load(fs.Arg(0))
	if err != nil {
		printErr(*out, err)
		return 1
	}
	for _, f := range sc.Faults {
		inj, err := faults.Lookup(f.Kind)
		if err != nil {
			printErr(*out, err)
			return 1
		}
		if err := inj.Validate(f); err != nil {
			printErr(*out, err)
			return 1
		}
	}
	opt := safety.Options{Yes: *yes}
	if *allow != "" {
		opt.AllowTargets = strings.Split(*allow, ",")
	}
	if err := safety.Check(sc, opt); err != nil {
		printErr(*out, err)
		return 1
	}
	if *out == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"valid": true, "name": sc.Metadata.Name})
	} else {
		fmt.Println("valid")
	}
	return 0
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	outFmt := outputFlag(fs)
	outPath := fs.String("out", "report.json", "JSON report path")
	htmlPath := fs.String("html", "report.html", "HTML report path")
	allow := fs.String("allow-target", "", "comma-separated extra hosts")
	yes := fs.Bool("yes", false, "allow long/destructive runs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "run FILE")
		return 2
	}
	file := fs.Arg(0)
	sc, err := scenario.Load(file)
	if err != nil {
		printErr(*outFmt, err)
		return 1
	}
	opt := safety.Options{Yes: *yes}
	if *allow != "" {
		opt.AllowTargets = strings.Split(*allow, ",")
	}
	if err := safety.Check(sc, opt); err != nil {
		printErr(*outFmt, err)
		return 2
	}
	res, err := engine.Run(context.Background(), sc, engine.Options{ScenarioDir: filepath.Dir(file)})
	if err != nil {
		printErr(*outFmt, err)
		audit.Append(audit.Line{Command: "run", File: file})
		return 1
	}
	_ = report.WriteJSON(*outPath, res)
	_ = report.WriteHTML(*htmlPath, res)
	passed := res.Passed
	audit.Append(audit.Line{Command: "run", File: file, Passed: &passed})
	if *outFmt == "json" {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("%s passed=%v hypotheses=%d report=%s html=%s\n", res.Name, res.Passed, len(res.Hypotheses), *outPath, *htmlPath)
		if len(res.Warnings) > 0 {
			fmt.Println(strings.Join(res.Warnings, "\n"))
		}
	}
	if !res.Passed {
		return 3
	}
	return 0
}

func cmdDiagnose(args []string) int {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	out := outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		return 2
	}
	res, err := report.LoadJSON(fs.Arg(0))
	if err != nil {
		printErr(*out, err)
		return 1
	}
	text := diagnose.Text(res)
	if *out == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"text": text})
	} else {
		fmt.Print(text)
	}
	return 0
}

func cmdHub(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "hub list|search|show|import")
		return 2
	}
	sub := args[0]
	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	out := outputFlag(fs)
	beside := fs.String("beside", ".", "import directory")
	name := fs.String("name", "", "import file stem")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	cwd, _ := os.Getwd()
	cat := hub.Open(cwd)
	switch sub {
	case "list":
		items, err := cat.List()
		if err != nil {
			printErr(*out, err)
			return 1
		}
		if *out == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(items)
		} else {
			for _, it := range items {
				fmt.Printf("%s\t%s\t%s\t%s\n", it.ID, it.Kind, it.Title, it.Source)
			}
		}
	case "search":
		q := fs.Arg(0)
		items, err := cat.Search(q)
		if err != nil {
			printErr(*out, err)
			return 1
		}
		if *out == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(items)
		} else {
			for _, it := range items {
				fmt.Println(it.ID)
			}
		}
	case "show":
		id := fs.Arg(0)
		it, err := cat.Get(id)
		if err != nil {
			printErr(*out, err)
			return 1
		}
		if *out == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(it)
		} else {
			fmt.Printf("%s\n%s\n%s\n", it.ID, it.Summary, it.Template)
		}
	case "import":
		id := fs.Arg(0)
		path, err := cat.Import(id, *beside, *name)
		if err != nil {
			printErr(*out, err)
			return 1
		}
		if *out == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"path": path})
		} else {
			fmt.Println(path)
		}
	default:
		return 2
	}
	return 0
}

func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	editor := fs.String("editor", "cursor", "cursor|claude-code")
	_ = outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	if strings.Contains(*editor, "cursor") {
		if err := hosts.WriteCursor(cwd, bin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	_ = hosts.WriteProjectMCP(cwd, bin)
	fmt.Println("initialized litmus-lite MCP in", cwd)
	return 0
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	target := fs.String("target", "", "optional HTTP URL")
	fix := fs.Bool("fix", false, "write missing Cursor MCP/rules files")
	out := outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *fix {
		return cmdInit([]string{"-editor", "cursor"})
	}
	checks := doctor.Run(*target)
	if *out == "json" {
		fmt.Println(string(doctor.JSON(checks)))
	} else {
		for _, c := range checks {
			fmt.Printf("%s\t%s\t%s\n", c.Name, c.Level, c.Message)
		}
	}
	if doctor.Failed(checks) {
		return 1
	}
	return 0
}

func cmdCompare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	out := outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		return 2
	}
	a, err := report.LoadJSON(fs.Arg(0))
	if err != nil {
		printErr(*out, err)
		return 1
	}
	b, err := report.LoadJSON(fs.Arg(1))
	if err != nil {
		printErr(*out, err)
		return 1
	}
	rows, failed := compare.Phases(a, b)
	if *out == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(rows)
	} else {
		fmt.Print(compare.Text(rows, failed))
	}
	if failed {
		return 3
	}
	return 0
}

func cmdMCP(args []string) int {
	if len(args) < 1 {
		return 2
	}
	switch args[0] {
	case "serve":
		if err := mcp.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "eval":
		p, f, text := mcp.Eval()
		fmt.Print(text)
		fmt.Printf("%d passed, %d failed\n", p, f)
		if f > 0 {
			return 1
		}
		return 0
	default:
		return 2
	}
}

func cmdMoveTo(args []string) int {
	fs := flag.NewFlagSet("move-to", flag.ContinueOnError)
	adapter := fs.String("adapter", "ir", "ir|litmus|harness")
	out := outputFlag(fs)
	outPath := fs.String("out", "", "optional file to write (stdout always)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "move-to [-adapter ir|litmus|harness] [-output text|json] [-out FILE] FILE")
		return 2
	}
	sc, err := scenario.Load(fs.Arg(0))
	if err != nil {
		printErr(*out, err)
		return 1
	}
	ex := moveto.FromScenario(sc)
	var body []byte
	switch *adapter {
	case "ir", "":
		body = moveto.JSON(ex)
	case "litmus":
		doc := litmusadapter.FromIR(ex)
		if *out == "json" {
			body = litmusadapter.JSON(doc)
		} else {
			body = litmusadapter.YAML(doc)
		}
	case "harness":
		fmt.Print(string(moveto.JSON(ex)))
		if err := moveto.Push("harness", ex); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	default:
		printErr(*out, fmt.Errorf("unknown adapter %q", *adapter))
		return 1
	}
	fmt.Print(string(body))
	if *outPath != "" {
		if err := os.WriteFile(*outPath, body, 0o644); err != nil {
			printErr(*out, err)
			return 1
		}
	}
	return 0
}

func cmdCI(args []string) int {
	if len(args) < 1 || args[0] != "github" {
		fmt.Fprintln(os.Stderr, "ci github")
		return 2
	}
	dir := filepath.Join(".github", "workflows")
	_ = os.MkdirAll(dir, 0o755)
	body := `name: litmus-lite
on: [push, pull_request]
jobs:
  chaos:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go build -o litmus-lite ./cmd/litmus-lite
      - run: ./litmus-lite validate examples/launchpad/ignite.chaos.yaml
`
	path := filepath.Join(dir, "litmus-lite.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(path)
	return 0
}

func cmdExport(args []string) int {
	if len(args) < 2 || args[0] != "job" {
		fmt.Fprintln(os.Stderr, "export job FILE")
		return 2
	}
	sc, err := scenario.Load(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(k8s.JobYAML(sc))
	return 0
}

func printErr(out string, err error) {
	if out == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
}
