# Walkthrough: chaos testing with an AI agent (MCP)

litmus-lite is agent-native, not agent-mandatory. An agent can write a `*.chaos.yaml` for you and run it, but the result is an ordinary file in your repository, and the CLI runs it without the agent. This page shows that with [Cursor](https://cursor.com); any editor or agent that speaks MCP over stdio works the same way (`litmus-lite mcp serve`).

Three rules the design keeps, and a good way to check an agent is following them:

1. **A test is a file, never chat state.** The agent must leave a `*.chaos.yaml` on disk, beside the service.
2. **The CLI works alone.** You can leave the editor and re-run the file in a terminal.
3. **MCP tools mirror CLI commands.** Each tool runs the same command you can type, and there is no richer agent-only path.

It takes 12 to 16 minutes with an app to test (the walkthrough uses [LaunchPad](https://github.com/umamukkara/launchpad): `missioncontrol` on `:8080`, `engine` on `:7070`) and `init` done once. The terminal version is [the terminal walkthrough](launchpad.md); what each fault does is in the [fault catalog](../reference/faults.md).

## 0. Set up once

1. Put `litmus-lite` on your `PATH` and start the app under test (here, LaunchPad; check `curl http://127.0.0.1:8080/healthz` returns `200`).
2. From the project root:

```
litmus-lite init -editor cursor
```

This adds `litmus-lite` → `mcp serve` to `.cursor/mcp.json` and writes `.cursor/rules/litmus-lite.mdc`. `init` records the absolute path of the binary that ran it; re-run it with another binary to switch. Reload MCP if the tools (`list_faults`, `run_test` and the rest) do not appear.

3. Try the first prompt once in Agent mode, so you know how long the tools take (about 15 seconds for the HTTP latency run).

Keep `report.html` openable as a file. No Grafana, load generator or Prometheus is needed.

## 1. Ask for a slow-API test

Paste, adjusting the path:

> LaunchPad is running on localhost:8080. Using the litmus-lite MCP tools only: import `http.latency` beside `cmd/missioncontrol` (or use `examples/launchpad/ignite.chaos.yaml` in the litmus-lite repository). Validate it. Point probes at the proxy listen address, not :8080. Run it. Then diagnose the report and tell me whether p95 tracked the injected delay and whether the service recovered.

What should happen:

- Tool calls: `hub_import` (or the agent opens `ignite.chaos.yaml`), then `validate_scenario`, `run_test` and `diagnose_failure` or `get_results`.
- A `*.chaos.yaml` in the tree, beside the service.
- `report.json` and `report.html` on disk.
- The agent says passed or failed using the **hypothesis names**, and talks about the inject band.

Open `report.html` yourself. The shaded band is the fault; p95 should follow the delay and recover afterwards. This file is the test: you can re-run it in CI without the editor.

If the agent only describes a scenario and writes nothing, ask it to write the file, then call `validate_scenario`. If it runs against `:8080`, the probes are not going through the proxy: edit the probes and `listen` to `:18080` and run again.

## 2. Check that the CLI and MCP agree

> Show me `list_faults`. Then run `doctor`. Do not add any tool that is not a CLI command.

The fault ids are the ones `litmus-lite hub list` prints. `doctor` may warn about a missing load generator or Prometheus; neither is needed.

## 3. Freeze a process, with your consent

> The LaunchPad engine process is on this machine. Find its PID (`pgrep -fl engine`). Validate and run `examples/launchpad/pause-engine.chaos.yaml` with `allow-pid` set to that PID. I expect GET /api/engine/status to fail or time out while the engine is stopped, then recover. Do not probe /api/launches for this. Do not use process.kill. Do not pass yes.

`allow-pid`, `allow-target` and `yes` confirm an action that is destructive or out of scope, so **an agent cannot supply them**. The MCP server refuses the call, before any CLI runs, unless you enabled that flag once for the server:

```
litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid
```

then restart the MCP server (reload the window). Without it the call is refused, and the agent should tell you the CLI command to run yourself. See [SECURITY.md](https://github.com/litmuschaos/litmus-lite/blob/main/SECURITY.md#mcp) for what the gate covers and what it does not (a scenario an agent writes can still contain a `cmd` probe, which `validate` and `run` list before running it).

If the agent paused the wrong process, run `kill -CONT PID` and give an explicit PID in the next message.

## 4. Compare two runs

After the first run, rename `report.json` to `baseline.json` (or ask the agent to). After a second run:

> Compare `baseline.json` and `report.json` with `compare_reports`. Go through steady, inject and recover. Do not invent a resilience score.

You get a table by phase, not a single number.

## 5. More prompts

Co-locate a different fault:

> Import `http.timeout` beside `cmd/missioncontrol` as `ignite-timeout.chaos.yaml`. Do not run it yet. Show me the file.

Expect a file on disk with `kind: http.timeout` and probes on `listen`. A timeout shows as a hole in availability, not a clean p95 shift.

With a load generator (only if `vegaload` is on your `PATH`; see [load and chaos](load-and-chaos.md)):

> Validate and run `examples/launchpad/ignite-load.chaos.yaml` with the same `run_test` tool. Diagnose p95 against 800 ms and whether the load's requests per second landed on the overlay.

`move-to` has no MCP tool; the CLI-only command is `litmus-lite move-to -adapter litmus FILE` (the agent can run it in a shell, or you can).

Do not ask an agent to `process.kill` a process you cannot restart, `disk.fill` a real volume, or pause a container that is not disposable.

## Prompt card

```
1) Import or use ignite (or http.latency). Validate. Probes on :18080. Run. Diagnose.
2) list_faults + doctor.
3) pause-engine with allow-pid <engine pid>. No kill. No yes. Explain the availability drop and the recovery.
4) compare_reports baseline vs candidate. Phase table only.
```

## MCP and the CLI

| MCP tool | CLI |
| --- | --- |
| `create_scenario` | `new` |
| `validate_scenario` | `validate` |
| `run_test` | `run` |
| `get_results` | read the report JSON |
| `diagnose_failure` | `diagnose` |
| `list_faults` | `hub list` |
| `hub_import` | `hub import` |
| `compare_reports` | `compare` |
| `doctor` | `doctor` |

When it is done, you have two files to keep: the chaos YAML and the HTML report. The agent is optional the second time.
