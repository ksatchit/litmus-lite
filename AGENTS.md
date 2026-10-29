# AGENTS.md — litmus-lite

This file tells any AI coding agent how to work in this repository. Read it before making a change.

## What this project is

Litmus-lite is a thin, open-source chaos testing tool: a single static binary that runs declarative `*.chaos.yaml` scenarios on a laptop or in CI, writes a self-contained overlay report, and speaks MCP by wrapping its own CLI. It is VegaLoad’s twin for failure injection.

Licence: Apache-2.0.

## The one design principle that overrides everything else

Litmus-lite is agent-native. It is not agent-mandatory.

1. **Every test is a real file, never chat state.** Scenarios an agent creates must be written as `*.chaos.yaml` on disk.
2. **The CLI always works alone.** If a feature only works through an agent, that is a bug.
3. **MCP tools mirror CLI commands.** Every MCP tool must invoke the same CLI a human can type. Do not build a richer, agent-only path.

If a proposed change breaks any of these, stop and flag it.

## Language and runtime

- Core: Go. Single static binary. Cross-compiles to Linux, macOS, Windows.
- Scenarios: YAML (`*.chaos.yaml`). Do not introduce JavaScript as the primary chaos authoring format.
- Load composition execs an external generator. `load.tool` is `vegaload` (default), `k6`, or `command`. Do not import a generator as a library.
- MCP is Go, subcommand `litmus-lite mcp serve`, not a separate Node process.

## Module boundaries

- **Engine** (`internal/engine`): orchestration only. No MCP, no HTML, no move-to adapters.
- **Faults** (`internal/faults`): plugins. Adding a kind must not edit the orchestrator beyond registration.
- **Probes** (`internal/probe`): HTTP and command.
- **Report** (`internal/report`): JSON + HTML from a `Result` value.
- **Hub** (`internal/hub`): embedded catalog; optional git pin via `.litmus-lite/hub.lock`.
- **MCP** (`internal/mcp`): execs CLI, parses `-output json`. Must not call engine functions directly. A tool call cannot consent for the person: `yes`, `allow-target` and `allow-pid` are refused, before any CLI runs, unless the person listed them in `LITMUS_LITE_MCP_ALLOW` in the server's env (`init -env`). A new safety flag must be added to `safetyFlagNames` (a test fails otherwise), and `mcp eval` keeps fixtures that prove the refusals.
- **Hosts / doctor** (`internal/hosts`, `internal/doctor`): `init` and `doctor` share host paths.
- **Compose** (`internal/compose`): load-generator CLI exec + normalized series merge (`vegaload`, `k6`, `command`).
- **move-to** (`internal/moveto`): IR and adapters. Core engine has zero awareness of any hosted platform.
- **Stream** (`internal/stream`): the run event format (`docs/stream/schema/v1.json`), NDJSON recordings, the HTTP servers behind `stream replay` and `-stream-pull` (`live.go`), the push sink and the reference receiver. Event format v1 is frozen: add optional fields or new types only. It must not import the engine. The engine talks to it only through the `engine.Emitter` interface (nil means off, and the engine never waits on it); `stream.Recorder` implements it and puts a bounded queue in front of every sink (`internal/stream/sink.go`). Live `point` events come from `report.BucketPoint`, the same function that builds the final series, so keep them in step. Streaming is additive and opt-in: with no `-stream-*` flag and no live-view service configured, `run` sends nothing and opens nothing. The live view (`internal/live`: a `live.json` identity, a push to a live-view service, the page opened in the browser) is dormant until a destination is configured with `-live-url`, `LITMUS_LITE_LIVE_URL` or `litmus-lite live enable URL`; there is no default address. It is off for a run with `-no-live` or `LITMUS_LITE_LIVE=off`, and any explicit `-stream-*` destination replaces it unless `-live` adds it. It is never used by commands other than `run`. MCP `run_test` follows the CLI (it execs it) and has `live`, `no-live` and `no-open` arguments; it returns the run JSON as its first text item and the stderr notes, with `live view: URL`, as a second one. Exit code 3, a finished run that did not pass, is returned the same way, not as a tool error. A streaming problem never changes a run's result, reports or exit code, and credentials in fault params are never emitted (`stream.Redact`). Destinations come from flags or environment, never from a scenario file. Design: `docs/reference/streaming.md`.
- **Failpoint** (`failpoint`): Go SDK (`Delay`, `Fail`, `Panic`). Off unless `LITMUS_LITE_FAILPOINT` lists the name. `UseWindow` keeps hooks dark until a scenario arms them. The `failpoint` fault POSTs to the app; it does not import the SDK. Sample app: `examples/cashier`.

## Conventions

- `gofmt` before commit.
- Tests live next to the code (`_test.go`).
- User docs are the MkDocs site in `docs/` (`mkdocs.yml`, published to GitHub Pages). A page that shows a task shows it both ways, in content tabs named "In Cursor" and "In a terminal". A behaviour change updates the page that describes it; `mkdocs build --strict` must pass.
- Pass/fail predicates on a chaos run are **hypotheses**, not “thresholds” and not “resilience score.”
- Community-facing copy never requires a vendor name for `move-to`.
- Commit messages: one line, present tense, why not a command log.
- A scenario that can run a command (a `cmd` probe, `load.tool: command`) must be announced: any new way to run one goes into `commandsToRun` (`cmd/litmus-lite/notice.go`) so `validate` and `run` list it before it runs. MCP's `yes`, `allow-target` and `allow-pid` are refused unless a person set `LITMUS_LITE_MCP_ALLOW`; a new safety flag goes into `safetyFlagNames`.
- Do not add telemetry. Nothing leaves the machine unprompted: every destination (a stream flag or variable, or a live-view service) is configured by the user, the push token and project id must never reach a report, event or the audit log, and tests must never touch a real config directory or a real live-view service (`cmd/litmus-lite/main_test.go` turns the live view off and isolates it; any new package that calls `run` needs the same).

## Commands an agent should know

- `litmus-lite new -beside DIR` — write a co-located scenario.
- `litmus-lite validate FILE`
- `litmus-lite run FILE` — `-out`, `-allow-target`, `-allow-pid`, `-yes`, `-strict`, `-output json`. `validate FILE -strict` fails on a key or param nothing reads. Hypothesis, probe-mode and report semantics: `docs/concepts/hypotheses.md`.
- `litmus-lite diagnose FILE.json`
- `litmus-lite hub list` / `hub import ID -beside DIR` — HTTP, process, docker.pause, cpu.hog, memory.hog, disk.fill, failpoint; optional `.litmus-lite/hub.lock` (embedded IDs win)
- `litmus-lite run FILE` — `-junit FILE` writes optional JUnit XML
- `litmus-lite init` / `doctor` / `mcp serve` / `mcp eval` (fixtures exec the CLI)
- `litmus-lite compare BASE CAND`
- `litmus-lite move-to -adapter ir|litmus`
- `litmus-lite run FILE -stream-file PATH|auto -stream-push URL -stream-pull ADDR -run-id ID -label k=v [-stream-samples -stream-scenario -stream-pull-linger D -stream-insecure -stream-cors-origin O]` — stream the run as live events to a file, to an ingest URL (token from `LITMUS_LITE_STREAM_TOKEN`) and as SSE (S1, S2). Without these flags `run` is unchanged. With them, SIGINT/SIGTERM roll faults back and end the stream with `run.failed`. A destination that is refused (http to a remote host, a non-loopback bind without a token) exits 2 before the run; one that fails later is only a warning.
- `litmus-lite live [-open] | live enable URL | live disable | live reset` — keep or forget the live-view service of this machine, print or open its project page, or start a new project. `run` uses a configured live view (`-live`, `-no-live`, `-no-open`, `-live-url`); see "Live view" in `docs/reference/streaming.md`.
- `litmus-lite init -env KEY=VALUE` (repeatable) — write variables into the MCP server entry. It refuses credentials (they would land in a committed file).
- `litmus-lite stream receive [-dir D -listen A -print -delay D -fail-percent N]` — reference ingest endpoint for `-stream-push`. `litmus-lite doctor -stream URL|ADDR` checks a destination.
- `litmus-lite serve [-dir D -listen A -cors-origin O -stale D]` — read-only data source over a directory of run files; follows a run in progress by tailing its file (`internal/stream/serve.go`). It must stay read-only and must never import the engine or talk to a running `run`.
- `litmus-lite stream from-report|validate|replay` — turn a report into events, check a recording, serve recordings over HTTP/SSE, or push them (`replay -stream-push`). Fixtures live in `docs/stream/fixtures/` (`generate.sh` re-records them; a test keeps each `.ndjson` equal to what `from-report` writes).
- `go test ./...`

## What not to do

- Do not add MCP behaviour that has no CLI equivalent.
- Do not generate ChaosEngine / ChaosExperiment manifests as the supported export (use IR).
- Do not make Prometheus or VegaLoad required for a chaos-only run.
- Do not put scenarios only in chat or only under `.litmus-lite/` — co-locate `*.chaos.yaml`.
- Do not name the commercial SaaS in Litmus-org getting-started docs; adapters are `move-to -adapter <name>`.

## Local-only files (do not commit)

Anything listed under "Local-only internals" in `.gitignore` is private working material for the maintainers. If such files exist on disk, read them for context, but never add them to the repository (not with `git add -f` either), and never quote or link them from a tracked file, a commit message or a pull request.
