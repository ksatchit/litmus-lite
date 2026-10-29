# Environment variables

None is required. A scenario never sets them: destinations, tokens and permissions come from you, not from a file someone else may have written.

## Safety and MCP

| Variable | Read by | Meaning |
| --- | --- | --- |
| `LITMUS_LITE_MCP_ALLOW` | `mcp serve` | comma-separated safety arguments a tool call may pass: `yes`, `allow-target`, `allow-pid`. Set it with `litmus-lite init -env LITMUS_LITE_MCP_ALLOW=...`. [Details](mcp.md#safety-arguments). |
| `LITMUS_LITE_BIN` | `mcp serve` | the `litmus-lite` binary the server runs (default: itself) |

## Files and directories

| Variable | Default | Meaning |
| --- | --- | --- |
| `LITMUS_LITE_HOME` | `~/.litmus-lite` | host directory; holds `audit.log` (one line per command) |
| `LITMUS_LITE_CONFIG_DIR` | your OS config directory, `litmus-lite` | where the live-view identity (`live.json`) is kept |

## Streaming

Each one is the same as the `run` flag with the same name; a flag wins.

| Variable | Flag |
| --- | --- |
| `LITMUS_LITE_STREAM_FILE` | `-stream-file` |
| `LITMUS_LITE_STREAM_PUSH` | `-stream-push` (comma-separated) |
| `LITMUS_LITE_STREAM_PULL` | `-stream-pull` |
| `LITMUS_LITE_RUN_ID` | `-run-id`. litmus-lite also exports it to a load generator. |
| `LITMUS_LITE_STREAM_TOKEN` | the bearer token for `-stream-push`, and the token a non-loopback `-stream-pull`, `serve` or `stream receive` requires. Never a flag, so it stays out of shell history. |

## Live view

| Variable | Meaning |
| --- | --- |
| `LITMUS_LITE_LIVE_URL` | a live-view service to stream runs to (as `-live-url`). Without one, there is no live view. |
| `LITMUS_LITE_LIVE` | `off` (or `0`, `false`, `no`) turns the live view off, as `-no-live` |
| `LITMUS_LITE_OPEN` | `off` streams to the live view without opening a browser, as `-no-open` |
| `CI` | when set (and not `0` or `false`), the browser is never opened |

[Live view](streaming.md#live-view) explains the service.

## Failpoints (your service)

Read by the Go [`failpoint`](https://pkg.go.dev/github.com/litmuschaos/litmus-lite/failpoint) package inside your service, not by litmus-lite.

| Variable | Meaning |
| --- | --- |
| `LITMUS_LITE_FAILPOINT` | comma-separated hook names that may fire (`*` for all). Unset: every hook is a no-op. |
| `LITMUS_LITE_FAILPOINT_TOKEN` | the token a scenario's `failpoint` fault must send to arm a hook |
