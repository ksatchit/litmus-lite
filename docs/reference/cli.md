# CLI

```text
litmus-lite COMMAND [flags] [arguments]
```

Flags can go before or after the arguments: `validate -strict FILE` and `validate FILE -strict` are the same. Everything after `--` is an argument, even if it starts with `-`. Most commands take `-output json` for machine-readable output; that is what the [MCP tools](mcp.md) read.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success; for `run`, every hypothesis held |
| `1` | the scenario or the command could not proceed |
| `2` | a usage error, or `run` refused by the safety rules |
| `3` | `run` finished and a hypothesis did not hold |
| `130` | `run` was interrupted; every fault was rolled back and no report was written |

## Writing scenarios

### `new`

Writes a scenario from a template.

```bash
litmus-lite new -beside ./checkout -kind http.latency -name checkout-slow
```

| Flag | Default | |
| --- | --- | --- |
| `-beside DIR` | `.` | directory to write into |
| `-kind ID` | `http.latency` | the template: any `hub list` id |
| `-name STEM` | `http-latency` | file name, without `.chaos.yaml` |

### `hub`

The built-in fault templates.

```bash
litmus-lite hub list                         # every template id
litmus-lite hub search latency               # ids that match
litmus-lite hub show http.latency            # describe and print a template
litmus-lite hub import http.latency -beside ./checkout [-name STEM]
```

Templates are embedded in the binary. A `.litmus-lite/hub.lock` file can pin an extra catalog from a git repository; the embedded ids always win.

### `validate`

Checks a scenario without touching anything: the format, every fault's parameters, the safety rules and the lints. Prints `valid` and any warnings, and lists the commands the scenario would run.

```bash
litmus-lite validate -strict checkout/http-latency.chaos.yaml
```

| Flag | |
| --- | --- |
| `-strict` | warnings are errors: unknown keys, probes that bypass the proxy, no hypotheses |
| `-allow-target HOSTS` | comma-separated non-loopback hosts the scenario may touch |
| `-allow-pid PIDS` | comma-separated PIDs the process faults may signal |
| `-yes` | allow irreversible faults and faults longer than 60 seconds |

## Running

### `run`

Runs a scenario and writes `report.json` and `report.html`.

```bash
litmus-lite run -out results/slow.json -junit results/slow.xml checkout/http-latency.chaos.yaml
```

| Flag | Default | |
| --- | --- | --- |
| `-out PATH` | `report.json` | the JSON report |
| `-html PATH` | the `-out` path with `.html` | the HTML report |
| `-junit PATH` | | also write JUnit XML |
| `-strict`, `-allow-target`, `-allow-pid`, `-yes` | | as for `validate` |
| `-output json` | | print the result as JSON |

Streaming a run as it happens (all optional; see [Streaming](streaming.md)):

| Flag | |
| --- | --- |
| `-stream-file PATH` | write the run's events as NDJSON; `auto` writes `.litmus-lite/runs/RUN_ID.ndjson` |
| `-stream-push URL` | POST events to an ingest URL, repeatable; `https` unless loopback. Token from `LITMUS_LITE_STREAM_TOKEN`. |
| `-stream-pull ADDR` | serve the run as server-sent events on `ADDR` while it runs |
| `-stream-pull-linger D` | keep serving after the run (default `15s`) |
| `-stream-samples` | also stream every probe sample |
| `-stream-scenario` | include the scenario, credentials masked, in `run.started` |
| `-stream-insecure` | allow `http://` push to a non-loopback host |
| `-stream-cors-origin O` | `Access-Control-Allow-Origin` for `-stream-pull` |
| `-run-id ID` | the run's id on events (default: random) |
| `-label k=v` | a label on every event, repeatable |
| `-live-url URL`, `-live`, `-no-live`, `-no-open` | the [live view](streaming.md#live-view) |

A streaming destination that is refused (a remote `http://` push, a non-loopback bind without a token) exits `2` before the run. One that fails during the run is a warning and never changes the result.

### `diagnose`

Explains a report: which hypotheses failed, by how much, and what the faults and recovery looked like.

```bash
litmus-lite diagnose report.json
```

### `compare`

Compares two reports phase by phase (steady, inject, recover). A phase regressed when its error rate rose by more than one percentage point, or its p95 by more than 20%. Exits `3` if any phase regressed.

```bash
litmus-lite compare before.json after.json
```

## Setup and checks

### `init`

Connects an editor: adds the MCP server and the agent rule to the project.

```bash
litmus-lite init                                       # Cursor, and .mcp.json for Claude Code
litmus-lite init -editor claude-code
litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid  # set an env var on the MCP server
```

`-env KEY=VALUE` is repeatable; `KEY=` removes it. Re-run `init` after you move or upgrade the binary: the entry records its absolute path.

### `doctor`

Checks the binary, `PATH`, an optional load generator and the editor setup.

| Flag | |
| --- | --- |
| `-file FILE` | also check that scenario's load tool |
| `-target URL` | check that an HTTP URL answers |
| `-stream URL\|ADDR` | check a streaming destination: an ingest URL accepts a test batch, a listen address can be bound |
| `-stream-insecure` | with `-stream`, allow `http://` to a remote host |
| `-fix` | write the missing Cursor MCP and rule files |

### `version`

Prints the version.

## Agents

### `mcp serve`

The MCP server, over stdio. Editors start it for you after `init`. [MCP tools](mcp.md).

### `mcp eval`

Runs the server against its fixtures and prints `PASS` or `FAIL` for each. Run it from a clone of the repository: some fixtures use the scenarios in `examples/`.

## CI and export

### `ci github`

Writes `.github/workflows/litmus-lite.yml`: it installs a release, checks it against `checksums.txt`, and runs `validate -strict` on every `*.chaos.yaml`. [CI guide](../guides/ci.md).

| Flag | |
| --- | --- |
| `-version TAG` | the release to install (default: the latest) |
| `-force` | overwrite an existing workflow |

### `move-to`

Exports a scenario to another tool's format. [Export guide](../guides/export.md).

```bash
litmus-lite move-to -adapter ir -out scenario.ir.json checkout/http-latency.chaos.yaml
```

| Flag | Default | |
| --- | --- | --- |
| `-adapter` | `ir` | `ir` (a neutral JSON form) or `litmus` (a LitmusChaos experiment) |
| `-out PATH` | | also write it to a file |

### `export job` (experimental)

Writes a Kubernetes ConfigMap and Job that run the scenario in a cluster. `-image` is required: the project publishes no image. `-allow-target` and `-yes` are passed to the Job.

## Streams and the live view

| Command | |
| --- | --- |
| `stream from-report REPORT.json` | turn a report into an event recording (`-out`, `-run-id`, `-label`, `-samples`) |
| `stream validate FILE.ndjson` | check a recording against the event format |
| `stream replay FILE.ndjson...` | serve recordings over HTTP/SSE (`-stream-pull`, `-speed`, `-max-gap`), or push them (`-stream-push`, `-fresh`, `-run-id`) |
| `stream receive` | a reference ingest endpoint for `-stream-push` (`-listen`, `-dir`, `-print`, and `-delay`, `-fail-percent` to test a sender) |
| `serve` | a read-only data source over a directory of run files (`-dir`, `-listen`, `-cors-origin`, `-stale`) |
| `live enable URL` / `live disable` | keep or forget a live-view service for this machine |
| `live [-open]` / `live reset` | print or open the project page; start a new project |

Details: [Streaming](streaming.md).
