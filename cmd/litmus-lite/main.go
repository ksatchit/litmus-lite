package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/litmuschaos/litmus-lite/internal/audit"
	"github.com/litmuschaos/litmus-lite/internal/compare"
	"github.com/litmuschaos/litmus-lite/internal/diagnose"
	"github.com/litmuschaos/litmus-lite/internal/doctor"
	"github.com/litmuschaos/litmus-lite/internal/engine"
	"github.com/litmuschaos/litmus-lite/internal/faults"
	"github.com/litmuschaos/litmus-lite/internal/hosts"
	"github.com/litmuschaos/litmus-lite/internal/hub"
	"github.com/litmuschaos/litmus-lite/internal/mcp"
	"github.com/litmuschaos/litmus-lite/internal/moveto"
	litmusadapter "github.com/litmuschaos/litmus-lite/internal/moveto/litmus"
	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/safety"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

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
	case "stream":
		return cmdStream(args[2:])
	case "serve":
		return cmdServe(args[2:])
	case "live":
		return cmdLive(args[2:])
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
  validate FILE          schema + safety (-strict: unknown keys are errors)
  run FILE               execute a scenario (-allow-target, -allow-pid, -yes, -strict, -stream-file|-push|-pull, -run-id, -label)
                         optional live view in your browser once a service is configured (-live-url, see "live")
  diagnose FILE.json     explain a report
  hub list|search|show|import
  init                   register MCP + Cursor rules (-env KEY=VALUE, repeatable, for the MCP server)
  doctor                 check CLI / hosts / target (-file FILE, -stream URL|ADDR)
  compare BASE CAND      phase regression
  mcp serve | mcp eval
  move-to                emit IR or Litmus 4.0 experiment YAML (-adapter ir|litmus)
  ci github              write a GitHub Actions workflow that installs a release and validates *.chaos.yaml (-version TAG, -force)
  export job FILE        ConfigMap + Kubernetes Job for a scenario (-image IMAGE is required; -allow-target, -yes). Experimental
  stream                 from-report | replay | validate | receive (run event streams)
  serve                  read-only data source over a directory of run files (-dir, -listen)
  live                   optional live view of a run: "live enable URL" | "live disable" | print or open the page (-open) | "live reset"

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
	allowPID := fs.String("allow-pid", "", "comma-separated PIDs process faults may signal")
	yes := fs.Bool("yes", false, "allow long/destructive runs")
	strict := fs.Bool("strict", false, strictUsage)
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
	opt, err := safetyFromFlags(*allow, *allowPID, *yes)
	if err != nil {
		printErr(*out, err)
		return 2
	}
	if err := safety.Check(sc, opt); err != nil {
		printErr(*out, err)
		return 1
	}
	warns := lintScenario(sc)
	if err := strictError(*strict, warns); err != nil {
		printErr(*out, err)
		return 1
	}
	cmds := commandsToRun(sc)
	if *out == "json" {
		doc := map[string]any{"valid": true, "name": sc.Metadata.Name}
		if len(warns) > 0 {
			doc["warnings"] = warns
		}
		if len(cmds) > 0 {
			doc["runsCommands"] = cmds
		}
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "warning: "+w)
		}
		printCommandNotice(os.Stderr, cmds)
		fmt.Println("valid")
	}
	return 0
}

const strictUsage = "treat warnings (a key or param the scenario format does not read) as errors"

// lintScenario lists what is wrong with a scenario that does not stop it from
// running: keys nothing reads, which would otherwise do nothing and say
// nothing.
func lintScenario(sc *scenario.Scenario) []string {
	out := append([]string(nil), sc.Warnings...)
	out = append(out, faults.Lint(sc)...)
	if len(sc.Hypotheses) == 0 {
		out = append(out, "no hypotheses: this run cannot fail, whatever the probes see, so a green result says nothing. "+
			"Add at least one, for example error_rate or recovery (see docs/hypotheses.md)")
	}
	for _, p := range append(append([]scenario.Probe(nil), sc.SteadyState...), sc.Probes...) {
		out = append(out, probe.Lint(p)...)
	}
	return out
}

// strictError is the error -strict makes of the warnings, or nil.
func strictError(strict bool, warns []string) error {
	if !strict || len(warns) == 0 {
		return nil
	}
	return fmt.Errorf("-strict: %d warning(s): %s", len(warns), strings.Join(warns, "; "))
}

// htmlPathFor derives the HTML report path from the JSON report path by
// swapping the extension, so "foo.json" pairs with "foo.html".
func htmlPathFor(jsonPath string) string {
	p := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".html"
	if p == jsonPath {
		p += ".html"
	}
	return p
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	outFmt := outputFlag(fs)
	outPath := fs.String("out", "report.json", "JSON report path")
	htmlFlag := fs.String("html", "", "HTML report path (default: -out path with .html extension)")
	junitPath := fs.String("junit", "", "optional JUnit XML path")
	allow := fs.String("allow-target", "", "comma-separated extra hosts")
	allowPID := fs.String("allow-pid", "", "comma-separated PIDs process faults may signal")
	yes := fs.Bool("yes", false, "allow long/destructive runs")
	strict := fs.Bool("strict", false, strictUsage)
	var sflags runStream
	sflags.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "run FILE")
		return 2
	}
	file := fs.Arg(0)
	htmlPath := *htmlFlag
	if htmlPath == "" {
		htmlPath = htmlPathFor(*outPath)
	}
	sc, err := scenario.Load(file)
	if err != nil {
		printErr(*outFmt, err)
		return 1
	}
	opt, err := safetyFromFlags(*allow, *allowPID, *yes)
	if err != nil {
		printErr(*outFmt, err)
		return 2
	}
	if err := safety.Check(sc, opt); err != nil {
		printErr(*outFmt, err)
		return 2
	}
	startWarns := lintScenario(sc)
	if err := strictError(*strict, startWarns); err != nil {
		printErr(*outFmt, err)
		return 1
	}
	// Shown now, not only after a long run, and kept in the report.
	shown := map[string]bool{}
	for _, w := range startWarns {
		fmt.Fprintln(os.Stderr, "warning: "+w)
		shown[w] = true
	}
	printCommandNotice(os.Stderr, commandsToRun(sc))
	ctx := safety.WithAllowPIDs(context.Background(), opt.AllowPIDs)
	eopt := engine.Options{ScenarioDir: filepath.Dir(file), Warnings: startWarns}
	st, err := sflags.setup()
	if err != nil {
		printErr(*outFmt, err)
		return 2
	}
	auditLine := audit.Line{Command: "run", File: file}
	// Ctrl-C and SIGTERM always stop the run the same way: the engine rolls
	// every fault back (resumes a paused process, unpauses a container, removes
	// a fill file) and no report is written. Without this the default action of
	// the signal ends the process at once and leaves the fault in place, which
	// for a chaos tool is the worst way to stop. A second signal, while the
	// rollback is running, is not caught, so it ends the process.
	var stop context.CancelFunc
	ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); stop() }()
	if st != nil {
		auditLine.Stream = st.audit
		eopt.Emitter = st.rec
		eopt.RunID = st.rec.RunID()
		defer func() { st.close(ctx) }()
	}
	res, err := engine.Run(ctx, sc, eopt)
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "interrupted: faults rolled back, no report written")
		audit.Append(auditLine)
		return 130
	}
	if err != nil {
		printErr(*outFmt, err)
		audit.Append(auditLine)
		return 1
	}
	if st != nil {
		res.Warnings = append(res.Warnings, st.warnings...)
		res.Warnings = append(res.Warnings, st.rec.Warnings()...)
	}
	_ = report.WriteJSON(*outPath, res)
	_ = report.WriteHTML(htmlPath, res)
	if *junitPath != "" {
		_ = report.WriteJUnit(*junitPath, res)
	}
	passed := res.Passed
	auditLine.Passed = &passed
	audit.Append(auditLine)
	if *outFmt == "json" {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("%s passed=%v hypotheses=%d report=%s html=%s\n", res.Name, res.Passed, len(res.Hypotheses), *outPath, htmlPath)
		var rest []string
		for _, w := range res.Warnings {
			if !shown[w] {
				rest = append(rest, w)
			}
		}
		if len(rest) > 0 {
			fmt.Println(strings.Join(rest, "\n"))
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
	var envs pushFlag
	fs.Var(&envs, "env", "set KEY=VALUE in the MCP server's environment (repeatable; KEY= removes it). For example LITMUS_LITE_LIVE_URL=https://live.example, or LITMUS_LITE_CONFIG_DIR=DIR to keep litmus-lite's config elsewhere")
	_ = outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	env, err := parseInitEnv(envs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "init:", err)
		return 2
	}
	bin, err := executablePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	if strings.Contains(*editor, "cursor") {
		if err := hosts.WriteCursor(cwd, bin, env); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	_ = hosts.WriteProjectMCP(cwd, bin, env)
	fmt.Println("initialized litmus-lite MCP in", cwd)
	return 0
}

// executable is os.Executable; tests replace it.
var executable = os.Executable

// executablePath is the path of this binary as written into the MCP entry. The
// system may hand it back as it was typed (/tmp/x/proj/../litmus-lite), so it
// is cleaned. Links are not resolved: a package manager's stable link must
// stay what the editor runs.
func executablePath() (string, error) {
	bin, err := executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(bin), nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// secretName matches variable names that hold a credential.
var secretName = regexp.MustCompile(`(?i)(token|secret|password|passwd|credential|api_?key)`)

// parseInitEnv checks the -env values of `init`. The MCP file is written into
// the project, where it is usually committed, so a credential must never go
// there: the name decides (LITMUS_LITE_STREAM_TOKEN and the like), and so does
// a URL with a user name or password in it. The values of the two path
// variables are made absolute, because an editor starts the server from a
// directory of its own choosing. An empty value means "remove".
func parseInitEnv(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := map[string]string{}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !envName.MatchString(k) {
			return nil, fmt.Errorf("-env %q: want KEY=VALUE, with KEY a variable name", kv)
		}
		if secretName.MatchString(k) {
			return nil, fmt.Errorf("-env %s: the MCP file is written into the project and is often committed, so it must not hold a credential. Set %s in the environment of the editor instead", k, k)
		}
		if strings.Contains(v, "://") {
			if u, err := url.Parse(v); err == nil && u.User != nil {
				return nil, fmt.Errorf("-env %s: the value has credentials in it; the MCP file must not hold them", k)
			}
		}
		if v != "" && (k == "LITMUS_LITE_CONFIG_DIR" || k == "LITMUS_LITE_HOME") {
			abs, err := absDir(v)
			if err != nil {
				return nil, err
			}
			v = abs
		}
		env[k] = v
	}
	return env, nil
}

// absDir makes a directory argument absolute, expanding a leading ~ the shell
// did not.
func absDir(d string) (string, error) {
	if d == "~" || strings.HasPrefix(d, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, strings.TrimPrefix(d, "~"))
	}
	return filepath.Abs(d)
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	target := fs.String("target", "", "optional HTTP URL")
	file := fs.String("file", "", "optional scenario; check its load tool")
	streamDest := fs.String("stream", "", "check a streaming destination: an ingest URL (accepts a test batch) or a listen address (can be bound)")
	streamInsecure := fs.Bool("stream-insecure", false, "with -stream: allow http:// to a non-loopback host")
	fix := fs.Bool("fix", false, "write missing Cursor MCP/rules files")
	out := outputFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *fix {
		return cmdInit([]string{"-editor", "cursor"})
	}
	checks := doctor.Run(*target, *file)
	if *streamDest != "" {
		checks = append(checks, doctor.Stream(*streamDest, os.Getenv("LITMUS_LITE_STREAM_TOKEN"), *streamInsecure))
	}
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
		mcp.Version = version
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
	adapter := fs.String("adapter", "ir", "ir|litmus")
	out := outputFlag(fs)
	outPath := fs.String("out", "", "optional file to write (stdout always)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "move-to [-adapter ir|litmus] [-output text|json] [-out FILE] FILE")
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
	default:
		printErr(*out, fmt.Errorf("unknown adapter %q (want ir or litmus)", *adapter))
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

func safetyFromFlags(allow, allowPID string, yes bool) (safety.Options, error) {
	pids, err := safety.ParsePIDs(allowPID)
	if err != nil {
		return safety.Options{}, err
	}
	opt := safety.Options{Yes: yes, AllowPIDs: pids}
	if allow != "" {
		for _, h := range strings.Split(allow, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				opt.AllowTargets = append(opt.AllowTargets, h)
			}
		}
	}
	return opt, nil
}

func printErr(out string, err error) {
	if out == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
}
