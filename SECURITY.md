# Security

## Reporting a vulnerability

Please report it privately, not in a public issue: use GitHub's "Report a vulnerability" button on the repository's Security tab, or write to karthik.s@harness.io. Include the version (`litmus-lite version`), what you did and what you expected.

litmus-lite is maintained by volunteers. We aim to acknowledge a report within five working days and to tell you what we plan to do within ten. We will credit you in the release notes if you want that. Please give us a reasonable time to ship a fix before you publish.

Only the latest release is supported.

## What litmus-lite is, for a security review

litmus-lite is a command-line tool that injects faults and runs probes **with the permissions of the user who runs it**. It is not a sandbox, and a scenario file is closer to a script than to a configuration file. The rules below are guard rails against mistakes. They are not a boundary against a scenario written by someone hostile.

## What it checks before it injects anything

- A target host that is not loopback must be named with `-allow-target`. A Kubernetes service host (`*.svc`) needs `-allow-target` and `-yes`. `0.0.0.0` and a bare `:port` are refused.
- `process.pause` and `process.kill` signal only PIDs named with `-allow-pid`. PIDs 0 and 1, litmus-lite's own PID and any PID not listed are refused when the file is validated and again when the fault starts.
- `process.kill`, `disk.fill` and any fault longer than 60 seconds need `-yes`.
- `disk.fill` is capped at 256 MiB per fault, never overwrites a file and does not follow symlinks.
- Every fault is rolled back when the run ends, fails or is interrupted (SIGINT, SIGTERM). Some rollbacks cannot run if the process is killed outright; see below.
- Fault parameters that look like credentials are masked in streamed events (`stream.Redact`). The push token and live-view project id never reach a report, an event or the audit log.
- Network clients that carry a token (stream push, live view, failpoint) do not follow redirects, so the token cannot be forwarded elsewhere. A remote `http://` push is refused unless `-stream-insecure`. The servers behind `-stream-pull`, `serve` and `stream receive` bind to loopback by default; binding elsewhere needs a token.

## A scenario can run commands

- A probe of `type: cmd` (or `command`) runs its command with `sh -c`, and `load.tool: command` runs a program, both with your permissions, **during `run`**, and there is no allowlist. Nothing runs at `validate` or at `hub import`.
- `validate` and `run` print every such command, in quotes, before anything runs, and `validate -output json` lists them under `runsCommands`. litmus-lite does not ask for confirmation, so that it keeps working in CI.
- **Read a scenario you did not write before you run it**: one from a hub, a cloned repository or a pull request, as you would read a shell script.
- `load.tool: vegaload` and `k6` run that tool from your `PATH`. A load generator's own arguments (`load.args`) are opaque to litmus-lite, so `-allow-target` cannot check what the generator calls.

## MCP

`litmus-lite mcp serve` lets an agent call litmus-lite. Each tool runs the same CLI command a person can run. Three flags confirm a destructive or out-of-scope action: `yes`, `allow-target` and `allow-pid`. **A tool call cannot supply them.** The server refuses a call that carries one, before any CLI runs, unless the person who set the server up named it in the `LITMUS_LITE_MCP_ALLOW` environment variable of the server entry:

```
litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid       # restart the editor afterwards
```

(`LITMUS_LITE_MCP_ALLOW` takes a comma-separated list of `yes`, `allow-target`, `allow-pid`.) What the gate does **not** cover: an agent that writes a scenario with a `cmd` probe or a `load.tool: command` can still run that command, because command probes are not gated. The notice above appears in the tool output. If your agent can write files, review what it wrote before you run it, or run it yourself from a terminal.

## Known limitations

These are known and not fixed. They are listed so you can decide whether they matter to you.

- **`docker.pause` has no allowlist.** It pauses whichever container name the scenario gives.
- **A fault that cannot be rolled back after `SIGKILL`.** If litmus-lite is killed with `SIGKILL` (or the machine loses power) during `process.pause`, the process stays stopped: resume it with `kill -CONT PID`. During `docker.pause`, run `docker unpause NAME`. A proxy stops with the process. A failpoint armed in your service stays armed until it is disarmed or the service restarts, because the arming has no expiry.
- **HTTP probes follow redirects.** A probe URL that points at a loopback address can be redirected to a host `-allow-target` would have refused. Probe only services you trust.
- **`-allow-pid` is a number.** If the target exits and the operating system reuses the PID within the run, the new process could be signalled. The window is small.
- **Loopback servers have no Host-header check.** `serve`, `-stream-pull` and `stream receive` listen on `127.0.0.1`. A web page open in your browser could read from them through DNS rebinding, or send a simple cross-origin POST to them. Run them only when you use them, and do not put a secret in a run you stream to one.
- **A non-loopback bind with a token is plain HTTP.** Put TLS in front of it.
- **Fault parameters are in the report.** `report.json` records the fault's parameters as written. Credentials are masked in streamed events, not in the report: do not put a real secret in a scenario (the sample failpoint token is `cashier-demo`).
- **Command probes need `sh`.** They do not run on Windows.
- **Release binaries are not signed or notarized.** Check them against `checksums.txt`.
- **`export job` is experimental.** It writes a ConfigMap and a Job and refuses a scenario the Job would refuse, but the project publishes no container image: you supply `-image`. Review the Job before you apply it to a cluster.

## Telemetry

None. See "What leaves your machine" in the [README](README.md) and [docs/streaming.md](docs/streaming.md).
