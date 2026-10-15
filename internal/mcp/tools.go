package mcp

import (
	"bytes"
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

// safetyProps are the flags validate_scenario and run_test forward to the CLI.
func safetyProps(extra map[string]any) map[string]any {
	p := map[string]any{
		"yes":          boolArg("Forward -yes: confirm a destructive fault (process.kill, disk.fill). Not needed for latency, status, timeout or pause faults."),
		"allow-target": strArg("Forward -allow-target: comma-separated extra non-loopback hosts the scenario may touch."),
		"allow-pid":    strArg("Forward -allow-pid: the one PID process.pause or process.kill may signal, for example \"4242\". Required for those faults; no other PID is signalled."),
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
			Desc: "litmus-lite run: execute a scenario (steady state, inject, recover) and write a JSON report and an overlay HTML report. The first text item of the result is the report as JSON. By default the run is also shown live in the user's browser (the live view): the page opens by itself and its URL is in a second text item, \"live view: URL\". Tell the user that URL. Pass no-live only if the user does not want the live view, or no-open to stream without opening a browser.",
			Props: safetyProps(map[string]any{
				"file":        strArg("Path to the *.chaos.yaml file."),
				"out":         strArg("Where to write the JSON report, for example report.json. Pass this path to diagnose_failure or get_results."),
				"stream_file": strArg("Optional. Forward -stream-file: write the run as NDJSON events to this path, or \"auto\" for .litmus-lite/runs/<run id>.ndjson."),
				"stream_push": strArg("Optional. Forward -stream-push: ingest URL(s) to POST events to, comma-separated. https unless the host is loopback. The token comes from LITMUS_LITE_STREAM_TOKEN, never from here."),
				"stream_pull": strArg("Optional. Forward -stream-pull: address to serve the run as SSE on while it runs, for example 127.0.0.1:7070. The tool call returns after the run and a short linger."),
				"run_id":      strArg("Optional. Forward -run-id: the id used on stream events (A-Z a-z 0-9 . _ -, up to 64). Give a generator the same id to join their streams."),
				"labels":      strArg("Optional. Forward -label: comma-separated key=value labels for every stream event, for example \"service=checkout,env=ci\"."),
				"live":        boolArg("Optional. Forward -live: show the run in the live view even when CI is set or a stream_* destination is given. The live view is already the default when none is given."),
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
	// the live view is on unless CI is set or a destination is given.
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
		return append(argv, str("file", "")), nil
	case "run_test":
		argv := []string{"run", "-output", "json"}
		if o := str("out", ""); o != "" {
			argv = append(argv, "-out", o)
		}
		argv = safetyFlags(argv)
		argv = streamFlags(argv)
		return append(argv, str("file", "")), nil
	case "diagnose_failure":
		return []string{"diagnose", "-output", "json", str("file", "")}, nil
	case "list_faults":
		return []string{"hub", "list", "-output", "json"}, nil
	case "hub_import":
		argv := []string{"hub", "import", "-beside", str("beside", "."), "-output", "json"}
		if n := str("name", ""); n != "" {
			argv = append(argv, "-name", n)
		}
		return append(argv, str("id", "")), nil
	case "compare_reports":
		return []string{"compare", "-output", "json", str("baseline", ""), str("candidate", "")}, nil
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
		b, err := os.ReadFile(str("file", "report.json"))
		return toolResult{Text: string(b)}, err
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
