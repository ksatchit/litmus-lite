package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tool is one MCP tool. Every entry must map to a CLI the user can type
// (get_results reads a file the CLI wrote; it is not an agent-only path).
//
// Desc, Props and Required are what tools/list advertises. They exist so an
// agent can call a tool correctly on the first try instead of probing for
// argument names. TestToolSchemasMatchCLIArgs keeps them in step with cliArgs.
type tool struct {
	Name     string
	CLI      string // litmus-lite subcommand, empty when ReadFile
	ReadFile bool
	Desc     string
	Props    map[string]any // argument name -> JSON Schema property
	Required []string
}

func strArg(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func boolArg(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// allowEnv is the environment variable a person sets in the MCP server's env
// (the "env" of its entry in mcp.json) to let tool calls forward safety flags.
// Its value is a comma-separated list of the flags: yes, allow-target,
// allow-pid. Without it a tool call that carries one of them is refused.
//
// The flags are the person's consent to a destructive or out-of-scope action.
// If the agent could supply them, "refused unless -yes" would mean "refused
// unless the agent says yes", and a scenario file the agent wrote itself could
// kill a process the person never named. Setting this is the person saying,
// once and in a place the agent does not write, that their agent may.
const allowEnv = "LITMUS_LITE_MCP_ALLOW"

// safetyFlagNames are the flags allowEnv can enable.
var safetyFlagNames = []string{"yes", "allow-target", "allow-pid"}

// allowedFlags returns the safety flags the person enabled for this server.
func allowedFlags() map[string]bool {
	on := map[string]bool{}
	for _, f := range strings.Split(os.Getenv(allowEnv), ",") {
		on[strings.ToLower(strings.TrimSpace(f))] = true
	}
	return on
}

// checkSafetyArgs refuses a tool call that carries a safety flag nobody
// enabled. The refusal names the flag and how a person enables it, and it
// changes nothing: no CLI is run.
func checkSafetyArgs(args map[string]any) error {
	on := allowedFlags()
	for _, f := range safetyFlagNames {
		supplied := false
		if f == "yes" {
			supplied = flagOn(args, f)
		} else if v, ok := args[f]; ok && v != nil && fmt.Sprint(v) != "" {
			supplied = true
		}
		if supplied && !on[f] {
			return fmt.Errorf("the %q argument is refused: it confirms a destructive or out-of-scope action, and a tool call cannot confirm that for the person. "+
				"If you are the person and accept that your agent may, add %s=%s to the env of this MCP server (litmus-lite init -env %s=%s) and restart it; "+
				"or run the same command yourself with -%s",
				f, allowEnv, f, allowEnv, f, f)
		}
	}
	return nil
}

// safetyProps are the flags validate_scenario and run_test forward to the CLI.
// They are forwarded only when a person enabled them (allowEnv).
func safetyProps(extra map[string]any) map[string]any {
	const gate = " Refused unless the person who set up this MCP server enabled it with " + allowEnv + "; if it is refused, tell them, or give them the CLI command to run themselves."
	p := map[string]any{
		"yes":          boolArg("Forward -yes: confirm a destructive fault (process.kill, disk.fill). Not needed for latency, status, timeout or pause faults." + gate),
		"allow-target": strArg("Forward -allow-target: comma-separated extra non-loopback hosts the scenario may touch." + gate),
		"allow-pid":    strArg("Forward -allow-pid: the one PID process.pause or process.kill may signal, for example \"4242\". Required for those faults; no other PID is signalled." + gate),
		"strict":       boolArg("Forward -strict: fail on a key or param the scenario format does not read (a typo such as \"dealy\"), instead of warning. Recommended for a scenario you just wrote."),
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func allTools() []tool {
	return []tool{
		{
			Name: "create_scenario", CLI: "new",
			Desc: "litmus-lite new: write a co-located http.latency scenario template (<name>.chaos.yaml) into a directory. Returns {\"path\": ...}. Edit the file afterwards to fit the test.",
			Props: map[string]any{
				"beside": strArg("Directory to write the scenario into, usually next to the service code (default \".\")."),
				"name":   strArg("File stem without .chaos.yaml (default \"scenario\")."),
			},
		},
		{
			Name: "validate_scenario", CLI: "validate",
			Desc:     "litmus-lite validate: check a scenario file and the safety rules without running it. Run this before run_test.",
			Props:    safetyProps(map[string]any{"file": strArg("Path to the *.chaos.yaml file.")}),
			Required: []string{"file"},
		},
		{
			Name: "run_test", CLI: "run",
			Desc: "litmus-lite run: execute a scenario (steady state, inject, recover) and write a JSON report and an overlay HTML report. The first text item of the result is the report as JSON. If a live-view service is configured (LITMUS_LITE_LIVE_URL in the server's environment, or kept by `litmus-lite live enable`), the run is also shown live in the user's browser: the page opens by itself and its URL is in a second text item, \"live view: URL\". Tell the user that URL. Pass no-live if the user does not want the live view, or no-open to stream without opening a browser. With no service configured nothing is shown or sent.",
			Props: safetyProps(map[string]any{
				"file":        strArg("Path to the *.chaos.yaml file."),
				"out":         strArg("Where to write the JSON report, for example report.json. Pass this path to diagnose_failure or get_results."),
				"stream_file": strArg("Optional. Forward -stream-file: write the run as NDJSON events to this path, or \"auto\" for .litmus-lite/runs/<run id>.ndjson."),
				"stream_push": strArg("Optional. Forward -stream-push: ingest URL(s) to POST events to, comma-separated. https unless the host is loopback. The token comes from LITMUS_LITE_STREAM_TOKEN, never from here."),
				"stream_pull": strArg("Optional. Forward -stream-pull: address to serve the run as SSE on while it runs, for example 127.0.0.1:7070. The tool call returns after the run and a short linger."),
				"run_id":      strArg("Optional. Forward -run-id: the id used on stream events (A-Z a-z 0-9 . _ -, up to 64). Give a generator the same id to join their streams."),
				"labels":      strArg("Optional. Forward -label: comma-separated key=value labels for every stream event, for example \"service=checkout,env=ci\"."),
				"live":        boolArg("Optional. Forward -live: also show the run in the live view when a stream_* destination is given. It fails if no live-view service is configured. When one is configured and no stream_* destination is given, the live view is used without this."),
				"no-live":     boolArg("Optional. Forward -no-live: do not stream to the live view or open a browser."),
				"no-open":     boolArg("Optional. Forward -no-open: stream to the live view but do not open the browser."),
			}),
			Required: []string{"file"},
		},
		{
			Name: "get_results", ReadFile: true,
			Desc:  "Read a report JSON file that run_test wrote.",
			Props: map[string]any{"file": strArg("Path to the report JSON (default \"report.json\").")},
		},
		{
			Name: "diagnose_failure", CLI: "diagnose",
			Desc:     "litmus-lite diagnose: explain a report in terms of its hypotheses, phases and likely cause.",
			Props:    map[string]any{"file": strArg("Path to the report JSON written by run_test.")},
			Required: []string{"file"},
		},
		{
			Name: "list_faults", CLI: "hub",
			Desc: "litmus-lite hub list: list the fault kinds in the hub catalog. Takes no arguments.",
		},
		{
			Name: "hub_import", CLI: "hub",
			Desc: "litmus-lite hub import: write a hub scenario template (<name>.chaos.yaml) into a directory. Returns {\"path\": ...}. An existing file with the same name is overwritten, so pass name when importing the same id twice.",
			Props: map[string]any{
				"id":     strArg("Hub id from list_faults, for example http.latency, http.status-inject, http.timeout or process.pause."),
				"beside": strArg("Directory to write into, usually next to the service code (default \".\")."),
				"name":   strArg("File stem without .chaos.yaml (default: the id with dots replaced by dashes)."),
			},
			Required: []string{"id"},
		},
		{
			Name: "compare_reports", CLI: "compare",
			Desc: "litmus-lite compare: compare two report JSON files phase by phase (steady, inject, recover).",
			Props: map[string]any{
				"baseline":  strArg("Path to the baseline report JSON."),
				"candidate": strArg("Path to the candidate report JSON."),
			},
			Required: []string{"baseline", "candidate"},
		},
		{
			Name: "doctor", CLI: "doctor",
			Desc:  "litmus-lite doctor: check the local environment (Cursor MCP setup, optional VegaLoad and Prometheus).",
			Props: map[string]any{"file": strArg("Optional scenario file; also checks its load tool.")},
		},
	}
}

func toolByName(name string) (tool, bool) {
	for _, t := range allTools() {
		if t.Name == name {
			return t, true
		}
	}
	return tool{}, false
}

func toolsHaveCLI() error {
	for _, t := range allTools() {
		if t.CLI == "" && !t.ReadFile {
			return fmt.Errorf("tool %q has no CLI equivalent", t.Name)
		}
	}
	return nil
}

func toolDefs() []map[string]any {
	var out []map[string]any
	for _, t := range allTools() {
		props := t.Props
		if props == nil {
			props = map[string]any{}
		}
		schema := map[string]any{
			"type":       "object",
			"properties": props,
			// Unknown keys stay accepted (and ignored), as before this schema existed.
			"additionalProperties": true,
		}
		if len(t.Required) > 0 {
			schema["required"] = t.Required
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Desc,
			"inputSchema": schema,
		})
	}
	return out
}

// cliArgs maps a tool call to the argv of the CLI command it wraps. Values the
// caller chooses and that go in a positional place (a file, an id) come after
// "--", so one that starts with "-" is a file name and never a flag: a tool
// call cannot add a flag that the tool does not declare (for example
// -stream-insecure, which docs/streaming.md says no tool call can set).
func cliArgs(name string, args map[string]any) ([]string, error) {
	str := func(k, def string) string {
		if args == nil {
			return def
		}
		if v, ok := args[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	safetyFlags := func(argv []string) []string {
		if flagOn(args, "strict") {
			argv = append(argv, "-strict")
		}
		if flagOn(args, "yes") {
			argv = append(argv, "-yes")
		}
		if v := str("allow-target", ""); v != "" {
			argv = append(argv, "-allow-target", v)
		}
		if v := str("allow-pid", ""); v != "" {
			argv = append(argv, "-allow-pid", v)
		}
		return argv
	}
	// Streaming arguments are passed to the same flags `run` has. There is no
	// MCP-only streaming path, and nothing here can relax the CLI's rules.
	// Without live, no-live or no-open, `run` does what it does from a shell:
	// the live view is used only when a service is configured and no other
	// destination is given.
	streamFlags := func(argv []string) []string {
		for _, f := range []string{"live", "no-live", "no-open"} {
			if flagOn(args, f) {
				argv = append(argv, "-"+f)
			}
		}
		if v := str("stream_file", ""); v != "" {
			argv = append(argv, "-stream-file", v)
		}
		for _, u := range strings.Split(str("stream_push", ""), ",") {
			if u = strings.TrimSpace(u); u != "" {
				argv = append(argv, "-stream-push", u)
			}
		}
		if v := str("stream_pull", ""); v != "" {
			argv = append(argv, "-stream-pull", v)
		}
		if v := str("run_id", ""); v != "" {
			argv = append(argv, "-run-id", v)
		}
		for _, l := range strings.Split(str("labels", ""), ",") {
			if l = strings.TrimSpace(l); l != "" {
				argv = append(argv, "-label", l)
			}
		}
		return argv
	}
	switch name {
	case "create_scenario":
		return []string{"new", "-beside", str("beside", "."), "-name", str("name", "scenario"), "-output", "json"}, nil
	case "validate_scenario":
		argv := safetyFlags([]string{"validate", "-output", "json"})
		return append(argv, "--", str("file", "")), nil
	case "run_test":
		argv := []string{"run", "-output", "json"}
		if o := str("out", ""); o != "" {
			argv = append(argv, "-out", o)
		}
		argv = safetyFlags(argv)
		argv = streamFlags(argv)
		return append(argv, "--", str("file", "")), nil
	case "diagnose_failure":
		return []string{"diagnose", "-output", "json", "--", str("file", "")}, nil
	case "list_faults":
		return []string{"hub", "list", "-output", "json"}, nil
	case "hub_import":
		argv := []string{"hub", "import", "-beside", str("beside", "."), "-output", "json"}
		if n := str("name", ""); n != "" {
			argv = append(argv, "-name", n)
		}
		return append(argv, "--", str("id", "")), nil
	case "compare_reports":
		return []string{"compare", "-output", "json", "--", str("baseline", ""), str("candidate", "")}, nil
	case "doctor":
		argv := []string{"doctor", "-output", "json"}
		if f := str("file", ""); f != "" {
			argv = append(argv, "-file", f)
		}
		return argv, nil
	default:
		return nil, fmt.Errorf("tool %q has no CLI equivalent", name)
	}
}

func flagOn(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return fmt.Sprint(t) == "true"
	}
}

// toolResult is what a tool call produced. Text is the CLI's stdout, which is
// JSON for every tool that has an -output flag. Notes is what it wrote to
// stderr: warnings, and the "live view: URL" line of `run`.
type toolResult struct {
	Text  string
	Notes string
}

func callTool(name string, args map[string]any) (string, error) {
	r, err := runTool(name, args)
	return r.Text, err
}

// runTool runs one tool. stdout and stderr are kept apart so that a note on
// stderr can never make the JSON on stdout unparseable.
func runTool(name string, args map[string]any) (toolResult, error) {
	t, ok := toolByName(name)
	if !ok {
		return toolResult{}, fmt.Errorf("unknown tool %s", name)
	}
	if t.CLI == "" && !t.ReadFile {
		return toolResult{}, fmt.Errorf("tool %q has no CLI equivalent", name)
	}
	if err := checkSafetyArgs(args); err != nil {
		return toolResult{}, err
	}
	str := func(k, def string) string {
		if args == nil {
			return def
		}
		if v, ok := args[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	if t.ReadFile {
		text, err := readReport(str("file", "report.json"))
		return toolResult{Text: text}, err
	}
	argv, err := cliArgs(name, args)
	if err != nil {
		return toolResult{}, err
	}
	bin, err := resolveCLI()
	if err != nil {
		return toolResult{}, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, argv...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Exit code 3 is a finished run or comparison whose verdict is "not
		// passed". That is a result, not a failure of the tool: the JSON says
		// passed:false and the notes (including the live view link) still matter,
		// so the agent gets both exactly as for a pass. Any other exit code is an
		// error, as before.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == exitNotPassed {
			return toolResult{Text: stdout.String(), Notes: strings.TrimSpace(stderr.String())}, nil
		}
		both := strings.TrimSpace(stdout.String() + stderr.String())
		return toolResult{Text: both}, fmt.Errorf("%s: %w", both, err)
	}
	return toolResult{Text: stdout.String(), Notes: strings.TrimSpace(stderr.String())}, nil
}

// exitNotPassed is the CLI exit code of a run or comparison that finished and
// did not pass (a failed hypothesis, a regression).
const exitNotPassed = 3

// maxReportBytes bounds what get_results reads. A report of a long run with
// every probe sample is a few MB; nothing legitimate comes near this.
const maxReportBytes = 64 << 20

// readReport returns the text of a report file that `run` wrote, and nothing
// else. get_results is the one tool with no CLI command behind it, so it must
// not be a way to read any file the server can: a path that is not a litmus-lite
// report (a key, a config file, any other JSON) is refused, and the refusal
// does not quote what the file held.
func readReport(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > maxReportBytes {
		return "", fmt.Errorf("%s is not a litmus-lite report", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var probe struct {
		Name   *string         `json:"name"`
		Passed *bool           `json:"passed"`
		Phases json.RawMessage `json:"phases"`
	}
	if json.Unmarshal(b, &probe) != nil || probe.Name == nil || probe.Passed == nil || len(probe.Phases) == 0 {
		return "", fmt.Errorf("%s is not a litmus-lite report", path)
	}
	return string(b), nil
}
