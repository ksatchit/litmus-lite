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
- **MCP** (`internal/mcp`): execs CLI, parses `-output json`. Must not call engine functions directly.
- **Hosts / doctor** (`internal/hosts`, `internal/doctor`): `init` and `doctor` share host paths.
- **Compose** (`internal/compose`): load-generator CLI exec + normalized series merge (`vegaload`, `k6`, `command`).
- **move-to** (`internal/moveto`): IR and adapters. Core engine has zero awareness of Litmus SaaS or Harness.
- **Stream** (`internal/stream`): the run event format (`docs/stream/schema/v1.json`), NDJSON recordings, the HTTP servers behind `stream replay` and `-stream-pull` (`live.go`), the push sink and the reference receiver. Event format v1 is frozen: add optional fields or new types only. It must not import the engine. The engine talks to it only through the `engine.Emitter` interface (nil means off, and the engine never waits on it); `stream.Recorder` implements it and puts a bounded queue in front of every sink (`internal/stream/sink.go`). Live `point` events come from `report.BucketPoint`, the same function that builds the final series, so keep them in step. Streaming is additive: with no `-stream-*` flag the only addition is the default live view (`internal/live`: a `live.json` identity, a push to the live-view service, the page opened in the browser; off with `-no-live`, `LITMUS_LITE_LIVE=off`, `CI` or any explicit `-stream-*` destination, and never used by commands other than `run`; MCP `run_test` follows the CLI default and has `live`, `no-live` and `no-open` arguments, and returns the run JSON as its first text item and the stderr notes, with `live view: URL`, as a second one; exit code 3, a finished run that did not pass, is returned the same way, not as a tool error), a streaming problem never changes a run's result, reports or exit code, and credentials in fault params are never emitted (`stream.Redact`). Destinations come from flags or environment, never from a scenario file. Design: `docs/streaming.md`.
- **Failpoint** (`failpoint`): Go SDK (`Delay`, `Fail`, `Panic`). Off unless `LITMUS_LITE_FAILPOINT` lists the name. `UseWindow` keeps hooks dark until a scenario arms them. The `failpoint` fault POSTs to the app; it does not import the SDK. Sample app: `examples/cashier`.

## Conventions

- `gofmt` before commit.
- Tests live next to the code (`_test.go`).
- Pass/fail predicates on a chaos run are **hypotheses**, not “thresholds” and not “resilience score.”
- Community-facing copy never requires a vendor name for `move-to`.
- Commit messages: one line, present tense, why not a command log.
- Do not add telemetry. The default live view of `run` is the one deliberate exception to "nothing leaves the machine unprompted": it sends the run (not usage data) to the live-view service named in `docs/streaming.md`, with documented opt-outs. Keep it that way: no other command may send anything by default, the push token and project id must never reach a report, event or the audit log, and tests must never touch a real config directory or a real bridge (`cmd/litmus-lite/main_test.go` turns it off and isolates it).

## Commands an agent should know

- `litmus-lite new -beside DIR` — write a co-located scenario.
- `litmus-lite validate FILE`
- `litmus-lite run FILE` — `-out`, `-allow-target`, `-allow-pid`, `-yes`, `-output json`
- `litmus-lite diagnose FILE.json`
- `litmus-lite hub list` / `hub import ID -beside DIR` — HTTP, process, docker.pause, cpu.hog, memory.hog, disk.fill, failpoint; optional `.litmus-lite/hub.lock` (embedded IDs win)
- `litmus-lite run FILE` — `-junit FILE` writes optional JUnit XML
- `litmus-lite init` / `doctor` / `mcp serve` / `mcp eval` (fixtures exec the CLI)
- `litmus-lite compare BASE CAND`
- `litmus-lite move-to -adapter ir|litmus`
- `litmus-lite run FILE -stream-file PATH|auto -stream-push URL -stream-pull ADDR -run-id ID -label k=v [-stream-samples -stream-scenario -stream-pull-linger D -stream-insecure -stream-cors-origin O]` — stream the run as live events to a file, to an ingest URL (token from `LITMUS_LITE_STREAM_TOKEN`) and as SSE (S1, S2). Without these flags `run` is unchanged. With them, SIGINT/SIGTERM roll faults back and end the stream with `run.failed`. A destination that is refused (http to a remote host, a non-loopback bind without a token) exits 2 before the run; one that fails later is only a warning.
- `litmus-lite live [-open] | live reset` — print or open this machine's live-view project page, or start a new project. `run` uses the live view by default (`-live`, `-no-live`, `-no-open`, `-live-url`); see "Live view" in `docs/streaming.md`.
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

`docs/PRD.md`, `docs/context/`, and `examples/launchpad/UPSTREAM.md` are gitignored. If they exist on disk, treat them as private working notes. Never add them to the repository.
