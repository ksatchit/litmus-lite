# MCP tools

`litmus-lite mcp serve` is an MCP server over stdio. `litmus-lite init` registers it for Cursor (`.cursor/mcp.json`) and Claude Code (`.mcp.json`); any other MCP client can start the same command.

**Every tool runs a CLI command you can type yourself.** The server starts the `litmus-lite` binary with the matching subcommand and `-output json`, and returns what it printed. There is no agent-only behaviour, so anything an agent did, you can repeat in a terminal or in CI.

## Tools

| Tool | Runs | Arguments |
| --- | --- | --- |
| `create_scenario` | `new` | `beside` (directory, default `.`), `name` (file stem, default `scenario`) |
| `list_faults` | `hub list` | |
| `hub_import` | `hub import` | `id` (required), `beside`, `name` |
| `validate_scenario` | `validate` | `file` (required), `strict`, and the [safety arguments](#safety-arguments) |
| `run_test` | `run` | `file` (required), `out`, `strict`, the safety arguments, and the streaming and live-view arguments below |
| `get_results` | reads the report | `file` (default `report.json`) |
| `diagnose_failure` | `diagnose` | `file` (required) |
| `compare_reports` | `compare` | `baseline`, `candidate` (both required) |
| `doctor` | `doctor` | `file` (optional scenario to check) |

`run_test` also takes `stream_file`, `stream_push`, `stream_pull`, `run_id`, `labels`, `live`, `no-live` and `no-open`, which map to the `run` flags of the same names. A push token is never a tool argument: it comes from `LITMUS_LITE_STREAM_TOKEN` in the server's environment.

### What `run_test` returns

The first text item is the run as JSON (the same as `run -output json`). A second item carries the warnings and notes `run` printed, including `live view: URL` when a live view is configured. A run that finished and did not pass (exit code `3`) is returned the same way as a pass, not as a tool error: the agent reads the verdict from the JSON.

## Safety arguments

`yes`, `allow-target` and `allow-pid` confirm a destructive or out-of-scope action. **A tool call cannot supply them on its own.** The server refuses a call that carries one, before any command runs, unless the person who set up the server listed it in `LITMUS_LITE_MCP_ALLOW`:

```bash
litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid     # comma-separated: yes, allow-target, allow-pid
```

Restart the server afterwards (in Cursor, reload the window). The refusal says which argument was refused and how to enable it, so the agent can pass that on, or give you the command to run yourself.

The gate does not cover command probes: an agent that writes a scenario with `type: cmd` can run that command. `validate_scenario` lists such commands under `runsCommands`. Review a scenario an agent wrote before you run it.

## The agent rule

`init` also writes `.cursor/rules/litmus-lite.mdc`, which tells the agent the conventions:

- write every scenario as a `*.chaos.yaml` file, never only in chat;
- point probes, and any load generator, at an HTTP fault's `listen` address;
- validate a scenario it wrote with `strict`;
- do not require Prometheus or a load generator for a chaos-only run;
- it cannot consent for you: when a safety argument is refused, tell you, or give you the command to run;
- when a run prints `live view: URL`, tell you the URL.

The rule is plain text in your repository; read it, and edit it if your team wants other conventions.

## Checking the server

```bash
litmus-lite mcp eval
```

runs the server against its fixtures, including the refusals, and prints `PASS` or `FAIL` for each. Run it from a clone of the repository: some fixtures use `examples/`.

## Using another binary

The server runs commands with its own binary, the one at the absolute path `init` recorded. `LITMUS_LITE_BIN` in the server's environment points it at another one.
